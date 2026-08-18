// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tests

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// sdkDir(t) is the Go client. A separate MODULE, so it is not built by
// `go build ./...` from the root and its own tests would not catch a
// route it never learned about. This test lives here, in the module
// that owns the routes, and reads those files from disk.

// sdkGenDir(t) is the generated half. Covering every route by hand stopped
// being sensible at two hundred of them, so cmd/sdkgen writes this one
// from the same metadata the OpenAPI document is built from.

// TestSDKCoversEveryV1Route pins the client to the surface.
//
// A hand-written client has one failure mode: the API grows and the
// client does not, so an integrator reads the OpenAPI document, sees
// an endpoint, and finds no method for it. The reverse is worse - a
// method calling a path that no longer exists compiles perfectly and
// fails at runtime, in production, in somebody else's service.
//
// Both directions fail here. A route the client should not expose is
// listed in notInTheSDK with the reason, which keeps the exemption
// visible rather than implied by absence.
func TestSDKCoversEveryV1Route(t *testing.T) {
	registered := v1Routes(t)
	called := sdkCalls(t)

	// Routes deliberately absent from the client.
	//
	// Empty, and it should stay that way: the generated half covers
	// every route by construction, so an entry here would mean somebody
	// excluded one by hand. It briefly held 181 entries, while the
	// product surface had moved to /api/v1 and the client had not - and
	// enumerating them was the point, because loosening the assertion
	// instead would have read ever after as "the client covers
	// everything".
	notInTheSDK := map[string]string{}

	var missing, extra []string
	for r := range registered {
		if called[normalizeParams(r)] {
			continue
		}

		if why, ok := notInTheSDK[r]; ok {
			t.Logf("not in the SDK by choice: %s (%s)", r, why)
			continue
		}

		missing = append(missing, r)
	}

	want := map[string]bool{}
	for r := range registered {
		want[normalizeParams(r)] = true
	}

	for c := range called {
		if !want[c] {
			extra = append(extra, c)
		}
	}

	sort.Strings(missing)
	sort.Strings(extra)

	for _, m := range missing {
		t.Errorf("the Go client has no method for %s - add one in sdk/go, or list it in notInTheSDK with a reason", m)
	}

	for _, e := range extra {
		t.Errorf("the Go client calls %s but routes.go registers no such route", e)
	}
}

// normalizeParams reduces ":anything" path segments to "*", so the
// client's runtime-built paths and the router's declarations compare.
func normalizeParams(route string) string {
	verb, path, _ := strings.Cut(route, " ")
	segs := strings.Split(path, "/")
	for i, s := range segs {
		if strings.HasPrefix(s, ":") {
			segs[i] = "*"
		}
	}

	return verb + " " + strings.Join(segs, "/")
}

// sdkCalls parses the client for the requests it makes.
//
// It looks for every function that reaches the network - the generic
// do[...], doRaw for the byte-stream routes, and the hand-written raw
// fetch - and reconstructs each path from the argument, turning
// anything that is not a string literal into the "*" a path parameter
// becomes.
//
// doRaw is a plain identifier call, so it matches none of the shapes the
// other two do. Adding it was not optional: the moment the attachment
// and raw-message routes started returning bytes, six real methods
// became invisible here and this test reported them as MISSING from a
// client that had just gained them.
func sdkCalls(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	fset := token.NewFileSet()
	var files []string
	for _, dir := range []string{sdkDir(t), sdkGenDir(t)} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("reading %s: %v - is the client still a separate module here?", dir, err)
		}

		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}

			files = append(files, filepath.Join(dir, e.Name()))
		}
	}

	for _, path := range files {
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			switch fn := call.Fun.(type) {
			case *ast.IndexExpr: // do[T](...)
				if id, ok := fn.X.(*ast.Ident); ok && id.Name == "do" {
					if verb, path, ok := doCall(call.Args); ok {
						out[verb+" "+path] = true
					}
				}
			case *ast.IndexListExpr: // do[T, U](...)
				if id, ok := fn.X.(*ast.Ident); ok && id.Name == "do" {
					if verb, path, ok := doCall(call.Args); ok {
						out[verb+" "+path] = true
					}
				}
			case *ast.Ident: // doRaw(ctx, c, method, path, opts)
				if fn.Name == "doRaw" {
					if verb, path, ok := doCall(call.Args); ok {
						out[verb+" "+path] = true
					}
				}
			case *ast.SelectorExpr: // c.raw(ctx, path)
				if fn.Sel.Name == "raw" && len(call.Args) == 2 {
					if p, ok := pathOf(call.Args[1]); ok {
						out["GET "+p] = true
					}
				}
			}

			return true
		})
	}

	if len(out) == 0 {
		t.Fatal("found no API calls in the client - did do() get renamed?")
	}

	return out
}

// doCall reads (ctx, c, method, path, ...) off a do[...] call.
func doCall(args []ast.Expr) (verb, path string, ok bool) {
	if len(args) < 4 {
		return "", "", false
	}

	// The hand-written half writes http.MethodGet, the generated half
	// writes "GET". Both are the verb.
	switch v := args[2].(type) {
	case *ast.SelectorExpr:
		verb = strings.ToUpper(strings.TrimPrefix(v.Sel.Name, "Method"))
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", "", false
		}

		lit, err := strconv.Unquote(v.Value)
		if err != nil {
			return "", "", false
		}

		verb = strings.ToUpper(lit)
	default:
		return "", "", false
	}

	path, ok = pathOf(args[3])

	return verb, path, ok
}

// pathOf flattens a path expression. Literals contribute their text,
// anything else contributes the "*" of a path parameter - which is
// exactly what `"/emails/" + url.PathEscape(id) + "/status"` means.
func pathOf(e ast.Expr) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}

		s, err := strconv.Unquote(v.Value)

		return s, err == nil
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}

		l, lok := pathOf(v.X)
		r, rok := pathOf(v.Y)
		if !lok || !rok {
			return "", false
		}

		return l + r, true
	case *ast.CallExpr:
		// fmt.Sprintf("/templates/%s/versions/%s", ...) - the format
		// string IS the route, with %s where the hand-written half
		// concatenates an id. The generated client builds every
		// parameterised path this way, so without this case the
		// checker sees one "*" and concludes the route is uncovered.
		if sel, ok := v.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Sprintf" && len(v.Args) > 0 {
			if format, ok := pathOf(v.Args[0]); ok {
				return strings.ReplaceAll(format, "%s", "*"), true
			}
		}

		return "*", true
	default:
		// An identifier: this is where an id goes.
		return "*", true
	}
}
