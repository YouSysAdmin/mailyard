// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tests

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// A mailbox is composed by smtpclient.FormatAddress, never by hand.
//
// `fmt.Sprintf("%s <%s>", name, addr)` was written twice - the campaign
// runner and platform mail - and it is wrong for any name that is not a
// plain ASCII word: `Faria, Inc.` becomes two addresses because the comma
// is the list separator, a quote needs escaping, and a non-ASCII name
// needs RFC 2047 encoding that Sprintf does not do. mail.Address.String()
// does all three.
func TestNobodyComposesAMailboxByHand(t *testing.T) {
	root := repoRoot(t)
	// The formatter itself, and the test that states what the hand-rolled
	// form did wrong.
	allowed := map[string]bool{
		filepath.Join("internal", "core", "smtpclient", "client.go"):             true,
		filepath.Join("internal", "core", "smtpclient", "formataddress_test.go"): true,
		filepath.Join("tests", "senderaddress_test.go"):                          true,
	}

	var findings []string
	files := 0
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			switch info.Name() {
			case ".git", "node_modules", "dist", "dev-data", "sdk":
				return filepath.SkipDir
			}

			return nil
		}

		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		rel, _ := filepath.Rel(root, path)
		if allowed[rel] {
			return nil
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		files++
		for i, line := range strings.Split(string(body), "\n") {
			if strings.Contains(line, "%s <%s>") {
				findings = append(findings, rel+":"+strconv.Itoa(i+1)+" "+strings.TrimSpace(line))
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	if files < 200 {
		t.Fatalf("only walked %d Go files - the tree was not found", files)
	}

	for _, f := range findings {
		t.Errorf("%s composes a mailbox by hand.\n\n"+
			"Use smtpclient.FormatAddress(name, addr): a name with a comma is two "+
			"addresses to a parser, a quote needs escaping, and a non-ASCII name needs "+
			"RFC 2047 encoding", f)
	}
}
