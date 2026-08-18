// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package apidoc

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// Route is one documented endpoint. Domains declare these next to
// their handlers, where the request and response types are in scope -
// including the unexported ones, which is the reason the metadata
// does not live in routes.go.
type Route struct {
	// Method is the HTTP verb, upper case.
	Method string

	// Path is relative to /api/v1 and uses Fiber's :param form. It is
	// rewritten to OpenAPI's {param} here, so the metadata can be
	// compared to routes.go verbatim.
	Path string

	Tag         string
	Summary     string
	Description string

	// Permission is the `resource:action` the route requires,
	// rendered into the description so a reader does not have to look
	// it up.
	//
	// It was Scope, naming one of seven strings that no longer exist -
	// and 35 operations went on publishing "requires scope read" to
	// every integrator who downloaded this document, because nothing
	// checked the PROSE against what the router enforces. That is what
	// TestDocumentedPermissionsMatchTheRouter is for.
	Permission string

	Query      []Param
	PathParams []Param

	// Request is a zero value of the request body type, or nil.
	Request any

	// Responses are the documented outcomes. The first 2xx is the
	// success case.
	Responses []Response
}

// Param is one query or path parameter.
type Param struct {
	Name        string
	Description string
	Required    bool

	// Type is the OpenAPI primitive: string, integer, boolean.
	Type string
	Enum []string

	// Format is optional (uuid, date, date-time).
	Format string
}

// Response is one documented outcome.
type Response struct {
	Status      int
	Description string

	// Body is a zero value of the response type. Nil means no body,
	// which is what a 204 has.
	Body any

	// ContentType defaults to application/json.
	ContentType string
}

// Info is the document header.
type Info struct {
	Title       string
	Description string
	Version     string
	ServerURL   string
}

// Build assembles the OpenAPI document from the declared routes.
//
// It returns a plain map so the caller decides the encoding. Errors
// name the offending route rather than failing silently: a route
// whose types cannot be reflected is a bug to fix, not a gap to ship.
func Build(info Info, routes []Route) (map[string]any, error) {
	reg := &registry{defs: map[string]Schema{}, seen: map[reflect.Type]string{}}
	paths := map[string]any{}

	sorted := make([]Route, len(routes))
	copy(sorted, routes)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Path != sorted[j].Path {
			return sorted[i].Path < sorted[j].Path
		}

		return sorted[i].Method < sorted[j].Method
	})

	for _, rt := range sorted {
		op, err := reg.operation(rt)
		if err != nil {
			return nil, fmt.Errorf("%s %s: %w", rt.Method, rt.Path, err)
		}

		key := openAPIPath(rt.Path)
		item, _ := paths[key].(map[string]any)
		if item == nil {
			item = map[string]any{}
			paths[key] = item
		}

		item[strings.ToLower(rt.Method)] = op
	}

	schemas := map[string]any{}
	for name, s := range reg.defs {
		schemas[name] = s
	}

	return map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":       info.Title,
			"description": info.Description,
			"version":     info.Version,
		},
		"servers":  []any{map[string]any{"url": info.ServerURL}},
		"security": []any{map[string]any{"apiKey": []any{}}},
		"paths":    paths,
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"apiKey": map[string]any{
					"type":        "http",
					"scheme":      "bearer",
					"description": "An API key (myk_...) created under Developers - API Keys.",
				},
			},
			"schemas": schemas,
		},
	}, nil
}

func (r *registry) operation(rt Route) (map[string]any, error) {
	op := map[string]any{}
	if rt.Tag != "" {
		op["tags"] = []any{rt.Tag}
	}

	if rt.Summary != "" {
		op["summary"] = rt.Summary
	}

	if d := describe(rt); d != "" {
		op["description"] = d
	}

	var params []any
	for _, p := range rt.PathParams {
		params = append(params, param(p, "path", true))
	}

	for _, p := range rt.Query {
		params = append(params, param(p, "query", p.Required))
	}

	if len(params) > 0 {
		op["parameters"] = params
	}

	if rt.Request != nil {
		t, err := typeOf(rt.Request)
		if err != nil {
			return nil, err
		}

		op["requestBody"] = map[string]any{
			"required": true,
			"content": map[string]any{
				"application/json": map[string]any{"schema": r.SchemaFor(t)},
			},
		}
	}

	responses := map[string]any{}
	for _, resp := range rt.Responses {
		entry := map[string]any{"description": resp.Description}
		// A byte stream has no Go type to reflect, so it is declared by
		// content type alone and rendered as OpenAPI's binary string.
		// Without this branch such a response emitted no content at all,
		// which reads as "200, empty body" - see apidoc.OctetStream.
		if resp.IsBinary() {
			entry["content"] = map[string]any{
				resp.ContentType: map[string]any{
					"schema": map[string]any{"type": "string", "format": "binary"},
				},
			}
		}

		if resp.Body != nil {
			t, err := typeOf(resp.Body)
			if err != nil {
				return nil, err
			}

			ct := resp.ContentType
			if ct == "" {
				ct = "application/json"
			}

			entry["content"] = map[string]any{ct: map[string]any{"schema": r.SchemaFor(t)}}
		}

		responses[fmt.Sprint(resp.Status)] = entry
	}

	if len(responses) == 0 {
		return nil, fmt.Errorf("no responses declared")
	}

	op["responses"] = responses

	return op, nil
}

// describe folds the required scope into the prose, so every
// operation states it without the author repeating themselves.
func describe(rt Route) string {
	var b strings.Builder
	if rt.Permission != "" {
		fmt.Fprintf(&b, "Requires the `%s` permission.", rt.Permission)
	}

	if rt.Description != "" {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}

		b.WriteString(rt.Description)
	}

	return b.String()
}

func param(p Param, in string, required bool) map[string]any {
	typ := p.Type
	if typ == "" {
		typ = "string"
	}

	schema := map[string]any{"type": typ}
	if len(p.Enum) > 0 {
		schema["enum"] = p.Enum
	}

	if p.Format != "" {
		schema["format"] = p.Format
	}

	out := map[string]any{"name": p.Name, "in": in, "schema": schema}
	if required {
		out["required"] = true
	}

	if p.Description != "" {
		out["description"] = p.Description
	}

	return out
}

// NormalizePath drops the trailing slash a route group's root
// registration carries, so "/templates/" and "/templates" are one
// path.
//
// Fiber wants "/" as the path of a group's own root, which makes the
// registered string "/templates/". Publishing that verbatim gives a
// generator two endpoints where the server has one, and the client it
// emits calls a URL that only works because of a redirect. The
// documented form is the one without it.
func NormalizePath(p string) string {
	if len(p) > 1 && strings.HasSuffix(p, "/") {
		return strings.TrimSuffix(p, "/")
	}

	return p
}

// openAPIPath rewrites Fiber's :param into OpenAPI's {param}.
func openAPIPath(p string) string {
	segs := strings.Split(NormalizePath(p), "/")
	for i, s := range segs {
		if name, ok := strings.CutPrefix(s, ":"); ok {
			segs[i] = "{" + name + "}"
		}
	}

	return strings.Join(segs, "/")
}
