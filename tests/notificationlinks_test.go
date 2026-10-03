// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tests

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// consoleRoutePath matches a `path: '...'` entry in the console router.
var consoleRoutePath = regexp.MustCompile(`path:\s*'([^']*)'`)

// TestEveryNotificationLinksToAConsolePage checks the Link of every
// notification the server raises against the console router. A link
// the router does not know opens the console's catch-all, so a warning
// about a full quota sent its reader to a page that does not exist.
//
// A link built as a literal plus a value - "/campaigns/" + c.ID - is
// judged as the literal followed by one parameter segment.
func TestEveryNotificationLinksToAConsolePage(t *testing.T) {
	routes := consoleRoutePatterns(t)

	links := map[string]string{}
	root := filepath.Join(repoRoot(t), "internal")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return perr
		}

		ast.Inspect(f, func(n ast.Node) bool {
			kv, ok := n.(*ast.KeyValueExpr)
			if !ok {
				return true
			}

			if key, ok := kv.Key.(*ast.Ident); !ok || key.Name != "Link" {
				return true
			}

			if link, ok := linkPattern(kv.Value); ok {
				links[link] = fset.Position(kv.Pos()).String()
			}

			return true
		})

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(links) < 5 {
		t.Fatalf("only found %d notification links - the scan is broken", len(links))
	}

	for link, where := range links {
		if !matchesAnyRoute(link, routes) {
			t.Errorf("notification link %s is not a console page (%s)", link, where)
		}
	}
}

// linkPattern turns a Link value into a path, with ":param" standing in
// for whatever is appended to a literal prefix.
func linkPattern(e ast.Expr) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}

		s, err := strconv.Unquote(v.Value)

		return s, err == nil && strings.HasPrefix(s, "/")
	case *ast.BinaryExpr:
		prefix, ok := linkPattern(v.X)
		if !ok || v.Op != token.ADD {
			return "", false
		}

		return prefix + ":param", true
	}

	return "", false
}

// consoleRoutePatterns reads the router's paths, children joined under
// the layout at "/".
func consoleRoutePatterns(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(consoleSrc(t), "router", "index.ts"))
	if err != nil {
		t.Fatal(err)
	}

	var out []string
	for _, m := range consoleRoutePath.FindAllStringSubmatch(string(b), -1) {
		p := m[1]
		if !strings.HasPrefix(p, "/") {
			p = "/" + p
		}

		out = append(out, p)
	}

	if len(out) < 20 {
		t.Fatalf("only found %d console routes - the parse is broken", len(out))
	}

	return out
}

func matchesAnyRoute(link string, routes []string) bool {
	segs := strings.Split(strings.Trim(link, "/"), "/")
	for _, r := range routes {
		if strings.Contains(r, "pathMatch") {
			continue
		}

		rs := strings.Split(strings.Trim(r, "/"), "/")
		if len(rs) != len(segs) {
			continue
		}

		ok := true
		for i := range rs {
			if strings.HasPrefix(rs[i], ":") || strings.HasPrefix(segs[i], ":") {
				continue
			}

			if rs[i] != segs[i] {
				ok = false

				break
			}
		}

		if ok {
			return true
		}
	}

	return false
}
