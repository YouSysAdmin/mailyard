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

// A message whose last file is deleted leaves no directory behind, a
// directory still holding a file is kept, and the top-level prefix is
// never removed.
func TestFSStoreDeleteLeavesNoEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	s, err := New(Config{Backend: "fs", FSPath: dir})
	if err != nil {
		t.Fatal(err)
	}

	ctx := t.Context()
	for _, key := range []string{"inbound/one/0_a.txt", "inbound/one/1_b.txt", "inbound/two/0_c.txt"} {
		if err := s.Put(ctx, key, strings.NewReader("x"), "text/plain"); err != nil {
			t.Fatalf("put %s: %v", key, err)
		}
	}

	if err := s.Delete(ctx, "inbound/one/0_a.txt"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "inbound", "one")); err != nil {
		t.Errorf("a directory still holding a file was removed: %v", err)
	}

	if err := s.Delete(ctx, "inbound/one/1_b.txt"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "inbound", "one")); !os.IsNotExist(err) {
		t.Errorf("the emptied message directory is still there (err %v)", err)
	}

	if err := s.Delete(ctx, "inbound/two/0_c.txt"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "inbound")); err != nil {
		t.Errorf("the top-level prefix was removed: %v", err)
	}

	if err := s.Put(ctx, "inbound/three/0_d.txt", strings.NewReader("x"), "text/plain"); err != nil {
		t.Errorf("put after pruning: %v", err)
	}
}
