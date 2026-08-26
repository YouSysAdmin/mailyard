// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package render turns a stored template into deliverable content.
//
// Three things happen to a localization on its way out: the friendly
// {{ name }} syntax an author writes becomes Go template syntax
// (normalize.go), the result is executed against the caller's data, and
// a stylesheet is folded into the HTML as inline attributes because mail
// clients discard style blocks.
//
// THE SUBJECT AND TEXT GO THROUGH text/template AND THE BODY THROUGH
// html/template, which is not a detail: the body is the only one of the
// three that a browser will interpret, so it is the only one where a
// value carrying markup has to be escaped. Running the subject through
// html/template instead would escape an ampersand in somebody's name and
// deliver it that way.
package render

import (
	"bytes"
	"errors"
	"fmt"
	htmltmpl "html/template"
	"io"
	"strings"
	texttmpl "text/template"

	"github.com/vanng822/go-premailer/premailer"
)

// What to do about a template referencing data the caller did not send.
// These are Go's own "missingkey" values, named here so callers do not
// pass template option strings around.
const (
	MissingKeyError   = "error"   // fail the render
	MissingKeyZero    = "zero"    // use the zero value
	MissingKeyInvalid = "invalid" // render "<no value>"
)

// Renderer renders templates.
//
// The zero value fails on a missing key. That is the safe default for a
// thing that produces mail: rendering a blank where a name should be is
// a message that goes out wrong, and nobody finds out.
type Renderer struct {
	MissingKeyBehavior string
}

// Input is one renderable localization plus the stylesheet it was
// written against.
type Input struct {
	Subject string
	HTML    string
	Text    string
	CSS     string
}

// Output is the rendered result, ready for the message builder.
type Output struct {
	Subject string `json:"subject"`
	HTML    string `json:"html"`
	Text    string `json:"text"`
}

// Render executes the three templates against data.
//
// An empty HTML or text template stays empty rather than rendering to
// "": a localization with no text part must not produce a message
// claiming to have one.
func (r *Renderer) Render(input *Input, data map[string]any) (*Output, error) {
	onMissing := r.missingKey()

	subject, err := plainTemplate(input.Subject, data, onMissing)
	if err != nil {
		return nil, fmt.Errorf("render subject: %w", err)
	}

	out := &Output{Subject: subject}

	if input.HTML != "" {
		body, err := escapingTemplate(input.HTML, data, onMissing)
		if err != nil {
			return nil, fmt.Errorf("render html: %w", err)
		}

		if input.CSS != "" {
			body = InlineCSS(body, input.CSS)
		}

		out.HTML = body
	}

	if input.Text != "" {
		out.Text, err = plainTemplate(input.Text, data, onMissing)
		if err != nil {
			return nil, fmt.Errorf("render text: %w", err)
		}
	}

	return out, nil
}

// missingKey answers a valid template option whatever the field holds,
// so a caller that sets nothing, or sets nonsense, gets the safe one.
func (r *Renderer) missingKey() string {
	switch r.MissingKeyBehavior {
	case MissingKeyZero, MissingKeyInvalid:
		return r.MissingKeyBehavior
	default:
		return MissingKeyError
	}
}

// runnable is the half of a parsed template that both stdlib packages
// share. text/template and html/template have no common type - their
// Parse methods return their own - so the pair below differs only in
// which one it calls, and everything after that is this interface.
type runnable interface {
	Execute(w io.Writer, data any) error
}

// plainTemplate renders text that nothing will interpret.
func plainTemplate(src string, data map[string]any, onMissing string) (string, error) {
	t, err := texttmpl.New("t").Option("missingkey=" + onMissing).Parse(Normalize(src))
	if err != nil {
		return "", fmt.Errorf("template parse error: %w", err)
	}

	return execute(t, data)
}

// escapingTemplate renders markup, with html/template's contextual
// escaping applied to every value it substitutes.
func escapingTemplate(src string, data map[string]any, onMissing string) (string, error) {
	t, err := htmltmpl.New("t").Option("missingkey=" + onMissing).Parse(Normalize(src))
	if err != nil {
		return "", fmt.Errorf("template parse error: %w", err)
	}

	return execute(t, data)
}

