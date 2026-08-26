// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package sdkgen writes the generated half of the Go client.
//
// The client is two halves on purpose. sdk/go is hand-written and
// small - sending, batching, typed errors, cursor paging - where a
// good signature beats uniformity. sdk/go/api is this package's output
// and covers every /api/v1 route, because hand-writing two hundred is
// how a client falls behind its server.
//
// Two PACKAGES so generated type names cannot collide with the curated ones.
// The handwritten client exposes the other through Client.API().
//
// Source of truth is openapi.Routes(), the same metadata the document
// is built from.
//
// A library and not only a command, so a test can regenerate in memory
// and compare - a generator nobody reruns silently stops matching the server.
package sdkgen

import (
	"fmt"
	"go/format"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/apidoc"
	"github.com/yousysadmin/mailyard/internal/openapi"
)

// Render builds the generated files, keyed by base name.
func Render() (map[string]string, error) {
	g := &generator{
		types:    map[string]string{},
		emitted:  map[reflect.Type]string{},
		takenBy:  map[string]reflect.Type{},
		imported: map[string]bool{},
	}

	routes := openapi.Routes()
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Path != routes[j].Path {
			return routes[i].Path < routes[j].Path
		}

		return routes[i].Method < routes[j].Method
	})

	methods := make([]method, 0, len(routes))
	for _, r := range routes {
		m, ok := g.method(r)
		if !ok {
			continue
		}

		methods = append(methods, m)
	}

	g.uniquifyMethodNames(methods)

	out := map[string]string{}
	for name, src := range map[string]string{
		"types.go":   g.renderTypes(),
		"methods.go": g.renderMethods(methods),
	} {
		formatted, err := format.Source([]byte(src))
		if err != nil {
			return map[string]string{name: src}, fmt.Errorf("formatting %s: %w", name, err)
		}

		out[name] = string(formatted)
	}

	return out, nil
}

// Dir is where Render's output belongs, relative to the repository
// root. Named here so the command and the freshness test cannot
// disagree about it.
const Dir = "sdk/go/api"

// ----------------------------------------------------------------------------
// Types
// ----------------------------------------------------------------------------
type generator struct {
	// types maps an emitted name to its declaration.
	types map[string]string

	// emitted maps a Go type to the name it was given.
	emitted map[reflect.Type]string

	// takenBy resolves name collisions across packages.
	takenBy map[string]reflect.Type

	// imported records which standard imports the output needs.
	imported map[string]bool
}

var timeType = reflect.TypeFor[time.Time]()

// typeName returns the SDK name for t, emitting its declaration the
// first time it is seen.
func (g *generator) typeName(t reflect.Type) string {
	if name, ok := g.emitted[t]; ok {
		return name
	}

	name := g.reserve(t)

	// Reserved before the fields are walked, so a self-referential type
	// finds its own name instead of recursing forever.
	g.emitted[t] = name
	g.types[name] = ""
	g.types[name] = g.structDecl(name, t)

	return name
}

// reserve picks an exported, unique name for t.
//
// Unexported request DTOs (createInput, sendInput) are the common case
// and simply get a capital letter. Two packages exporting the same name
// are qualified with the package, which is what the OpenAPI reflector
// does for the same reason.
func (g *generator) reserve(t reflect.Type) string {
	name := exported(t.Name())
	if taken, exists := g.takenBy[name]; (exists && taken != t) || handWritten[name] {
		name = exported(pkgOf(t)) + name
	}

	for i := 2; ; i++ {
		taken, exists := g.takenBy[name]
		if !exists || taken == t {
			break
		}

		name = fmt.Sprintf("%s%d", name, i)
	}

	g.takenBy[name] = t

	return name
}

// handWritten are the identifiers client.go already declares in the
// generated package.
//
// A generated type taking one of these names does not produce a bad
// name - it produces a package that does not COMPILE, which is how a
// type called Project met an option called Project. Qualifying by
// package instead is invisible to anyone who was not going to hit it.
var handWritten = map[string]bool{
	"Client": true, "New": true, "ClientOption": true,
	"WithHTTPClient": true, "WithUserAgent": true, "WithProject": true,
	"Error": true, "FieldError": true, "RequestOption": true,
	"Query": true, "Header": true, "ForProject": true,
	"escape": true, "do": true,
}

func pkgOf(t reflect.Type) string {
	pkg := t.PkgPath()
	if _, base, ok := strings.CutLast(pkg, "/"); ok {
		pkg = base
	}

	return pkg
}

