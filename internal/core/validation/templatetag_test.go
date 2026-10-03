// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package validation

import (
	"strings"
	"testing"
)

func TestTemplateBlankAndMediaTypeRules(t *testing.T) {
	type input struct {
		Subject     string `json:"subject"      validate:"required,notblank,template=text"`
		HTML        string `json:"html"         validate:"omitempty,template=html"`
		ContentType string `json:"content_type" validate:"omitempty,mediatype"`
	}

	ok := input{Subject: "Hi {{ name }}", HTML: "<p>{{ name }}</p>", ContentType: "application/pdf"}
	if err := V().Struct(ok); err != nil {
		t.Fatalf("valid input refused: %v", err)
	}

	got := map[string]string{}
	bad := input{Subject: "   ", HTML: `<a href="{{ url }}`, ContentType: "not a type"}
	for _, fe := range Humanize(V().Struct(bad)) {
		got[fe.Field] = fe.Message
	}

	if got["subject"] != "Subject must not be blank" {
		t.Errorf("subject: %q", got["subject"])
	}

	if !strings.HasPrefix(got["html"], "HTML is not a valid template: ") {
		t.Errorf("html: %q", got["html"])
	}

	if got["content_type"] != "Content type must be a MIME type such as application/pdf" {
		t.Errorf("content_type: %q", got["content_type"])
	}

	got = map[string]string{}
	for _, fe := range Humanize(V().Struct(input{Subject: "Hi {{ name"})) {
		got[fe.Field] = fe.Message
	}

	if !strings.Contains(got["subject"], "not a valid template") {
		t.Errorf("unparseable subject: %q", got["subject"])
	}
}
