// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tests

import (
	"fmt"
	"go/ast"
	"go/token"
	"slices"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/yousysadmin/mailyard/internal/core/apidoc"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/openapi"
)

// TestEveryV1RouteIsDocumented is what remains to be enforced once the
// shapes are reflected.
//
// The document's FIELDS now come from the response types, so it cannot
// disagree with the code about what a body contains - that class of
// drift is gone by construction rather than by test. What reflection
// cannot notice is a route nobody described, or a description whose
// route was renamed: metadata is a separate list from the
// registrations, so the two can still fall out of step.
//
// Both directions fail. An undocumented route ships a surface the
// document denies exists, and an orphaned entry documents a 404.
func TestEveryV1RouteIsDocumented(t *testing.T) {
	registered := v1Routes(t)

	documented := map[string]bool{}
	for _, r := range openapi.Routes() {
		documented[r.Method+" "+apidoc.NormalizePath(r.Path)] = true
	}

	var missing, orphaned []string
	for r := range registered {
		if !documented[r] {
			missing = append(missing, r)
		}
	}

	for d := range documented {
		if !registered[d] {
			orphaned = append(orphaned, d)
		}
	}

	slices.Sort(missing)
	slices.Sort(orphaned)
	for _, r := range missing {
		t.Errorf("route %s has no entry in any domain's APIDocs - add one beside its handler", r)
	}

	for _, d := range orphaned {
		t.Errorf("APIDocs describes %s but routes.go registers no such route", d)
	}
}

// TestSpecBuildsAndDescribesEveryRoute runs the reflector over the
// real metadata. A type it cannot walk, or a route with no declared
// response, fails here rather than at the first request to the one
// endpoint whose job is describing the others.
func TestSpecBuildsAndDescribesEveryRoute(t *testing.T) {
	raw, err := openapi.MachineSpec()
	if err != nil {
		t.Fatalf("building the document: %v", err)
	}

	var doc struct {
		Paths      map[string]map[string]any `yaml:"paths"`
		Components struct {
			Schemas map[string]any `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("the document is not valid yaml: %v", err)
	}

	ops := 0
	for _, item := range doc.Paths {
		for verb := range item {
			switch verb {
			case "get", "post", "put", "patch", "delete":
				ops++
			}
		}
	}

	if want := len(openapi.Routes()); ops != want {
		t.Errorf("document carries %d operations, metadata declares %d", ops, want)
	}

	if len(doc.Components.Schemas) == 0 {
		t.Error("no component schemas were registered - the reflector produced nothing")
	}

	// Every $ref must resolve. A dangling one silently breaks every
	// generator that reads this.
	for _, ref := range refsIn(raw) {
		name := strings.TrimPrefix(ref, "#/components/schemas/")
		if name == ref {
			t.Errorf("unexpected ref form %q", ref)
			continue
		}

		if _, ok := doc.Components.Schemas[name]; !ok {
			t.Errorf("$ref %q resolves to nothing", ref)
		}
	}
}

// refsIn pulls every $ref out of the serialized document. Reading the
// bytes rather than walking the map keeps this independent of how
// deeply the refs are nested.
func refsIn(raw []byte) []string {
	var out []string
	for line := range strings.SplitSeq(string(raw), "\n") {
		_, after, found := strings.Cut(line, "$ref:")
		if !found {
			continue
		}

		out = append(out, strings.Trim(strings.TrimSpace(after), `"'`))
	}

	return out
}

// v1Routes parses routes.go and returns "method /path" for every
// registration on the v1 group, with the group prefix stripped so it
// can be compared to the metadata directly.
func v1Routes(t *testing.T) map[string]bool {
	t.Helper()
	out := routesUnder(t, "/api/v1", nil)
	if len(out) == 0 {
		t.Fatal("found no v1 routes in routes.go - did the group variable get renamed?")
	}

	return out
}

// groupPrefixes resolves every `x := y.Group("/path", ...)` in
// routes.go to the full prefix x carries, following the chain up to
// the app.
//
// A flat list of v1.Get calls would need only one receiver name
// recognised. The surface registers
// through per-resource groups, because permOn declares its resource on
// the GROUP - so a checker that cannot resolve a prefix would silently
// see zero routes and pass while describing nothing.
func groupPrefixes(file *ast.File) map[string]string {
	prefix := map[string]string{}
	// Repeat until stable: a group may be declared before the one it
	// hangs off is seen in source order.
	for range 4 {
		ast.Inspect(file, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
				return true
			}

			name, ok := as.Lhs[0].(*ast.Ident)
			if !ok {
				return true
			}

			call, ok := as.Rhs[0].(*ast.CallExpr)
			if !ok {
				return true
			}

			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Group" || len(call.Args) == 0 {
				return true
			}

			p, ok := groupPath(call.Args[0])
			if !ok {
				return true
			}

			base := ""
			if parent, ok := sel.X.(*ast.Ident); ok && parent.Name != "app" {
				base = prefix[parent.Name]
			}

			prefix[name.Name] = base + p

			return true
		})
	}

	return prefix
}