func exported(s string) string {
	if s == "" {
		return ""
	}

	return strings.ToUpper(s[:1]) + s[1:]
}

func (g *generator) structDecl(name string, t reflect.Type) string {
	var b strings.Builder
	fmt.Fprintf(&b, "// %s is the %s body.\ntype %s struct {\n", name, describeKind(name), name)
	g.fields(&b, t, "\t")
	b.WriteString("}\n")

	return b.String()
}

func describeKind(name string) string {
	switch {
	case strings.HasSuffix(name, "Input"), strings.HasSuffix(name, "Request"):
		return "request"
	case strings.HasSuffix(name, "Response"), strings.HasSuffix(name, "Output"):
		return "response"
	default:
		return "wire"
	}
}

func (g *generator) fields(b *strings.Builder, t reflect.Type, indent string) {
	for f := range t.Fields() {
		if f.PkgPath != "" && !f.Anonymous {
			continue // unexported
		}

		tag := f.Tag.Get("json")
		nameTag, _, _ := strings.Cut(tag, ",")
		if nameTag == "-" {
			continue
		}

		if f.Anonymous && nameTag == "" {
			// An embedded struct is flattened into the wire object, so
			// flatten it here too.
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}

			if ft.Kind() == reflect.Struct {
				g.fields(b, ft, indent)
				continue
			}
		}

		// A field with no json tag would emit `json:""`, which is not
		// what the server marshals it as. Fall back to the field name.
		if tag == "" {
			tag = f.Name
		}

		fmt.Fprintf(b, "%s%s %s `json:%q`\n", indent, exported(f.Name), g.goType(f.Type), tag)
	}
}

// goType maps a reflected type onto the Go source the client uses.
func (g *generator) goType(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Pointer:
		return "*" + g.goType(t.Elem())
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return "[]byte"
		}

		return "[]" + g.goType(t.Elem())
	case reflect.Map:
		return "map[" + g.goType(t.Key()) + "]" + g.goType(t.Elem())
	case reflect.Interface:
		return "any"
	case reflect.Struct:
		if t == timeType {
			g.imported["time"] = true

			return "time.Time"
		}

		if t.Name() == "" {
			// Anonymous struct: inline it rather than inventing a name
			// nobody wrote.
			var b strings.Builder
			b.WriteString("struct {\n")
			g.fields(&b, t, "\t\t")
			b.WriteString("\t}")

			return b.String()
		}

		return g.typeName(t)
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "bool"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return "int64"
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "uint64"
	case reflect.Float32, reflect.Float64:
		return "float64"
	default:
		return "any"
	}
}

// ----------------------------------------------------------------------------
// Methods
// ----------------------------------------------------------------------------
type method struct {
	Name       string
	HTTPMethod string

	// Path is the Go format string, with %s where a parameter goes.
	Path       string
	PathParams []string
	Body       string // "" when the route takes none

	// Raw marks a route answering bytes rather than JSON - a raw
	// message, a decoded attachment. Such a method returns []byte and
	// goes through doRaw, because json.Unmarshal over an RFC 5322
	// message fails and the payload the call exists to fetch is lost.
	Raw    bool
	Result string // "" when the route answers 204
	Doc    string
	route  apidoc.Route
}

func (g *generator) method(r apidoc.Route) (method, bool) {
	m := method{HTTPMethod: r.Method, route: r}

	// Path and its parameters.
	var formatt []string
	for seg := range strings.SplitSeq(strings.Trim(r.Path, "/"), "/") {
		if seg == "" {
			continue
		}

		if strings.HasPrefix(seg, ":") {
			formatt = append(formatt, "%s")
			m.PathParams = append(m.PathParams, argName(strings.TrimPrefix(seg, ":")))
			continue
		}

		formatt = append(formatt, seg)
	}

	m.Path = "/" + strings.Join(formatt, "/")

	if r.Request != nil {
		m.Body = g.typeName(reflect.TypeOf(r.Request))
	}

	for _, resp := range r.Responses {
		if resp.Status < 200 || resp.Status >= 300 {
			continue
		}

		if resp.IsBytes() {
			m.Raw = true
			m.Result = "[]byte"

			break
		}

		if resp.Body == nil {
			continue
		}

		m.Result = g.typeName(reflect.TypeOf(resp.Body))

		break
	}

	m.Name = methodName(r)
	m.Doc = methodDoc(r)

	return m, true
}