// MaxOutputBytes bounds what one template may render to. Four times
// the largest source a template may be, which no honest message
// approaches.
//
// A bound on the SOURCE is not a bound on the OUTPUT: a kilobyte of
// nested range actions over a few thousand-element arrays expands to
// gigabytes, and Go's own maxExecDepth stops recursion, not breadth.
// The buffer grew until the process died, from a request the lowest
// template role can make - preview renders synchronously and needs no
// send.
const MaxOutputBytes = 4 * 1024 * 1024

// ErrOutputTooLarge is the render refusing past MaxOutputBytes.
var ErrOutputTooLarge = errors.New("rendered output exceeds the 4 MiB limit")

// limitWriter is a bytes.Buffer that refuses to grow past
// MaxOutputBytes. The template engine returns a writer's error
// unwrapped, so the caller sees ErrOutputTooLarge itself.
type limitWriter struct {
	buf bytes.Buffer
}

func (w *limitWriter) Write(p []byte) (int, error) {
	if w.buf.Len()+len(p) > MaxOutputBytes {
		return 0, ErrOutputTooLarge
	}

	return w.buf.Write(p)
}

func execute(t runnable, data map[string]any) (string, error) {
	var out limitWriter
	if err := t.Execute(&out, data); err != nil {
		if errors.Is(err, ErrOutputTooLarge) {
			return "", err
		}

		return "", fmt.Errorf("template execute error: %w", err)
	}

	return out.buf.String(), nil
}

// InlineCSS folds a stylesheet into the markup as element attributes.
//
// Mail clients strip style blocks - Gmail's web client does, Outlook
// does - so a stylesheet that stays a block is a stylesheet that does
// nothing. premailer resolves the rules against the document and writes
// them onto the elements they matched.
//
// A failure returns the document with the block still in it. That is a
// message styled for the clients that DO read a block rather than no
// message at all, and this runs on the send path where refusing costs
// somebody their mail.
func InlineCSS(html, css string) string {
	styled := withStyleBlock(html, css)

	opts := premailer.NewOptions()
	// The classes stay. premailer would strip them once their rules are
	// inlined, and they are what the tracking stripper and anyone
	// reading the source have to go on.
	opts.RemoveClasses = false

	prem, err := premailer.NewPremailerFromString(styled, opts)
	if err != nil {
		return styled
	}

	inlined, err := prem.Transform()
	if err != nil {
		return styled
	}

	return inlined
}

// withStyleBlock puts the stylesheet where a browser would look for it.
//
// A template body is not always a whole document - it is frequently a
// fragment with no head and no body tag at all - so the insertion point
// is whichever of these the markup actually has, in the order a real
// document would nest them.
func withStyleBlock(html, css string) string {
	block := "<style>\n" + css + "\n</style>"

	at, ok := styleInsertionPoint(html)
	if !ok {
		return block + "\n" + html
	}

	return html[:at] + block + "\n" + html[at:]
}

// styleInsertionPoint answers the offset the block goes at, or false
// when the markup offers nowhere sensible.
//
// Matched case-insensitively against a lowered copy, and the offsets are
// used against the ORIGINAL - lowering does not change any length, so
// the two stay aligned.
func styleInsertionPoint(html string) (int, bool) {
	lower := strings.ToLower(html)

	if at := strings.Index(lower, "</head>"); at != -1 {
		return at, true
	}

	// Just inside <body>, past whatever attributes it carries. An
	// unterminated tag means the markup is broken enough that guessing
	// an offset inside it would be worse than prepending.
	if at := strings.Index(lower, "<body"); at != -1 {
		if end := strings.Index(html[at:], ">"); end != -1 {
			return at + end + 1, true
		}

		return 0, false
	}

	if at := strings.Index(lower, "</body>"); at != -1 {
		return at, true
	}

	return 0, false
}
