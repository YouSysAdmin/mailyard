package render

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"{{ name }}":                    "{{ .name }}",
		"{{name}}":                      "{{ .name }}",
		"{{ .name }}":                   "{{ .name }}",
		"{{ $var }}":                    "{{ $var }}",
		"{{ range features }}":          "{{ range .features }}",
		"{{ if active }}":               "{{ if .active }}",
		"{{ end }}":                     "{{ end }}",
		"{{ else }}":                    "{{ else }}",
		"{{- name -}}":                  "{{- .name -}}",
		"Hi {{ name }}, {{ order_id }}": "Hi {{ .name }}, {{ .order_id }}",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderBasic(t *testing.T) {
	r := &Renderer{}
	out, err := r.Render(&Input{
		Subject: "Order {{ order_id }}",
		HTML:    "<p>Hello {{ name }}</p>",
		Text:    "Hello {{ name }}",
	}, map[string]any{"order_id": "42", "name": "Ada"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	if out.Subject != "Order 42" {
		t.Errorf("subject = %q", out.Subject)
	}

	if out.HTML != "<p>Hello Ada</p>" {
		t.Errorf("html = %q", out.HTML)
	}

	if out.Text != "Hello Ada" {
		t.Errorf("text = %q", out.Text)
	}
}

func TestRenderHTMLEscapes(t *testing.T) {
	r := &Renderer{}
	out, err := r.Render(&Input{Subject: "s", HTML: "<p>{{ name }}</p>"},
		map[string]any{"name": "<script>alert(1)</script>"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	if strings.Contains(out.HTML, "<script>") {
		t.Errorf("html injection not escaped: %q", out.HTML)
	}
}

func TestRenderMissingKey(t *testing.T) {
	strict := &Renderer{MissingKeyBehavior: MissingKeyError}
	if _, err := strict.Render(&Input{Subject: "{{ missing }}"}, map[string]any{}); err == nil {
		t.Error("missingkey=error must fail on missing data")
	}

	lax := &Renderer{MissingKeyBehavior: MissingKeyZero}
	out, err := lax.Render(&Input{Subject: "x{{ missing }}y"}, map[string]any{})
	if err != nil {
		t.Fatalf("missingkey=zero must not fail: %v", err)
	}

	// text/template renders a missing map key as "<no value>" even
	// under missingkey=zero (zero value of any is nil), and the
	// renderer takes that marker out: lenient means blank, in the
	// subject and text part as it already is in the html.
	if out.Subject != "xy" {
		t.Errorf("subject = %q, want %q", out.Subject, "xy")
	}

	out, err = lax.Render(&Input{Subject: "s", HTML: "<p>[{{ missing }}]</p>", Text: "[{{ missing }}]"}, map[string]any{})
	if err != nil {
		t.Fatalf("missingkey=zero must not fail: %v", err)
	}

	if !strings.Contains(out.HTML, "[]") || out.Text != "[]" {
		t.Errorf("html = %q, text = %q, want a blank in both", out.HTML, out.Text)
	}
}

func TestRenderInlinesCSS(t *testing.T) {
	r := &Renderer{}
	out, err := r.Render(&Input{
		Subject: "s",
		HTML:    "<html><head></head><body><p class=\"lead\">hi</p></body></html>",
		CSS:     "p.lead { color: red; }",
	}, map[string]any{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	if !strings.Contains(strings.ReplaceAll(out.HTML, " ", ""), `style="color:red`) {
		t.Errorf("css not inlined: %q", out.HTML)
	}
}

func TestInlineCSSFallsBackOnBareFragment(t *testing.T) {
	got := InlineCSS("<p>hi</p>", "p { color: blue; }")
	if !strings.Contains(got, "hi") {
		t.Errorf("content lost: %q", got)
	}
}

func TestHTMLToText(t *testing.T) {
	in := `<html><body><p>Hello &amp; welcome</p><ul><li>One</li><li>Two</li></ul>` +
		`<p>Visit <a href="https://example.com">our site</a> now<br>bye</p></body></html>`
	got := HTMLToText(in)
	for _, want := range []string{
		"Hello & welcome",
		"- One",
		"- Two",
		"our site (https://example.com)",
		"bye",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("text missing %q in:\n%s", want, got)
		}
	}

	if strings.Contains(got, "<") {
		t.Errorf("tags leaked: %q", got)
	}

	if HTMLToText("") != "" {
		t.Error("empty input must stay empty")
	}
}

// A template's output is bounded, and bounded EARLY: the render below
// would produce 32 MiB from forty bytes of source, and the point is
// that it stops at the cap rather than allocating all of it first.
func TestRenderRefusesUnboundedOutput(t *testing.T) {
	wide := make([]any, 2000)
	for i := range wide {
		wide[i] = i
	}

	data := map[string]any{"a": wide, "b": wide}
	r := &Renderer{}
	for _, in := range []*Input{
		{Subject: "s", HTML: "{{range .a}}{{range $.b}}12345678{{end}}{{end}}"},
		{Subject: "s", Text: "{{range .a}}{{range $.b}}12345678{{end}}{{end}}"},
		{Subject: "{{range .a}}{{range $.b}}12345678{{end}}{{end}}"},
	} {
		_, err := r.Render(in, data)
		if !errors.Is(err, ErrOutputTooLarge) {
			t.Errorf("got %v, want ErrOutputTooLarge", err)
		}
	}

	// And an ordinary render is untouched.
	out, err := r.Render(&Input{Subject: "hi {{.n}}", HTML: "<p>{{.n}}</p>"}, map[string]any{"n": "x"})
	if err != nil || out.HTML != "<p>x</p>" {
		t.Fatalf("plain render: %q %v", out, err)
	}
}

// A range whose body writes nothing never meets the output cap, so
// the iteration budget has to stop it. A literal integer range needs
// no data at all and is the same case, and so is a template that calls
// itself twice, which branches with no range anywhere.
func TestRenderRefusesUnboundedIteration(t *testing.T) {
	wide := make([]any, 20000)
	for i := range wide {
		wide[i] = i
	}

	data := map[string]any{"a": wide}
	r := &Renderer{}
	for name, in := range map[string]*Input{
		"nested text": {Subject: "s", Text: "{{range .a}}{{range $.a}}{{end}}{{end}}"},
		"nested html": {Subject: "s", HTML: "{{range .a}}{{range $.a}}{{end}}{{end}}"},
		"literal":     {Subject: "{{range 3000000}}{{end}}"},
		"defined":     {Subject: `{{define "r"}}{{range $.a}}{{end}}{{end}}{{range .a}}{{template "r" $}}{{end}}`},
		"recursion": {Subject: `{{define "b"}}{{if .}}{{template "b" (slice . 1)}}{{template "b" (slice . 1)}}{{end}}{{end}}` +
			`{{template "b" "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"}}`},
		"recursion html": {Subject: "s", HTML: `{{define "b"}}{{if .}}{{template "b" (slice . 1)}}{{template "b" (slice . 1)}}{{end}}{{end}}` +
			`{{template "b" "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"}}`},
	} {
		start := time.Now()
		_, err := r.Render(in, data)
		if !errors.Is(err, ErrTooManyIterations) {
			t.Errorf("%s: err = %v, want ErrTooManyIterations", name, err)
		}

		// Generous because the race detector slows a million counted
		// iterations several times over.
		if took := time.Since(start); took > 60*time.Second {
			t.Errorf("%s: refused only after %s", name, took)
		}
	}
}

// The planted counter must not change what an ordinary template
// renders, including a defined template called from two escaping
// contexts, which is the path where html/template copies nodes.
func TestRenderCountsWithoutChangingOutput(t *testing.T) {
	r := &Renderer{}
	out, err := r.Render(&Input{
		Subject: "{{range .xs}}{{.}}{{end}}",
		HTML:    `{{define "n"}}{{range .xs}}{{.}}{{end}}{{end}}<p>{{template "n" .}}</p><a title="{{template "n" .}}">x</a>`,
		Text:    "{{range .xs}}{{.}},{{else}}none{{end}}",
	}, map[string]any{"xs": []any{1, 2, 3}})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	if out.Subject != "123" || out.HTML != `<p>123</p><a title="123">x</a>` || out.Text != "1,2,3," {
		t.Errorf("subject=%q html=%q text=%q", out.Subject, out.HTML, out.Text)
	}

	out, err = r.Render(&Input{Subject: "{{range .xs}}{{.}}{{else}}none{{end}}"}, map[string]any{"xs": []any{}})
	if err != nil || out.Subject != "none" {
		t.Errorf("empty range: %q, %v", out.Subject, err)
	}
}

// fmt allocates the padding before writing, so the verb is refused
// at parse.
func TestRenderRefusesWideFormats(t *testing.T) {
	r := &Renderer{}
	for _, src := range []string{
		`{{printf "%999999999d" 1}}`,
		`{{printf "%.999999999f" 1.0}}`,
		`{{printf "%*d" 5 1}}`,
		`{{printf "%[1]*d" 5 1}}`,
		`{{printf "%[2]*[1]d" 1 5}}`,
		`{{printf "%.[1]*f" 5 1.0}}`,
		`{{$f := "%99999d"}}{{printf $f 1}}`,
		`{{printf (print "%9999" "9d") 1}}`,
		`{{"%99999d" | printf}}`,
		`{{if true}}{{printf "%-99999s" "x"}}{{end}}`,
	} {
		if _, err := r.Render(&Input{Subject: src}, nil); !errors.Is(err, ErrFormatWidth) {
			t.Errorf("%s: err = %v, want ErrFormatWidth", src, err)
		}
	}

	out, err := r.Render(&Input{Subject: `{{printf "%05d|%8.2f" 42 3.14159}}`}, nil)
	if err != nil || out.Subject != "00042|    3.14" {
		t.Errorf("ordinary printf: %q, %v", out.Subject, err)
	}
}

// The planted iteration count writes nothing in any context. As an
// action it wrote an empty string, which html/template renders as `""`
// inside a script, so a template call in a JSON-LD block produced
// `var a=""1`.
func TestTheIterationCountWritesNothingInAScript(t *testing.T) {
	r := &Renderer{}
	out, err := r.Render(&Input{
		HTML: `{{define "x"}}1{{end}}<script>var a={{template "x" .}};var b=[{{range .N}}{{.}},{{end}}];</script>`,
	}, map[string]any{"N": []int{1, 2}})
	if err != nil {
		t.Fatal(err)
	}

	if want := `<script>var a=1;var b=[ 1 , 2 ,];</script>`; out.HTML != want {
		t.Errorf("html = %q, want %q", out.HTML, want)
	}
}
