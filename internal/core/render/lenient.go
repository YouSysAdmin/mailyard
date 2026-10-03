// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package render

import (
	"reflect"
	"strconv"
	"strings"
	"text/template/parse"
)

// digFunc is the name of the call a lenient render puts in place of a
// nested field reference. Underscored for the same reason as
// budgetFunc.
const digFunc = "_dig"

// dig walks keys down from v and answers nil at the first level that
// is missing or is not a map. text/template reads `.user.first` as
// "field first of .user" and fails when .user is nil, so under
// missingkey=zero a missing parent errored where a missing leaf
// rendered blank.
func dig(v any, keys ...string) any {
	for _, k := range keys {
		rv := reflect.ValueOf(v)
		for rv.Kind() == reflect.Interface || rv.Kind() == reflect.Pointer {
			if rv.IsNil() {
				return nil
			}

			rv = rv.Elem()
		}

		if rv.Kind() != reflect.Map || rv.Type().Key().Kind() != reflect.String {
			return nil
		}

		el := rv.MapIndex(reflect.ValueOf(k).Convert(rv.Type().Key()))
		if !el.IsValid() {
			return nil
		}

		v = el.Interface()
	}

	return v
}

// digNode parses the call standing in for a chain of fields read off
// base (`.` or a variable). Parsed rather than built so the node
// carries a tree, which html/template needs when it copies one.
func digNode(base string, keys []string) (parse.Node, error) {
	var b strings.Builder
	if base != "." && base != "$" {
		b.WriteString("{{" + base + " := 0}}")
	}

	b.WriteString("{{(" + digFunc + " " + base)
	for _, k := range keys {
		b.WriteString(" " + strconv.Quote(k))
	}

	b.WriteString(")}}")

	trees, err := parse.Parse("dig", b.String(), "", "", map[string]any{digFunc: dig})
	if err != nil {
		return nil, err
	}

	nodes := trees["dig"].Root.Nodes
	action, ok := nodes[len(nodes)-1].(*parse.ActionNode)
	if !ok {
		return nil, nil
	}

	return action.Pipe.Cmds[0].Args[0], nil
}

// digArg answers the replacement for one command argument, or nil to
// keep it.
func digArg(n parse.Node) (parse.Node, error) {
	switch n := n.(type) {
	case *parse.FieldNode:
		if len(n.Ident) > 1 {
			return digNode(".", n.Ident)
		}
	case *parse.VariableNode:
		if len(n.Ident) > 1 {
			return digNode(n.Ident[0], n.Ident[1:])
		}
	case *parse.PipeNode:
		return nil, digPipe(n)
	}

	return nil, nil
}

// digPipe rewrites the field chains of one pipeline. A field in the
// first position followed by arguments is a method call, not a value,
// and is left alone.
func digPipe(p *parse.PipeNode) error {
	if p == nil {
		return nil
	}

	for _, cmd := range p.Cmds {
		for i, arg := range cmd.Args {
			if i == 0 && len(cmd.Args) > 1 {
				if _, ok := arg.(*parse.PipeNode); !ok {
					continue
				}
			}

			repl, err := digArg(arg)
			if err != nil {
				return err
			}

			if repl != nil {
				cmd.Args[i] = repl
			}
		}
	}

	return nil
}

func digList(l *parse.ListNode) error {
	if l == nil {
		return nil
	}

	for _, n := range l.Nodes {
		var err error
		switch n := n.(type) {
		case *parse.ActionNode:
			err = digPipe(n.Pipe)
		case *parse.TemplateNode:
			err = digPipe(n.Pipe)
		case *parse.IfNode:
			err = digBranch(&n.BranchNode)
		case *parse.RangeNode:
			err = digBranch(&n.BranchNode)
		case *parse.WithNode:
			err = digBranch(&n.BranchNode)
		case *parse.ListNode:
			err = digList(n)
		}

		if err != nil {
			return err
		}
	}

	return nil
}

func digBranch(n *parse.BranchNode) error {
	if err := digPipe(n.Pipe); err != nil {
		return err
	}

	if err := digList(n.List); err != nil {
		return err
	}

	return digList(n.ElseList)
}
