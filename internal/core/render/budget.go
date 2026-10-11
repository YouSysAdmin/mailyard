// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package render

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	texttmpl "text/template"
	"text/template/parse"
)

// MaxIterations bounds how many times the range bodies and template
// bodies of one render may run, summed over everything it invokes. The
// output cap cannot reach a range or a recursion that writes nothing,
// and text/template has no per-iteration hook, so every range body and
// every template body is planted with a call that counts and errors
// once the budget is spent.
const MaxIterations = 1_000_000

// ErrTooManyIterations is the render refusing past MaxIterations.
var ErrTooManyIterations = errors.New("template ran past the iteration limit")

// ErrFormatWidth is a printf verb asking for more padding than any
// message needs.
var ErrFormatWidth = errors.New("template printf width or precision is too large")

// budgetFunc is the name of the planted call. Underscored so it cannot
// collide with a data key an author references through Normalize.
const budgetFunc = "_iteration"

// tickTree is parsed once to borrow a well-formed node from. The call
// sits in the CONDITION of an empty if, never in an action: an action
// is output, and html/template writes an empty string as `""` inside
// a script, so a planted action turned `var a={{template "x"}}` into
// `var a=""1`. A condition renders nothing in any context. Every
// site gets a copy: html/template edits the nodes it escapes and
// refuses one it meets twice, and Copy needs the node's tree pointer
// set, which a hand-built node lacks.
var tickTree = func() *parse.Tree {
	trees, err := parse.Parse("tick", "{{if "+budgetFunc+"}}{{end}}", "", "",
		map[string]any{budgetFunc: func() (string, error) { return "", nil }})
	if err != nil {
		panic(err)
	}

	return trees["tick"]
}()

// MaxTextBytes bounds the bytes the string functions of one render
// may read and produce, summed over every call. A variable can be
// reassigned inside a range, so `{{$a = print $a $a}}` doubles a
// string per iteration while writing nothing: the output cap never
// sees it and the iteration cap is reached only long after memory is
// gone. Charging what each call reads and returns stops a doubling
// within a few dozen iterations and bounds the copying a loop of
// comparisons over a long string can do.
const MaxTextBytes = 64 * 1024 * 1024

// ErrTooMuchText is the render refusing past MaxTextBytes.
var ErrTooMuchText = errors.New("template string functions ran past the 64 MiB limit")

// budget is one render's allowance: iterations, and the bytes the
// string functions may handle. Lenient renders print a missing value
// as nothing rather than as fmt's <nil>.
type budget struct {
	left    int
	text    int
	lenient bool
}

// tick is the planted function.
func (b *budget) tick() (string, error) {
	if b.left <= 0 {
		return "", ErrTooManyIterations
	}

	b.left--

	return "", nil
}

// spend charges n bytes against the text allowance.
func (b *budget) spend(n int) error {
	if n > b.text {
		b.text = 0

		return ErrTooMuchText
	}

	b.text -= n

	return nil
}

// textLen is the length of the string arguments, which is what a
// string function copies or compares. Other values are small or come
// from the request, whose size is bounded already.
func textLen(args ...any) int {
	n := 0
	for _, a := range args {
		switch v := a.(type) {
		case string:
			n += len(v)
		case []byte:
			n += len(v)
		}
	}

	return n
}

// blanks replaces nil arguments with an empty string on a lenient
// render, so a missing key prints as nothing in every function.
func (b *budget) blanks(args []any) []any {
	if !b.lenient || !slices.Contains(args, nil) {
		return args
	}

	out := slices.Clone(args)
	for i, a := range out {
		if a == nil {
			out[i] = ""
		}
	}

	return out
}

// stringer wraps a function producing text from its arguments, charging
// what it reads before the call and what it returned after.
func (b *budget) stringer(fn func(...any) string) func(...any) (string, error) {
	return func(args ...any) (string, error) {
		if err := b.spend(textLen(args...)); err != nil {
			return "", err
		}

		out := fn(b.blanks(args)...)
		if err := b.spend(len(out)); err != nil {
			return "", err
		}

		return out, nil
	}
}

// printf charges like stringer, and refuses before fmt runs when the
// most the call can produce does not fit what is left: fmt builds the
// whole result, padding included, before it can be charged.
func (b *budget) printf(format string, args ...any) (string, error) {
	if err := b.spend(len(format) + textLen(args...)); err != nil {
		return "", err
	}

	if err := checkFormat(format); err != nil {
		return "", err
	}

	args = b.blanks(args)
	if printfBound(format, args) > b.text {
		b.text = 0

		return "", ErrTooMuchText
	}

	out := fmt.Sprintf(format, floatArgs(format, args)...)
	if err := b.spend(len(out)); err != nil {
		return "", err
	}

	return out, nil
}

