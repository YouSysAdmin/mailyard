// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package render

import (
	"slices"
	"strings"
	"text/template/parse"
)

// Normalize lets a template author write {{ name }} where Go wants
// {{ .name }}.
//
// The dot means "a field of the value passed in", and every template
// here is executed against one flat map, so it is true of every field
// reference in every template - which makes it pure ceremony for the
// person writing one. They are writing marketing copy, not Go.
//
// The source is parsed with Go's own parser, function checks off, and
// every identifier that names no function gets a dot inserted in front
// of it. Nothing else in the text moves, so trim markers, comments and
// line numbers in error messages are the author's own. A source that
// does not parse is answered unchanged and the real parse reports it.
//
//	{{ name }}                    -> {{ .name }}
//	{{ range $i, $x := items }}   -> {{ range $i, $x := .items }}
//	{{ if gt (len items) 1 }}     -> {{ if gt (len .items) 1 }}
//	{{ slice name 0 2 }}          -> {{ slice .name 0 2 }}
//	{{ user.first }}              -> {{ .user.first }}
//	{{ .name }} {{ $x }} {{ . }}  -> unchanged
//	{{ break }} {{ continue }}    -> unchanged
func Normalize(s string) string {
	t := parse.New("t")
	t.Mode = parse.SkipFuncCheck
	set := map[string]*parse.Tree{}
	if _, err := t.Parse(s, "", "", set); err != nil {
		return s
	}

	var at []int
	for _, tree := range set {
		if tree != nil {
			at = bareFields(tree.Root, s, at)
		}
	}

	if len(at) == 0 {
		return s
	}

	slices.Sort(at)
	at = slices.Compact(at)

	var b strings.Builder
	b.Grow(len(s) + len(at))
	prev := 0
	for _, p := range at {
		b.WriteString(s[prev:p])
		b.WriteByte('.')
		prev = p
	}

	b.WriteString(s[prev:])

	return b.String()
}

// functions is every name a template can call: the builtins plus what
// plant and the comparisons add. Anything else is data.
var functions = map[string]bool{
	"and": true, "call": true, "html": true, "index": true, "js": true,
	"len": true, "not": true, "or": true, "print": true, "printf": true,
	"println": true, "slice": true, "urlquery": true,
	"eq": true, "ge": true, "gt": true, "le": true, "lt": true, "ne": true,
	budgetFunc: true, digFunc: true,
}

// bareFields collects the offsets of identifiers that name no function.
func bareFields(n parse.Node, src string, at []int) []int {
	switch n := n.(type) {
	case *parse.ListNode:
		if n == nil {
			return at
		}

		for _, c := range n.Nodes {
			at = bareFields(c, src, at)
		}
	case *parse.ActionNode:
		at = bareFields(n.Pipe, src, at)
	case *parse.IfNode:
		at = bareBranch(&n.BranchNode, src, at)
	case *parse.RangeNode:
		at = bareBranch(&n.BranchNode, src, at)
	case *parse.WithNode:
		at = bareBranch(&n.BranchNode, src, at)
	case *parse.TemplateNode:
		at = bareFields(n.Pipe, src, at)
	case *parse.PipeNode:
		if n == nil {
			return at
		}

		for _, cmd := range n.Cmds {
			for _, arg := range cmd.Args {
				at = bareFields(arg, src, at)
			}
		}
	case *parse.ChainNode:
		at = bareFields(n.Node, src, at)
	case *parse.IdentifierNode:
		p := int(n.Pos)
		if !functions[n.Ident] && p < len(src) && strings.HasPrefix(src[p:], n.Ident) {
			at = append(at, p)
		}
	}

	return at
}

func bareBranch(n *parse.BranchNode, src string, at []int) []int {
	at = bareFields(n.Pipe, src, at)
	at = bareFields(n.List, src, at)

	return bareFields(n.ElseList, src, at)
}
