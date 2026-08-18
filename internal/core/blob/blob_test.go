// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package blob

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFSStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := New(Config{Backend: "fs", FSPath: dir})
	if err != nil {
		t.Fatal(err)
	}

	ctx := t.Context()
	key := "emails/abc/0_report.pdf"
	if err := s.Put(ctx, key, strings.NewReader("pdf bytes"), "application/pdf"); err != nil {
		t.Fatalf("put: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "emails", "abc", "0_report.pdf")); err != nil {
		t.Errorf("file not on disk: %v", err)
	}

	rc, err := s.Get(ctx, key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	got, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(got) != "pdf bytes" {
		t.Errorf("content = %q", got)
	}

	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if _, err := s.Get(ctx, key); err == nil {
		t.Error("get after delete must fail")
	}

	// Deleting a missing key is not an error.
	if err := s.Delete(ctx, key); err != nil {
		t.Errorf("double delete: %v", err)
	}
}

func TestFSStoreRefusesTraversal(t *testing.T) {
	dir := t.TempDir()
	s, err := New(Config{Backend: "fs", FSPath: dir})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Put(t.Context(), "../escape", strings.NewReader("x"), ""); err == nil {
		t.Error("traversal key must be refused")
	}
}

func TestNewInlineAndInvalid(t *testing.T) {
	s, err := New(Config{Backend: ""})
	if err != nil || s != nil {
		t.Errorf("empty backend must mean nil store, got %v %v", s, err)
	}

	if _, err := New(Config{Backend: "ftp"}); err == nil {
		t.Error("unknown backend must error")
	}

	if _, err := New(Config{Backend: "s3"}); err == nil {
		t.Error("s3 without bucket must error")
	}
}

func TestSanitizeFilename(t *testing.T) {
	cases := map[string]string{
		"":                    "attachment",
		"report.pdf":          "report.pdf",
		"../../etc/passwd":    "etc_passwd",
		"we ird/na:me.tar.gz": "we_ird_na_me.tar.gz",
	}
	for in, want := range cases {
		if got := SanitizeFilename(in); got != want {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}
