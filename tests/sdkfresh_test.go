// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tests

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/yousysadmin/mailyard/internal/sdkgen"
)

// TestGeneratedClientIsUpToDate regenerates into memory and compares
// against the files on disk.
//
// TestSDKCoversEveryV1Route catches a missing or orphaned ROUTE, which
// is the loud kind of drift. This catches the quiet kind: a field
// renamed on a wire type, a response gaining a key, a request losing
// one. None of that changes a path, so the coverage test stays green
// while the client sends and expects the wrong shape - and it compiles
// perfectly, because the client's copy of the type is internally
// consistent. The first symptom would be a field silently arriving
// empty in somebody else's service.
//
// The fix when this fails is `task sdk-gen`, which the message says.
func TestGeneratedClientIsUpToDate(t *testing.T) {
	want, err := sdkgen.Render()
	if err != nil {
		t.Fatalf("generating the client: %v", err)
	}

	if len(want) == 0 {
		t.Fatal("the generator produced nothing - this test would pass vacuously")
	}

	for name, expected := range want {
		path := filepath.Join(repoRoot(t), sdkgen.Dir, name)
		actual, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s is missing - run `task sdk-gen`", path)
			continue
		}

		if string(actual) == expected {
			continue
		}

		t.Errorf("%s is out of date - run `task sdk-gen`.\n%s",
			path, firstDifference(string(actual), expected))
	}
}

// The Python and Ruby clients, same rule. They carry no generated
// types, so the quiet drift this catches for them is narrower - a route
// renamed, a body appearing or disappearing - but a client that names a
// method the server no longer serves is exactly as broken.
func TestGeneratedScriptClientsAreUpToDate(t *testing.T) {
	for dir, want := range map[string]map[string]string{
		sdkgen.PythonDir: sdkgen.RenderPython(),
		sdkgen.RubyDir:   sdkgen.RenderRuby(),
	} {
		if len(want) == 0 {
			t.Fatalf("%s: the generator produced nothing", dir)
		}

		for name, expected := range want {
			path := filepath.Join(repoRoot(t), dir, name)
			actual, err := os.ReadFile(path)
			if err != nil {
				t.Errorf("%s is missing - run `task sdk-gen`", path)
				continue
			}

			if string(actual) == expected {
				continue
			}

			t.Errorf("%s is out of date - run `task sdk-gen`.\n%s",
				path, firstDifference(string(actual), expected))
		}
	}
}

// firstDifference reports the first line that differs, because a
// whole-file diff of four thousand generated lines tells nobody
// anything.
func firstDifference(actual, expected string) string {
	a := strings.Split(actual, "\n")
	e := strings.Split(expected, "\n")
	for i := range max(len(a), len(e)) {
		var la, le string
		if i < len(a) {
			la = a[i]
		}

		if i < len(e) {
			le = e[i]
		}

		if la != le {
			return "first difference at line " + strconv.Itoa(i+1) +
				":\n  on disk:    " + la + "\n  generated:  " + le
		}
	}

	return "the files differ only in length"
}