// compare charges a comparison for the strings it reads.
func (b *budget) compare(fn func(a, b any) (bool, error)) func(a, c any) (bool, error) {
	return func(x, y any) (bool, error) {
		if err := b.spend(textLen(x, y)); err != nil {
			return false, err
		}

		return fn(x, y)
	}
}

// funcs is the function map one render executes with.
func (b *budget) funcs() map[string]any {
	funcs := maps.Clone(comparisons)
	funcs[budgetFunc] = b.tick
	funcs[digFunc] = dig
	funcs["printf"] = b.printf
	funcs["print"] = b.stringer(fmt.Sprint)
	funcs["println"] = b.stringer(fmt.Sprintln)
	funcs["html"] = b.stringer(texttmpl.HTMLEscaper)
	funcs["js"] = b.stringer(texttmpl.JSEscaper)
	funcs["urlquery"] = b.stringer(texttmpl.URLQueryEscaper)
	funcs["eq"] = func(x any, ys ...any) (bool, error) {
		if err := b.spend(textLen(x) + textLen(ys...)); err != nil {
			return false, err
		}

		return eqFunc(x, ys...)
	}
	funcs["ne"] = b.compare(neFunc)
	funcs["lt"] = b.compare(orderFunc(func(c int) bool { return c < 0 }))
	funcs["le"] = b.compare(orderFunc(func(c int) bool { return c <= 0 }))
	funcs["gt"] = b.compare(orderFunc(func(c int) bool { return c > 0 }))
	funcs["ge"] = b.compare(orderFunc(func(c int) bool { return c >= 0 }))

	return funcs
}

// plant walks every tree a template parsed and prepends the counting
// call to every range body and to the body of every template, checking
// printf widths on the way. The template body is what bounds a
// recursion through {{template}}, which needs no range to branch. A
// lenient render also has every nested field reference rewritten so a
// missing level reads as nothing. It returns the function map the
// template must be given before Execute.
func plant(trees map[string]*parse.Tree, lenient bool) (map[string]any, error) {
	b := &budget{left: MaxIterations, text: MaxTextBytes, lenient: lenient}
	for _, t := range trees {
		if t == nil || t.Root == nil {
			continue
		}

		if err := plantList(t.Root); err != nil {
			return nil, err
		}

		if lenient {
			if err := digList(t.Root); err != nil {
				return nil, err
			}
		}

		t.Root.Nodes = append([]parse.Node{tickTree.Root.Nodes[0].Copy()}, t.Root.Nodes...)
	}

	return b.funcs(), nil
}

// checkFormat applies the width check at execution, so a format
// assembled at run time - a variable, a print concatenation, a
// pipeline - meets the same check as a literal one. The parse check
// stays for the error to name the template line.
func checkFormat(format string) error {
	if wideVerb.MatchString(format) {
		return fmt.Errorf("%w: %s", ErrFormatWidth, format)
	}

	return nil
}

// boundCap is where printfBound saturates. Anything past it is refused
// whatever the budget holds, and it keeps the sums from overflowing.
const boundCap = 1 << 40

// satAdd and satMul are addition and multiplication clamped to boundCap.
func satAdd(a, b int) int {
	return min(a+b, boundCap)
}

func satMul(a, b int) int {
	if a != 0 && b > boundCap/a {
		return boundCap
	}

	return min(a*b, boundCap)
}

// printfBound is the most text fmt.Sprintf can produce for the call.
// Every verb, or the one listing extra arguments, may print every
// argument in full, and every width and precision may pad every
// element of every argument, since fmt applies them per element of a
// slice or map.
func printfBound(format string, args []any) int {
	verbs, pad := formatVerbs(format)
	leaves, size := 0, 0
	for _, a := range args {
		l, s := printedSize(reflect.ValueOf(a), 0)
		leaves = satAdd(leaves, l)
		size = satAdd(size, s)
	}

	bound := satAdd(len(format), satMul(max(verbs, 1), satAdd(size, 16)))

	return satAdd(bound, satMul(pad, max(leaves, 1)))
}

