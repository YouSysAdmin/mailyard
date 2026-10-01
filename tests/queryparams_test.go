// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tests

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/apidoc"
	"github.com/yousysadmin/mailyard/internal/openapi"
)

const modulePath = "github.com/yousysadmin/mailyard"

// TestEveryQueryParameterIsDocumented pins the Query list of every
// documented route to the query string its handler actually reads.
//
// The document's bodies are reflected from the response types, and
// its paths and permissions are checked against the router, but the
// query parameters are a hand-kept list with nothing behind it. A
// handler that pages through paging.From accepts limit and offset
// whether or not the entry says so, and a reader building a client
// from the entry does not know.
//
// What a handler reads is collected from its body and from the
// helpers it calls in its own package: c.Query, fiber.Query,
// paging.Search, and the paging readers - From stands for limit and
// offset, WindowFrom for limit and cursor, From(c).Limit for the
// limit alone.
// Both directions fail: a parameter read but not listed, and a
// parameter listed that nothing reads.
func TestEveryQueryParameterIsDocumented(t *testing.T) {
	handlers := routeHandlers(t)
	if len(handlers) == 0 {
		t.Fatal("found no route registrations with a resolvable handler")
	}

	reads := map[string]*packageReads{}
	readsOf := func(h routeHandler) ([]string, bool) {
		pr, ok := reads[h.pkgDir]
		if !ok {
			pr = parsePackageReads(t, h.pkgDir)
			reads[h.pkgDir] = pr
		}

		return pr.closure(h.typeName + "." + h.method)
	}

	documented := map[string]map[string]bool{}
	for _, r := range openapi.Routes() {
		documented["product "+r.Method+" /api/v1"+apidoc.NormalizePath(r.Path)] = paramNames(r.Query)
	}

	for _, r := range openapi.ConsoleRoutes() {
		documented["console "+r.Method+" "+apidoc.NormalizePath(r.Path)] = paramNames(r.Query)
	}

	var seen int
	for _, h := range handlers {
		want, ok := readsOf(h)
		if !ok {
			t.Errorf("%s: %s.%s was not found in %s", h.route, h.typeName, h.method, h.pkgDir)

			continue
		}

		for _, doc := range []string{"product", "console"} {
			got, ok := documented[doc+" "+h.route]
			if !ok {
				continue
			}

			seen++
			for _, name := range want {
				if !got[name] {
					t.Errorf("%s document, %s: the handler reads ?%s= but the entry's Query does not list it",
						doc, h.route, name)
				}
			}

			for name := range got {
				if !slices.Contains(want, name) {
					t.Errorf("%s document, %s: the entry lists ?%s= but %s.%s never reads it",
						doc, h.route, name, h.typeName, h.method)
				}
			}
		}
	}

	if seen == 0 {
		t.Fatal("no registered route matched a documented one - the key shapes have drifted apart")
	}
}

func paramNames(ps []apidoc.Param) map[string]bool {
	out := map[string]bool{}
	for _, p := range ps {
		out[p.Name] = true
	}

	return out
}

// routeHandler is one registration whose handler is a method on a
// domain type: `sb.Get("/", permRead, sbh.List)` with
// `sbh := &sandbox.Handler{...}` above it.
type routeHandler struct {
	route    string
	pkgDir   string
	typeName string
	method   string
}

