// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package tests holds the checks that are not about any one package.
//
// A test lives beside the code it describes - that is the rule
// everywhere else in this tree, and every package's own tests, store
// tests included, stay where they are. What is here is the other kind:
// a rule over the whole repository, where the subject is the agreement
// between two things that live in different packages.
//
//	The router against the OpenAPI document, and against the permission
//	catalogue, the console nav, the alert list and three SDKs
//	every query against the migrated schema, and against tenancy
//	every SQL string being a constant, every replica read being a read
//	the console's own rules - one clock, one preview, one stylesheet
//	nothing but ids.New minting an id, no pointer field saying omitempty
//
// Put one of these in the package it happens to touch - the Vue preview
// guard in internal/domain/trackingpage, say - and it sits in a package
// that does not break the rule and is not where anyone would look for
// it.
//
// Two of them need a database and skip without MAILYARD_TEST_DSN,
// schemaguard and tenancyguard. They are not split into a directory of
// their own because they share the query evaluator with the static
// guards beside them, and a second copy of that evaluator would be a
// second answer to "what SQL does this repository contain".
// `task test-db` runs everything either way.
package tests

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot finds the repository by searching upward for go.mod.
//
// Searching, never a fixed number of `..` segments. Half of these
// guards walk a directory, and a walker pointed at nothing finds no
// violations and PASSES - which reads exactly like a repository with no
// violations in it. Two of them arrived here with a hardcoded depth,
// which is a guard that silently stops guarding the day somebody moves
// the file. This is the same search dbtest.MigrationsDir does.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	for range 10 {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}

		dir = parent
	}

	t.Fatal("could not find go.mod above the working directory")

	return ""
}

// consoleSrc is the Vue source tree, which five of these guards read.
func consoleSrc(t *testing.T) string {
	t.Helper()

	return filepath.Join(repoRoot(t), "web", "src")
}

// The three source files these guards read rather than walk.
//
// Named from the repository root for the same reason repoRoot searches:
// they were opened as "routes.go", relative to whatever directory the
// test happened to run in, so every one of them stopped finding its
// subject the moment these files moved. That failed loudly - a parse
// error naming the file - which is the only reason this was a five
// minute fix rather than a suite that had quietly stopped checking.
// routerFiles is every file that registers routes, not routes.go alone.
//
// It was one filename, and that stopped being true the moment a route
// group moved into a file of its own: six guards went on parsing
// routes.go and simply did not see the routes that had left it. The
// failure was loud here - the console document described five routes
// "routes.go registers no such route" - but the same walk decides
// whether a route is DOCUMENTED, PERMISSIONED and MAINTENANCE-GATED, and
// in those directions a route the parser cannot see is a route nobody
// checks.
//
// The subject of these guards is the router, and the router is a
// package. Test files are skipped, nothing else is.
func routerFiles(t *testing.T) []string {
	t.Helper()

	dir := filepath.Join(repoRoot(t), "internal", "server")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read the server package: %v", err)
	}

	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		if !editionFile(name) {
			continue
		}

		out = append(out, filepath.Join(dir, name))
	}

	if len(out) == 0 {
		t.Fatalf("no Go files under %s", dir)
	}

	return out
}

// editionFile reports whether a source file is part of this build.
//
// By name, not by reading the build constraint, and the two must agree:
// a `//go:build enterprise` file is named _ee.go, its community half
// _ce.go. That pairing is the convention the whole split rests on, and
// TestEveryEditionFileIsNamedForItsTag is what keeps the name and the
// tag from drifting apart - without it a mis-named file compiles into
// one build while every guard here judges it as part of the other.
//
// Neither ce nor ee is a GOOS or a GOARCH, so Go's own implicit
// filename constraints never fire on them.
func editionFile(name string) bool {
	switch {
	case strings.HasSuffix(name, "_ee.go"):
		return enterpriseBuild
	case strings.HasSuffix(name, "_ce.go"):
		return !enterpriseBuild
	default:
		return true
	}
}

// parseRouter parses them all, so a caller walks the package the way it
// used to walk the file.
func parseRouter(t *testing.T, mode parser.Mode) (*token.FileSet, []*ast.File) {
	t.Helper()

	fset := token.NewFileSet()

	var files []*ast.File
	for _, path := range routerFiles(t) {
		f, err := parser.ParseFile(fset, path, nil, mode)
		if err != nil {
			t.Fatalf("parse %s: %v", filepath.Base(path), err)
		}

		files = append(files, f)
	}

	return fset, files
}

func providerFile(t *testing.T) string {
	t.Helper()

	return filepath.Join(repoRoot(t), "internal", "database", "postgres", "provider.go")
}

func validationErrorsFile(t *testing.T) string {
	t.Helper()

	return filepath.Join(repoRoot(t), "internal", "core", "validation", "errors.go")
}

// goTree is the Go source this repository owns, which the SQL and
// replica-read guards walk in full.
//
// internal only: the two SDKs under sdk/ are generated clients with
// their own module, and web/ carries no Go.
func goTree(t *testing.T) string {
	t.Helper()

	return filepath.Join(repoRoot(t), "internal")
}

func serverDir(t *testing.T) string {
	t.Helper()

	return filepath.Join(repoRoot(t), "internal", "server")
}

func consoleAPIDir(t *testing.T) string {
	t.Helper()

	return filepath.Join(consoleSrc(t), "api")
}

func sdkDir(t *testing.T) string {
	t.Helper()

	return filepath.Join(repoRoot(t), "sdk", "go")
}

func sdkGenDir(t *testing.T) string {
	t.Helper()

	return filepath.Join(sdkDir(t), "api")
}
