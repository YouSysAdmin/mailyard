// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package render

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"text/template/parse"
)

// MaxIterations bounds how many times the range bodies of one render
// may run, summed over every range in every template it invokes. The
// output cap cannot reach a range that writes nothing and
// text/template has no per-iteration hook, so every range body is
// planted with a call that counts and errors once the budget is spent.
const MaxIterations = 1_000_000

// ErrTooManyIterations is the render refusing past MaxIterations.
var ErrTooManyIterations = errors.New("template ran past the iteration limit")

// ErrFormatWidth is a printf verb asking for more padding than any
// message needs.
var ErrFormatWidth = errors.New("template printf width or precision is too large")

// budgetFunc is the name of the planted call. Underscored so it cannot
// collide with a data key an author references through Normalize.
const budgetFunc = "_iteration"

// tickTree is parsed once to borrow a well-formed action node from.
// Every range gets a copy: html/template edits the action nodes it
// escapes and refuses one it meets twice, and Copy needs the node's
// tree pointer set, which a hand-built node lacks.
var tickTree = func() *parse.Tree {
	trees, err := parse.Parse("tick", "{{"+budgetFunc+"}}", "", "",
		map[string]any{budgetFunc: func() (string, error) { return "", nil }})
	if err != nil {
		panic(err)
	}

	return trees["tick"]
}()

// budget is one render's iteration allowance.
type budget struct {
	left int
}

// tick is the planted function.
func (b *budget) tick() (string, error) {
	if b.left <= 0 {
		return "", ErrTooManyIterations
	}

	b.left--

	return "", nil
}

// plant walks every tree a template parsed and prepends the counting
// call to every range body, checking printf widths on the way. It
// returns the function map the template must be given before Execute.
func plant(trees map[string]*parse.Tree) (map[string]any, error) {
	b := &budget{left: MaxIterations}
	for _, t := range trees {
		if t == nil || t.Root == nil {
			continue
		}

		if err := plantList(t.Root); err != nil {
			return nil, err
		}
	}

	return map[string]any{budgetFunc: b.tick}, nil
}

func plantList(l *parse.ListNode) error {
	if l == nil {
		return nil
	}

	for _, n := range l.Nodes {
		if err := plantNode(n); err != nil {
			return err
		}
	}

	return nil
}

func plantNode(n parse.Node) error {
	switch n := n.(type) {
	case *parse.RangeNode:
		if err := checkPipe(n.Pipe); err != nil {
			return err
		}

		if n.List == nil {
			n.List = &parse.ListNode{NodeType: parse.NodeList}
		}

		n.List.Nodes = append([]parse.Node{tickTree.Root.Nodes[0].Copy()}, n.List.Nodes...)

		if err := plantList(n.List); err != nil {
			return err
		}

		return plantList(n.ElseList)
	case *parse.IfNode:
		if err := checkPipe(n.Pipe); err != nil {
			return err
		}

		if err := plantList(n.List); err != nil {
			return err
		}

		return plantList(n.ElseList)
	case *parse.WithNode:
		if err := checkPipe(n.Pipe); err != nil {
			return err
		}

		if err := plantList(n.List); err != nil {
			return err
		}

		return plantList(n.ElseList)
	case *parse.ActionNode:
		return checkPipe(n.Pipe)
	case *parse.TemplateNode:
		return checkPipe(n.Pipe)
	case *parse.ListNode:
		return plantList(n)
	}

	return nil
}

// wideVerb matches a printf verb whose width or precision runs to
// five digits or is taken from an argument. fmt allocates the padding
// before writing, so the output cap does not bound it.
var wideVerb = regexp.MustCompile(`%[-+# 0]*(\d{5,}|\*)|%[-+# 0]*\d*\.(\d{5,}|\*)`)

// checkPipe refuses a printf whose format asks for absurd padding.
// Only a literal format is checked.
func checkPipe(p *parse.PipeNode) error {
	if p == nil {
		return nil
	}

	for _, cmd := range p.Cmds {
		for i, arg := range cmd.Args {
			s, ok := arg.(*parse.StringNode)
			if !ok || !wideVerb.MatchString(s.Text) {
				continue
			}

			if i > 0 {
				if id, ok := cmd.Args[0].(*parse.IdentifierNode); ok && strings.HasPrefix(id.Ident, "print") {
					return fmt.Errorf("%w: %s", ErrFormatWidth, s.Text)
				}
			}
		}

		for _, arg := range cmd.Args {
			if inner, ok := arg.(*parse.PipeNode); ok {
				if err := checkPipe(inner); err != nil {
					return err
				}
			}
		}
	}

	return nil
}