// routeHandlers resolves every registration in the router files to
// the package and method behind it. A handler that is a closure, a
// wrapper call or a function of the server package itself is skipped:
// those are the probes, the spec download and the static mounts, none
// of which pages.
func routeHandlers(t *testing.T) []routeHandler {
	t.Helper()
	fset, files := parseRouter(t, 0)

	var out []routeHandler
	for _, file := range files {
		prefix := groupPrefixes(file)
		vars := handlerVars(file)
		imports := importPaths(file)
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
			if !ok || len(call.Args) < 2 {
				return true
			}

			group, known := prefix[recv.Name]
			if !known {
				return true
			}

			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}

			handler, ok := call.Args[len(call.Args)-1].(*ast.SelectorExpr)
			if !ok {
				return true
			}

			hv, ok := handler.X.(*ast.Ident)
			if !ok {
				return true
			}

			typ, ok := vars[hv.Name]
			if !ok {
				return true
			}

			importPath, ok := imports[typ.pkg]
			if !ok || !strings.HasPrefix(importPath, modulePath+"/") {
				t.Errorf("%s: cannot place package %s", fset.Position(call.Pos()), typ.pkg)

				return true
			}

			p, _ := strconv.Unquote(lit.Value)
			out = append(out, routeHandler{
				route:    strings.ToUpper(sel.Sel.Name) + " " + apidoc.NormalizePath(group+p),
				pkgDir:   filepath.Join(repoRoot(t), filepath.FromSlash(strings.TrimPrefix(importPath, modulePath+"/"))),
				typeName: typ.name,
				method:   handler.Sel.Name,
			})

			return true
		})
	}

	return out
}

type handlerType struct {
	pkg  string
	name string
}

// handlerVars maps a local variable to the domain type it holds,
// from `x := &pkg.Type{...}` and `x := pkg.NewType(...)`.
func handlerVars(file *ast.File) map[string]handlerType {
	out := map[string]handlerType{}
	ast.Inspect(file, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
			return true
		}

		name, ok := as.Lhs[0].(*ast.Ident)
		if !ok {
			return true
		}

		switch rhs := as.Rhs[0].(type) {
		case *ast.UnaryExpr:
			lit, ok := rhs.X.(*ast.CompositeLit)
			if !ok {
				return true
			}

			if sel, ok := lit.Type.(*ast.SelectorExpr); ok {
				if pkg, ok := sel.X.(*ast.Ident); ok {
					out[name.Name] = handlerType{pkg: pkg.Name, name: sel.Sel.Name}
				}
			}
		case *ast.CallExpr:
			sel, ok := rhs.Fun.(*ast.SelectorExpr)
			if !ok || !strings.HasPrefix(sel.Sel.Name, "New") {
				return true
			}

			if pkg, ok := sel.X.(*ast.Ident); ok {
				out[name.Name] = handlerType{pkg: pkg.Name, name: strings.TrimPrefix(sel.Sel.Name, "New")}
			}
		}

		return true
	})

	return out
}

// importPaths maps the local name of each import to its path.
func importPaths(file *ast.File) map[string]string {
	out := map[string]string{}
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}

		name := path[strings.LastIndex(path, "/")+1:]
		if imp.Name != nil {
			name = imp.Name.Name
		}

		out[name] = path
	}

	return out
}

// packageReads is what each function of one package reads from the
// query string, directly and through the same-package functions it
// calls. Keys are "Type.Method" for methods and "func" for functions.
type packageReads struct {
	direct map[string][]string
	calls  map[string][]string
}

func parsePackageReads(t *testing.T, dir string) *packageReads {
	t.Helper()
	pr := &packageReads{direct: map[string][]string{}, calls: map[string][]string{}}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}

		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}

			key, recvName := funcKey(fn)
			direct, calls := readsIn(t, fset, fn.Body, recvName, strings.TrimSuffix(key, "."+fn.Name.Name))
			pr.direct[key] = append(pr.direct[key], direct...)
			pr.calls[key] = append(pr.calls[key], calls...)
		}
	}

	return pr
}

// funcKey names a declaration the way calls to it are resolved, and
// returns the receiver's local name for a method.
func funcKey(fn *ast.FuncDecl) (string, string) {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name, ""
	}

	recv := fn.Recv.List[0]
	typ := recv.Type
	if star, ok := typ.(*ast.StarExpr); ok {
		typ = star.X
	}

	typeName := ""
	switch x := typ.(type) {
	case *ast.Ident:
		typeName = x.Name
	case *ast.IndexExpr:
		if id, ok := x.X.(*ast.Ident); ok {
			typeName = id.Name
		}
	}

	recvName := ""
	if len(recv.Names) > 0 {
		recvName = recv.Names[0].Name
	}

	return typeName + "." + fn.Name.Name, recvName
}