// argName turns a path parameter into a Go identifier.
func argName(s string) string {
	s = strings.ReplaceAll(s, "-", "_")
	parts := strings.Split(s, "_")
	for i := 1; i < len(parts); i++ {
		parts[i] = exported(parts[i])
	}

	name := strings.Join(parts, "")
	switch name {
	case "type", "range", "func", "map", "len", "cap":
		return name + "Param"
	}

	return name
}

// methodName derives a readable name from the verb and the path.
//
// The rules are the ones a person would use reading the route aloud:
// a GET on a collection LISTS, a GET on one thing GETS it, a trailing
// literal segment is the ACTION (send, retry, revoke). Anything the
// rules cannot separate is disambiguated afterwards by
// uniquifyMethodNames rather than by making the rules cleverer.
func methodName(r apidoc.Route) string {
	segs := strings.Split(strings.Trim(r.Path, "/"), "/")
	var literals []string
	lastIsParam := false
	for _, s := range segs {
		if s == "" {
			continue
		}

		if strings.HasPrefix(s, ":") {
			lastIsParam = true
			continue
		}

		lastIsParam = false
		literals = append(literals, s)
	}

	// /admin is a namespace, not a resource. Left in place it makes
	// every platform route read as an operation on a thing called
	// "admin" - ProjectsAdminUsers rather than GetAdminUserProjects.
	prefix := ""
	if len(literals) > 0 && literals[0] == "admin" {
		prefix = "Admin"
		literals = literals[1:]
	}

	if len(literals) == 0 {
		return exported(strings.ToLower(r.Method)) + prefix
	}

	head := camel(literals[0])
	tail := literals[1:]
	tailName := camel(strings.Join(tail, "-"))

	// The addressed collection, singular: /templates/:id/versions/:vid
	// is one VERSION, however the segment is spelled.
	tailOne := tailName
	if len(tail) > 0 {
		tailOne = camel(strings.Join(tail[:len(tail)-1], "-")) + camel(singular(tail[len(tail)-1]))
	}

	hasParam := strings.Contains(r.Path, ":")
	subject := head
	if hasParam {
		subject = singular(head)
	}

	// A trailing literal is either a sub-COLLECTION or an ACTION, and
	// the difference is not the HTTP method - POST /templates/:id/versions
	// creates a version while POST /campaigns/:id/send sends. A plural
	// separates them: every action this API has is a verb (send, retry,
	// revoke, approve, run) and no verb here ends in s.
	collection := len(tail) > 0 && strings.HasSuffix(tail[len(tail)-1], "s")

	switch r.Method {
	case "GET":
		if lastIsParam {
			return "Get" + prefix + subject + tailOne
		}

		if collection {
			return "List" + prefix + subject + tailName
		}

		if len(tail) > 0 {
			return "Get" + prefix + subject + tailName
		}

		return "List" + prefix + head
	case "POST":
		if len(tail) == 0 {
			return "Create" + prefix + singular(head)
		}

		if collection {
			return "Create" + prefix + subject + tailOne
		}

		// The whole tail is the verb, so /subscribers/import/csv reads
		// ImportCsvSubscriber rather than CsvSubscriberImport.
		return tailName + prefix + singular(head)
	case "PATCH", "PUT":
		return "Update" + prefix + subject + tailOne
	case "DELETE":
		return "Delete" + prefix + subject + tailOne
	}

	return exported(strings.ToLower(r.Method)) + prefix + head + tailName
}

// singular strips the plural from the segment a path parameter hangs
// off, so /templates/:id reads GetTemplate rather than GetTemplates.
//
// Deliberately naive - it handles the shapes this API actually has
// (templates, campaigns, api-keys, subscriber-lists) and nothing else.
// A real Inflector would be a dependency and a source of surprises.
func singular(s string) string {
	switch {
	case strings.HasSuffix(s, "ies"):
		return strings.TrimSuffix(s, "ies") + "y"
	case strings.HasSuffix(s, "sses"), strings.HasSuffix(s, "ches"):
		return strings.TrimSuffix(s, "es")
	case strings.HasSuffix(s, "ss"):
		return s
	case strings.HasSuffix(s, "s"):
		return strings.TrimSuffix(s, "s")
	}

	return s
}

func camel(s string) string {
	if s == "" {
		return ""
	}

	var b strings.Builder
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == '-' || r == '_' || r == '/' }) {
		b.WriteString(exported(part))
	}

	return b.String()
}

