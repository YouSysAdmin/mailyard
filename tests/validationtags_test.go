// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tests

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// casesOfDefaultMessage reads the switch rather than a second list of
// tags kept beside it. A list is the drift this test exists to catch,
// one level up.
// Every rule the tree uses needs a sentence here. The fallback is
// validator's own message, which reads
//
//	Key: 'updateInput.sample_data' Error:Field validation for
//	'sample_data' failed on the 'json' tag
//
// - it names a Go type the caller cannot see, describes the failure in
// library terms, and is what an operator got for a malformed sample
// payload, a bad URL and a bad hostname.
// On a POINTER field, omitempty does not mean what it reads as.
//
// It asks hasValue, and hasValue for a dereferenced pointer returns
// true whenever the pointer is non-nil - the value it points AT is
// never consulted. So every rule after it runs against the empty
// string, and `omitempty,email` rejects "" as not an address.
//
// That is not theoretical. A project could not be saved at all without
// a bounce address, because the console sends "" to clear one and the
// field is a pointer precisely so that "" can mean clear. Clearing a
// template's sample_data hit the same wall through `json`.
//
// omitzero is the one that reads the value: nil skips, and so does a
// pointer to the zero value.
var validateTag = regexp.MustCompile(`validate:"([^"]*)"`)

func TestNoPointerFieldSaysOmitempty(t *testing.T) {
	var findings []string

	err := filepath.Walk(repoRoot(t), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			switch info.Name() {
			case "node_modules", "dist", ".git", "dev-data", "vendor":
				return filepath.SkipDir
			}

			return nil
		}

		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil // not our business here
		}

		ast.Inspect(file, func(n ast.Node) bool {
			st, ok := n.(*ast.StructType)
			if !ok || st.Fields == nil {
				return true
			}

			for _, f := range st.Fields.List {
				if f.Tag == nil {
					continue
				}

				if _, isPtr := f.Type.(*ast.StarExpr); !isPtr {
					continue
				}

				raw, err := strconv.Unquote(f.Tag.Value)
				if err != nil {
					continue
				}

				rule := reflect.StructTag(raw).Get("validate")
				if rule == "" {
					continue
				}

				for part := range strings.SplitSeq(rule, ",") {
					if strings.TrimSpace(part) != "omitempty" {
						continue
					}

					name := "?"
					if len(f.Names) > 0 {
						name = f.Names[0].Name
					}

					findings = append(findings,
						path+":"+strconv.Itoa(fset.Position(f.Pos()).Line)+
							" "+name+" `validate:\""+rule+"\"`")
				}
			}

			return true
		})

		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	if len(findings) > 0 {
		t.Errorf("%d pointer field(s) using omitempty - use omitzero:\n  %s\n\n"+
			"On a pointer, omitempty only asks whether the pointer is nil, so every\n"+
			"rule after it runs against the empty value it points at.",
			len(findings), strings.Join(findings, "\n  "))
	}
}

func TestEveryRuleInUseHasASentence(t *testing.T) {
	// Structural tags: they steer the walk, they never fail.
	structural := map[string]bool{
		"omitempty": true, "omitzero": true, "dive": true, "required": true,
	}

	used := map[string]string{} // rule -> where it was seen
	err := filepath.Walk(repoRoot(t), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			switch info.Name() {
			case "node_modules", "dist", ".git", "dev-data", "vendor":
				return filepath.SkipDir
			}

			return nil
		}

		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		for _, m := range validateTag.FindAllStringSubmatch(string(body), -1) {
			for part := range strings.SplitSeq(m[1], ",") {
				rule, _, _ := strings.Cut(strings.TrimSpace(part), "=")
				if rule == "" || structural[rule] || !isRuleName(rule) {
					// Not a rule: the package doc shows
					// validate:"dive,..." as an example.
					continue
				}

				if _, seen := used[rule]; !seen {
					used[rule] = path
				}
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	if len(used) == 0 {
		t.Fatal("no validate tags found - this test would pass vacuously")
	}

	handled := casesOfDefaultMessage(t)
	var missing []string
	for rule, where := range used {
		if !handled[rule] {
			missing = append(missing, rule+" (first seen in "+where+")")
		}
	}

	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d rule(s) with no message, so a caller gets validator's own:\n  %s\n\n"+
			"Add a case to defaultMessage in errors.go.",
			len(missing), strings.Join(missing, "\n  "))
	}
}

func casesOfDefaultMessage(t *testing.T) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, validationErrorsFile(t), nil, 0)
	if err != nil {
		t.Fatalf("parsing errors.go: %v", err)
	}

	out := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "defaultMessage" {
			return true
		}

		ast.Inspect(fn.Body, func(n ast.Node) bool {
			cl, ok := n.(*ast.CaseClause)
			if !ok {
				return true
			}

			for _, e := range cl.List {
				if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if s, err := strconv.Unquote(lit.Value); err == nil {
						out[s] = true
					}
				}
			}

			return true
		})

		return false
	})
	if len(out) == 0 {
		t.Fatal("no cases found in defaultMessage - the parser lost it")
	}

	return out
}

func isRuleName(s string) bool {
	for _, r := range s {
		if r != '_' && !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') {
			return false
		}
	}

	return s != ""
}
