// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package render

import (
	"regexp"
	"strings"
)

// Normalize lets a template author write {{ name }} where Go wants
// {{ .name }}.
//
// The dot means "a field of the value passed in", and every template
// here is executed against one flat map, so it is true of every field
// reference in every template - which makes it pure ceremony for the
// person writing one. They are writing marketing copy, not Go.
//
// WHAT IS LEFT ALONE MATTERS MORE THAN WHAT IS REWRITTEN. A token that
// already resolves - `.field`, `$var`, `.` itself - is somebody who knows
// the syntax and must not be second-guessed. A keyword is the template
// language, not data. And anything with an operator, a pipe or a call in
// it is beyond what this can safely rearrange, so it passes through
// untouched and Go reports the error if there is one.
//
//	{{ name }}            -> {{ .name }}
//	{{ range features }}  -> {{ range .features }}
//	{{ if active }}       -> {{ if .active }}
//	{{- name -}}          -> {{- .name -}}      trim markers survive
//	{{ .name }} {{ $x }}  -> unchanged
//	{{ end }} {{ else }}  -> unchanged
//	{{ len .xs | printf }} -> unchanged
func Normalize(s string) string {
	return action.ReplaceAllStringFunc(s, func(match string) string {
		parts := action.FindStringSubmatch(match)
		if parts == nil {
			return match
		}

		openTrim, inner, closeTrim := parts[1], parts[2], parts[3]

		return "{{" + openTrim + " " + dotFields(inner) + " " + closeTrim + "}}"
	})
}

// action matches one {{ ... }} block, capturing the trim markers so they
// can be put back, and the inner text so it can be rewritten.
var action = regexp.MustCompile(`\{\{(-?)\s*(.*?)\s*(-?)\}\}`)

// bareIdentifier is a name with nothing in front of it - the only shape
// that can be safely dotted.
var bareIdentifier = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// keywords is the template language itself: control words, builtin
// functions and literals. Dotting any of these would turn a working
// template into one that looks up a field nobody has.
var keywords = map[string]bool{
	// Control.
	"if": true, "else": true, "end": true, "range": true, "with": true,
	"template": true, "block": true, "define": true,
	// Literals.
	"nil": true, "true": true, "false": true,
	// Builtin functions.
	"and": true, "call": true, "html": true, "index": true, "js": true,
	"len": true, "not": true, "or": true, "print": true, "printf": true,
	"println": true, "urlquery": true,
	// Comparison.
	"eq": true, "ge": true, "gt": true, "le": true, "lt": true, "ne": true,
}

// dotFields rewrites the inside of one action.
//
// Only two arrangements are touched, and both are ones where every token
// after the first is unambiguously a field name: a lone token, and a
// keyword followed by its arguments. Anything else - two bare words, a
// pipeline, a method call - is left exactly as written, because guessing
// which half of it is data is how a normalizer breaks a template that
// worked.
func dotFields(inner string) string {
	tokens := strings.Fields(inner)
	if len(tokens) == 0 {
		return inner
	}

	if len(tokens) == 1 {
		return dotted(tokens[0])
	}

	if !keywords[tokens[0]] {
		return inner
	}

	out := make([]string, len(tokens))
	out[0] = tokens[0]
	for i, tok := range tokens[1:] {
		out[i+1] = dotted(tok)
	}

	return strings.Join(out, " ")
}

// dotted prefixes a bare field name, and answers anything else unchanged.
func dotted(token string) string {
	if strings.HasPrefix(token, ".") || strings.HasPrefix(token, "$") {
		return token
	}

	if keywords[token] || !bareIdentifier.MatchString(token) {
		return token
	}

	return "." + token
}