// uniquifyMethodNames resolves the collisions the naming rules leave.
//
// It appends path context rather than a number: DeleteTemplateVersion
// beats DeleteTemplate2, and a caller reading it can tell which route
// they are on.
func (g *generator) uniquifyMethodNames(ms []method) {
	seen := map[string]int{}
	for i := range ms {
		name := ms[i].Name
		if seen[name] == 0 {
			seen[name]++
			continue
		}

		// Rebuild with every literal segment, then fall back to a
		// suffix if even that repeats.
		var parts []string
		for s := range strings.SplitSeq(strings.Trim(ms[i].route.Path, "/"), "/") {
			if s != "" && !strings.HasPrefix(s, ":") {
				parts = append(parts, camel(s))
			}
		}

		alt := exported(strings.ToLower(ms[i].HTTPMethod)) + strings.Join(parts, "")
		for n := 2; seen[alt] > 0; n++ {
			alt = fmt.Sprintf("%s%d", alt, n)
		}

		seen[alt]++
		ms[i].Name = alt
	}
}

func methodDoc(r apidoc.Route) string {
	summary := strings.TrimSpace(r.Summary)
	if summary == "" {
		summary = r.Method + " " + r.Path
	}

	line := summary
	if !strings.HasSuffix(line, ".") {
		line += "."
	}

	return line + "\n//\n// " + r.Method + " " + r.Path
}

// ----------------------------------------------------------------------------
// Rendering
// ----------------------------------------------------------------------------
const header = `
// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Code generated by cmd/sdkgen. DO NOT EDIT.
`

func (g *generator) renderTypes() string {
	names := make([]string, 0, len(g.types))
	for n := range g.types {
		names = append(names, n)
	}

	slices.Sort(names)

	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\npackage api\n\n")
	if g.imported["time"] {
		b.WriteString("import \"time\"\n\n")
	}

	for _, n := range names {
		b.WriteString(g.types[n])
		b.WriteString("\n")
	}

	return b.String()
}

func (g *generator) renderMethods(ms []method) string {
	var b strings.Builder
	b.WriteString(header)
	b.WriteString(`
package api

import (
	"context"
	"fmt"
)

`)
	for _, m := range ms {
		args := []string{"ctx context.Context"}
		for _, p := range m.PathParams {
			args = append(args, p+" string")
		}

		if m.Body != "" {
			args = append(args, "body "+m.Body)
		}

		args = append(args, "opts ...RequestOption")

		path := strconv(m.Path)
		if len(m.PathParams) > 0 {
			path = "fmt.Sprintf(" + strconv(m.Path) + ", " + strings.Join(escaped(m.PathParams), ", ") + ")"
		}

		fmt.Fprintf(&b, "// %s %s\n", m.Name, m.Doc)
		switch {
		case m.Raw:
			fmt.Fprintf(&b, "func (c *Client) %s(%s) ([]byte, error) {\n", m.Name, strings.Join(args, ", "))
			fmt.Fprintf(&b, "\treturn doRaw(ctx, c, %q, %s, opts)\n}\n\n", m.HTTPMethod, path)
		case m.Result == "":
			fmt.Fprintf(&b, "func (c *Client) %s(%s) error {\n", m.Name, strings.Join(args, ", "))
			if m.Body != "" {
				fmt.Fprintf(&b, "\t_, err := do[struct{}](ctx, c, %q, %s, body, opts)\n", m.HTTPMethod, path)
			} else {
				fmt.Fprintf(&b, "\t_, err := do[struct{}](ctx, c, %q, %s, nil, opts)\n", m.HTTPMethod, path)
			}

			b.WriteString("\treturn err\n}\n\n")
		default:
			fmt.Fprintf(&b, "func (c *Client) %s(%s) (%s, error) {\n", m.Name, strings.Join(args, ", "), m.Result)
			if m.Body != "" {
				fmt.Fprintf(&b, "\treturn do[%s](ctx, c, %q, %s, body, opts)\n", m.Result, m.HTTPMethod, path)
			} else {
				fmt.Fprintf(&b, "\treturn do[%s](ctx, c, %q, %s, nil, opts)\n", m.Result, m.HTTPMethod, path)
			}

			b.WriteString("}\n\n")
		}
	}

	return b.String()
}

func escaped(params []string) []string {
	out := make([]string, len(params))
	for i, p := range params {
		out[i] = "escape(" + p + ")"
	}

	return out
}

func strconv(s string) string { return fmt.Sprintf("%q", s) }
