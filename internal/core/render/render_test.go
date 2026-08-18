package render

import (
	"strings"
	"testing"
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
	// under missingkey=zero (zero value of any is nil).
	if out.Subject != "x<no value>y" && out.Subject != "xy" {
		t.Errorf("subject = %q", out.Subject)
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
