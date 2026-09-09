// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package tests holds the checks that are not about any one package.
//
// A test lives beside the code it describes, store tests included.
// What is here is the other kind: a rule over the whole repository,
// where the subject is the agreement between things that live in
// different packages.
//
//	The router against the OpenAPI document, and against the permission
//	catalogue, the console nav, the alert list and three SDKs
//	every query against the migrated schema, and against tenancy
//	every SQL string being a constant, every replica read being a read
//	the console's own rules - one clock, one preview, one stylesheet
//	nothing but ids.New minting an id, no pointer field saying omitempty
//
// Two of them need a database and skip without MAILYARD_TEST_DSN,
// schemaguard and tenancyguard. They stay beside the static guards
// because they share the query evaluator, and a second copy of it would
// be a second answer to "what SQL does this repository contain".
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
// Searching, never a fixed number of `..` segments: a walker pointed at
// nothing finds no violations and PASSES, which reads exactly like a
// repository with none. The same search dbtest.MigrationsDir does.
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

// The source files these guards read rather than walk, named from the
// repository root for the same reason repoRoot searches.

// routerFiles is every file that registers routes, not routes.go
// alone. The same walk decides whether a route is DOCUMENTED,
// PERMISSIONED and MAINTENANCE-GATED, and a route the parser cannot see
// is a route nobody checks.
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

		out = append(out, filepath.Join(dir, name))
	}

	if len(out) == 0 {
		t.Fatalf("no Go files under %s", dir)
	}

	return out
}

// parseRouter parses every router file into one file set.
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