// groupPath resolves the prefix argument of a Group() call.
//
// It is not always a literal: the console's own api is mounted at
// env.ConsolePath + "/api", because the mount point is defined once
// and three unrelated places build links from it. A parser that only
// understood literals silently saw no routes under that group and
// reported every one of them as an orphaned doc entry - which is how
// this function came to exist.
func groupPath(arg ast.Expr) (string, bool) {
	switch e := arg.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}

		s, err := strconv.Unquote(e.Value)

		return s, err == nil
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}

		l, lok := groupPath(e.X)
		r, rok := groupPath(e.Y)

		return l + r, lok && rok
	case *ast.SelectorExpr:
		// env.ConsolePath is the only constant a mount is built from.
		if pkg, ok := e.X.(*ast.Ident); ok && pkg.Name == "env" && e.Sel.Name == "ConsolePath" {
			return env.ConsolePath, true
		}

		return "", false
	}

	return "", false
}

// routesUnder collects "method /path" for every registration whose
// resolved full path sits under base, with base stripped. Paths listed
// in exclude (already base-relative) are skipped.
func routesUnder(t *testing.T, base string, exclude func(full string) bool) map[string]bool {
	t.Helper()
	fset, files := parseRouter(t, 0)

	out := map[string]bool{}
	// Prefixes are resolved PER FILE, because a group variable is local
	// to the function that opened it - two files may each name a group
	// `enrol` and mean different mounts.
	for _, file := range files {
		prefix := groupPrefixes(file)
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !httpVerbs[sel.Sel.Name] {
				return true
			}

			recv, ok := sel.X.(*ast.Ident)
			if !ok || len(call.Args) == 0 {
				return true
			}

			group, known := prefix[recv.Name]
			if !known {
				return true
			}

			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				t.Errorf("%s: a route with a non-literal path cannot be checked",
					fset.Position(call.Pos()))

				return true
			}

			p, _ := strconv.Unquote(lit.Value)
			full := group + p
			if !strings.HasPrefix(full, base) {
				return true
			}

			if exclude != nil && exclude(full) {
				return true
			}

			rel := apidoc.NormalizePath(strings.TrimPrefix(full, base))
			out[fmt.Sprintf("%s %s", strings.ToUpper(sel.Sel.Name), rel)] = true

			return true
		})
	}

	return out
}

// A path parameter in the hand-written docs is written :name, the
// form routes.go uses - never {name}.
//
// The Go generator reads the colon form when it builds a method name
// and passes anything else through verbatim, so one brace produced
// `func (c *Client) DeleteAdminCertificates{name}(...)`. That is not a
// compile error anybody sees in this module: sdk/go is a separate
// module, and what actually failed was three SQL guards that parse the
// tree and could no longer parse the client.
func TestDocumentedPathParametersUseTheRouterForm(t *testing.T) {
	var findings []string
	for _, r := range openapi.Routes() {
		if strings.Contains(r.Path, "{") || strings.Contains(r.Path, "}") {
			findings = append(findings, r.Method+" "+r.Path)
		}
	}

	if len(findings) > 0 {
		t.Errorf("%d documented path(s) use {braces} where routes.go uses :colons:\n  %s\n\n"+
			"The SDK generator builds a method name from the colon form and copies\n"+
			"anything else through, so a brace lands in a Go identifier.",
			len(findings), strings.Join(findings, "\n  "))
	}
}