// formatVerbs counts the verbs of a format and sums the literal widths
// and precisions they ask for. Digits inside an argument index are not
// padding.
func formatVerbs(format string) (verbs, pad int) {
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			continue
		}

		verbs++
		i++
		for i < len(format) && strings.IndexByte("+-# .*[0123456789", format[i]) >= 0 {
			switch c := format[i]; {
			case c == '[':
				end := strings.IndexByte(format[i:], ']')
				if end < 0 {
					return verbs, pad
				}

				i += end + 1
			case c >= '0' && c <= '9':
				n := 0
				for i < len(format) && format[i] >= '0' && format[i] <= '9' {
					n = satAdd(satMul(n, 10), int(format[i]-'0'))
					i++
				}

				pad = satAdd(pad, n)
			default:
				i++
			}
		}
	}

	return verbs, pad
}

// printedSize is how many elements fmt formats one at a time in v and
// how many bytes it can print for v under any verb, flags included but
// widths and precisions left to printfBound. fmt prints a pointer below
// the top level as an address, so the walk does not follow one.
func printedSize(v reflect.Value, depth int) (leaves, size int) {
	if !v.IsValid() {
		return 1, 16
	}

	typ := len(v.Type().String()) + 8
	switch v.Kind() {
	case reflect.String:
		return 1, satAdd(satMul(len(v.String()), 6), typ)
	case reflect.Bool:
		return 1, typ
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return 1, 70 + typ
	case reflect.Float32, reflect.Float64:
		return 1, 330 + typ
	case reflect.Complex64, reflect.Complex128:
		return 1, 700 + typ
	case reflect.Interface:
		if v.IsNil() {
			return 1, 16 + typ
		}

		return printedSize(v.Elem(), depth)
	case reflect.Pointer:
		if v.IsNil() || depth > 0 {
			return 1, 32 + typ
		}

		l, s := printedSize(v.Elem(), depth+1)

		return l, satAdd(s, typ)
	case reflect.Slice, reflect.Array:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return v.Len(), satAdd(satMul(v.Len(), 6), typ)
		}

		size = typ
		for i := range v.Len() {
			l, s := printedSize(v.Index(i), depth+1)
			leaves = satAdd(leaves, l)
			size = satAdd(size, satAdd(s, 2))
		}

		return max(leaves, 1), size
	case reflect.Map:
		size = typ
		iter := v.MapRange()
		for iter.Next() {
			kl, ks := printedSize(iter.Key(), depth+1)
			vl, vs := printedSize(iter.Value(), depth+1)
			leaves = satAdd(leaves, satAdd(kl, vl))
			size = satAdd(size, satAdd(satAdd(ks, vs), 4))
		}

		return max(leaves, 1), size
	case reflect.Struct:
		size = typ
		for i := range v.NumField() {
			l, s := printedSize(v.Field(i), depth+1)
			leaves = satAdd(leaves, l)
			size = satAdd(size, satAdd(s, len(v.Type().Field(i).Name)+2))
		}

		return max(leaves, 1), size
	}

	return 1, 32 + typ
}

// floatArgs hands a float verb a float. Whole numbers in the data are
// integers (see wholeNumbers), and `%.2f` of an int64 prints
// %!f(int64=10) where the author wanted 10.00.
func floatArgs(format string, a []any) []any {
	var out []any
	arg := 0
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			continue
		}

		i++
		for i < len(format) && strings.IndexByte("+-# 0123456789.[", format[i]) >= 0 {
			if format[i] != '[' {
				i++

				continue
			}

			end := strings.IndexByte(format[i:], ']')
			if end < 0 {
				return a
			}

			n, err := strconv.Atoi(format[i+1 : i+end])
			if err != nil {
				return a
			}

			arg = n - 1
			i += end + 1
		}

		if i >= len(format) || format[i] == '%' {
			continue
		}

		if strings.IndexByte("eEfFgG", format[i]) >= 0 && arg >= 0 && arg < len(a) {
			if n, ok := a[arg].(int64); ok {
				if out == nil {
					out = slices.Clone(a)
				}

				out[arg] = float64(n)
			}
		}

		arg++
	}

	if out == nil {
		return a
	}

	return out
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
// five digits or is taken from an argument, the argument named by an
// explicit index (`%[1]*d`) included. fmt allocates the padding
// before writing, so the output cap does not bound it.
var wideVerb = regexp.MustCompile(
	`%[-+# 0]*(\[\d+\])?(\d{5,}|\*)|%[-+# 0]*(\[\d+\])?\d*\.(\[\d+\])?(\d{5,}|\*)`)

// checkPipe refuses a printf whose format asks for absurd padding.
// Only a literal format is checked here, checkFormat covers the rest.
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
