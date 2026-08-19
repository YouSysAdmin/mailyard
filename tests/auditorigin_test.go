// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tests

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// An audit event takes where it came from from one place.
//
// `ClientIP: c.IP()` was written out at twenty-five event literals, which
// is twenty-five chances for the twenty-sixth to omit it - and that field
// is the only trace of origin the trail has. `Recorder.Project` and
// `Recorder.Security` take the request and stamp it now, so an event
// cannot be recorded without one.
//
// The same move added the user agent, and the two travel together on
// purpose. Neither identifies anybody: a Safari user with iCloud Private
// Relay on arrives from a Cloudflare, Akamai or Fastly egress shared with
// strangers, and no header carries their own - the egress proxy is never
// told the client's address, so nothing downstream can reveal what it
// never received. The user agent is forgeable. Together they are what a
// request offers, which is why one function owns the pair.
//
// Matched on the type, through the AST, rather than on the field name:
// sessions, tracking events and the relay node's own reports all record
// an address and an agent legitimately, and a name-based rule reported
// every one of them.
func TestAnAuditEventTakesItsOriginFromOnePlace(t *testing.T) {
	root := filepath.Join(repoRoot(t), "internal")
	// core/audit is the one place allowed to say it - that is the point.
	skip := filepath.Join("core", "audit")

	var findings []string
	literals := 0
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") ||
			strings.HasSuffix(path, "_test.go") {
			return err
		}

		rel, _ := filepath.Rel(root, path)
		if strings.HasPrefix(rel, skip) {
			return nil
		}

		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil // not this test's business
		}

		// Which local name the audit model is imported under here, if it
		// is imported at all.
		alias := auditModelAlias(file)
		if alias == "" {
			return nil
		}

		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}

			sel, ok := lit.Type.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Event" {
				return true
			}

			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != alias {
				return true
			}

			literals++
			for _, el := range lit.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok {
					continue
				}

				key, ok := kv.Key.(*ast.Ident)
				if !ok {
					continue
				}

				if key.Name == "ClientIP" || key.Name == "UserAgent" {
					findings = append(findings,
						rel+":"+strconv.Itoa(fset.Position(kv.Pos()).Line)+" "+key.Name)
				}
			}

			return true
		})

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	if literals < 15 {
		t.Fatalf("only found %d audit event literals - the parse is broken and this test "+
			"would pass vacuously", literals)
	}

	for _, f := range findings {
		t.Errorf("%s is set by hand on an audit event.\n\n"+
			"audit.Recorder.Project and .Security take the fiber.Ctx and stamp both the "+
			"address and the user agent through audit.Stamp. A copy here is a field the "+
			"next event type will forget - and the pair has to travel together, because a "+
			"shared egress address says nothing on its own", f)
	}
}

// auditModelAlias reports the local name internal/models/audit is
// imported under, empty when the file does not import it.
func auditModelAlias(file *ast.File) string {
	const want = `"github.com/yousysadmin/mailyard/internal/models/audit"`
	for _, imp := range file.Imports {
		if imp.Path.Value != want {
			continue
		}

		if imp.Name != nil {
			return imp.Name.Name
		}

		return "audit"
	}

	return ""
}
