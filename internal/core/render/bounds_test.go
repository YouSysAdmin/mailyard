package render

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

// A variable reassigned inside a range doubles without writing a
// byte, so neither the output cap nor the iteration cap reaches it in
// time. Every string function is charged, and the doubling is refused
// fast and small.
func TestADoublingVariableIsRefusedEarly(t *testing.T) {
	loops := map[string]string{
		"print":    `{{$a := "xx"}}{{range 200}}{{$a = print $a $a}}{{end}}`,
		"printf":   `{{$a := "xx"}}{{range 200}}{{$a = printf "%s%s" $a $a}}{{end}}`,
		"println":  `{{$a := "xx"}}{{range 200}}{{$a = println $a $a}}{{end}}`,
		"html":     `{{$a := "<>"}}{{range 200}}{{$a = html $a $a}}{{end}}`,
		"js":       `{{$a := "<>"}}{{range 200}}{{$a = js $a $a}}{{end}}`,
		"urlquery": `{{$a := "<>"}}{{range 200}}{{$a = urlquery $a $a}}{{end}}`,
		"compare":  `{{$a := "xx"}}{{range 20}}{{$a = print $a $a}}{{end}}{{range 1000000}}{{if eq $a $a}}{{end}}{{end}}`,
	}

	for _, r := range []*Renderer{{}, {MissingKeyBehavior: MissingKeyZero}} {
		for name, src := range loops {
			for part, in := range map[string]*Input{
				"subject": {Subject: src},
				"html":    {Subject: "s", HTML: src},
				"text":    {Subject: "s", Text: src},
			} {
				var before, after runtime.MemStats
				runtime.GC()
				runtime.ReadMemStats(&before)
				start := time.Now()
				_, err := r.Render(in, map[string]any{})
				took := time.Since(start)
				runtime.ReadMemStats(&after)

				if !errors.Is(err, ErrTooMuchText) {
					t.Errorf("%s in %s: got %v, want ErrTooMuchText", name, part, err)
				}

				if took > 5*time.Second {
					t.Errorf("%s in %s: took %v", name, part, took)
				}

				if alloc := after.TotalAlloc - before.TotalAlloc; alloc > 512<<20 {
					t.Errorf("%s in %s: allocated %d MiB", name, part, alloc>>20)
				}
			}
		}
	}
}

// The charge is far above what an honest template spends.
func TestTheTextBudgetLeavesOrdinaryTemplatesAlone(t *testing.T) {
	items := make([]any, 5000)
	for i := range items {
		items[i] = map[string]any{"name": fmt.Sprintf("item number %d", i)}
	}

	src := `{{range items}}{{if ne .name "x"}}{{printf "%s: %s" .name (print .name)}}{{end}}{{end}}`
	_, err := (&Renderer{}).Render(&Input{Subject: "s", Text: src}, map[string]any{"items": items})
	if err != nil {
		t.Fatal(err)
	}
}

// Inlining a stylesheet costs rules times elements, and past the
// bound the block is kept rather than inlined, quickly.
func TestInliningIsBounded(t *testing.T) {
	var css, html strings.Builder
	for i := range 3000 {
		fmt.Fprintf(&css, "p { color: red; margin-%d: 0 }\n", i)
	}

	for range 3000 {
		html.WriteString("<p>x</p>")
	}

	start := time.Now()
	out := InlineCSS(html.String(), css.String())
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("took %v", took)
	}

	if !strings.Contains(out, "<style>") || strings.Contains(out, `style="`) {
		t.Errorf("an oversized stylesheet must stay a block")
	}

	small := InlineCSS("<p>x</p>", "p { color: red }")
	if !strings.Contains(small, `style="color:red`) {
		t.Errorf("a small stylesheet must still inline: %s", small)
	}
}

// Lenient means blank for a missing parent too, and for print of a
// missing key, in every part.
func TestALenientRenderBlanksMissingNestedKeys(t *testing.T) {
	r := &Renderer{MissingKeyBehavior: MissingKeyZero}
	data := map[string]any{"user": map[string]any{"first": "Ann"}, "n": nil}
	cases := map[string]string{
		"[{{ user.first }}]":                                         "[Ann]",
		"[{{ user.last }}]":                                          "[]",
		"[{{ account.owner.name }}]":                                 "[]",
		"[{{ print missing }}]":                                      "[]",
		"[{{ printf \"%s\" missing }}]":                              "[]",
		"[{{ printf \"%s\" account.owner }}]":                        "[]",
		"[{{ println missing }}]":                                    "[\n]",
		"[{{ if account.vip }}y{{ else }}n{{ end }}]":                "[n]",
		"[{{ range account.items }}x{{ end }}]":                      "[]",
		"[{{ with $u := user }}{{ $u.first }}{{ $u.x.y }}{{ end }}]": "[Ann]",
		"[{{ $.user.first }}]":                                       "[Ann]",
		"[{{ print n }}]":                                            "[]",
	}

	for src, want := range cases {
		out, err := r.Render(&Input{Subject: src, HTML: "<p>" + src + "</p>", Text: src}, data)
		if err != nil {
			t.Errorf("%s: %v", src, err)

			continue
		}

		if out.Subject != want || out.Text != want || out.HTML != "<p>"+want+"</p>" {
			t.Errorf("%s: subject %q text %q html %q, want %q", src, out.Subject, out.Text, out.HTML, want)
		}

		for _, s := range []string{out.Subject, out.HTML, out.Text} {
			if strings.Contains(s, "<nil>") || strings.Contains(s, "no value") {
				t.Errorf("%s: %q", src, s)
			}
		}
	}

	// Strict renders keep failing on a missing parent.
	if _, err := (&Renderer{}).Render(&Input{Subject: "{{ account.owner }}"}, data); err == nil {
		t.Error("a strict render must refuse a missing key")
	}
}

func TestCheckRefusesWhatCannotRender(t *testing.T) {
	for _, src := range []string{"{{ name", "{{ end }}", "{{ if }}", `{{ printf "%99999d" 1 }}`} {
		if CheckText(src) == nil {
			t.Errorf("text %q must be refused", src)
		}

		if CheckHTML(src) == nil {
			t.Errorf("html %q must be refused", src)
		}
	}

	if CheckHTML(`<a href="{{ url }}`) == nil {
		t.Error("an action in an unterminated attribute must be refused")
	}

	for _, src := range []string{"Hello {{ name }}", "{{ index items 3 }}", "{{ range items }}{{ .x }}{{ end }}", "{{ user.first }}", ""} {
		if err := CheckText(src); err != nil {
			t.Errorf("text %q: %v", src, err)
		}

		if err := CheckHTML("<p>" + src + "</p>"); err != nil {
			t.Errorf("html %q: %v", src, err)
		}
	}
}