// readsIn collects the query names a body reads and the same-package
// callees it reaches: a bare `name(...)` is a package function, and
// `recv.name(...)` on the method's own receiver is a sibling method.
func readsIn(t *testing.T, fset *token.FileSet, body *ast.BlockStmt, recvName, typeName string) (direct, calls []string) {
	t.Helper()
	// paging.From(c).Limit reads the limit alone, which is how a
	// keyset list sizes its page. The call is marked here, on the
	// way down, so the visit of the call itself does not also count
	// the offset.
	narrowed := map[token.Pos]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if call, ok := sel.X.(*ast.CallExpr); ok && isPagingPage(call.Fun) {
				narrowed[call.Pos()] = true
				direct = append(direct, strings.ToLower(sel.Sel.Name))

				return true
			}
		}

		call, ok := n.(*ast.CallExpr)
		if !ok || narrowed[call.Pos()] {
			return true
		}

		switch fun := call.Fun.(type) {
		case *ast.Ident:
			calls = append(calls, fun.Name)
		case *ast.IndexExpr:
			// fiber.Query[T](c, "name", ...)
			if isPkgFunc(fun.X, "fiber", "Query") && len(call.Args) >= 2 {
				direct = append(direct, literalArg(t, fset, call, 1))
			}
		case *ast.SelectorExpr:
			x, ok := fun.X.(*ast.Ident)
			if !ok {
				return true
			}

			switch {
			case recvName != "" && x.Name == recvName:
				calls = append(calls, typeName+"."+fun.Sel.Name)
			case isPagingPage(fun):
				direct = append(direct, "limit", "offset")
			case x.Name == "paging" && fun.Sel.Name == "WindowFrom":
				direct = append(direct, "limit", "cursor")
			case x.Name == "paging" && fun.Sel.Name == "CursorFrom":
				direct = append(direct, "cursor")
			case x.Name == "paging" && fun.Sel.Name == "Search" && len(call.Args) >= 2:
				direct = append(direct, literalArg(t, fset, call, 1))
			case fun.Sel.Name == "Query" && len(call.Args) >= 1:
				// c.Query("name"). A store's Query takes a context
				// first, so a literal first argument is the
				// discriminator.
				if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					s, _ := strconv.Unquote(lit.Value)
					direct = append(direct, s)
				}
			}
		}

		return true
	})

	return direct, calls
}

// isPagingPage is paging.From or paging.FromWith, the two readers of
// an offset page.
func isPagingPage(e ast.Expr) bool {
	return isPkgFunc(e, "paging", "From") || isPkgFunc(e, "paging", "FromWith")
}

func isPkgFunc(e ast.Expr, pkg, name string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	x, ok := sel.X.(*ast.Ident)

	return ok && x.Name == pkg && sel.Sel.Name == name
}

// literalArg is the string literal at position i, and a failure when
// a query name is computed: a parameter named at runtime cannot be
// checked against the document.
func literalArg(t *testing.T, fset *token.FileSet, call *ast.CallExpr, i int) string {
	t.Helper()
	lit, ok := call.Args[i].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		t.Errorf("%s: a query parameter named by an expression cannot be checked", fset.Position(call.Pos()))

		return ""
	}

	s, _ := strconv.Unquote(lit.Value)

	return s
}

// closure is the sorted, deduplicated set of names a function reads,
// following calls within the package.
func (pr *packageReads) closure(key string) ([]string, bool) {
	if _, ok := pr.direct[key]; !ok {
		return nil, false
	}

	seen := map[string]bool{}
	names := map[string]bool{}
	var walk func(k string)
	walk = func(k string) {
		if seen[k] {
			return
		}

		seen[k] = true
		for _, n := range pr.direct[k] {
			names[n] = true
		}

		for _, callee := range pr.calls[k] {
			if _, ok := pr.direct[callee]; ok {
				walk(callee)
			}
		}
	}
	walk(key)

	out := make([]string, 0, len(names))
	for n := range names {
		out = append(out, n)
	}

	slices.Sort(out)

	return out, true
}
