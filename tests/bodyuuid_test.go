// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tests

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// notAUUID names the request fields ending in _id that do not hold one
// of our ids, with the reason.
var notAUUID = map[string]string{
	"client_id":     "the identity provider's own client id",
	"credential_id": "a WebAuthn credential id",
	"message_id":    "an RFC 5322 Message-ID header",
	// A relay node reports what it read out of the message it carried.
	// Refusing the report over one entry would have the node retry it
	// forever, so a bad entry is skipped where the report is applied.
	"reportOutcome.email_id": "read out of a message by a relay node",
}

// A request field holding one of our ids validates it as a uuid, so a
// malformed one is a 400 naming the field and never the 404 a store
// gives an id it cannot compare.
func TestEveryBodyIDIsValidatedAsAUUID(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(repoRoot(t), "internal", "domain", "*", "types.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no types.go files found: %v", err)
	}

	checked := 0
	var findings []string
	for _, path := range files {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}

		ast.Inspect(file, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}

			st, ok := ts.Type.(*ast.StructType)
			if !ok || st.Fields == nil {
				return true
			}

			// Responses are exported, request bodies are not.
			if ast.IsExported(ts.Name.Name) {
				return true
			}

			for _, f := range st.Fields.List {
				if f.Tag == nil {
					continue
				}

				raw, err := strconv.Unquote(f.Tag.Value)
				if err != nil {
					continue
				}

				tag := reflect.StructTag(raw)
				name, _, _ := strings.Cut(tag.Get("json"), ",")
				if !strings.HasSuffix(name, "_id") && !strings.HasSuffix(name, "_ids") {
					continue
				}

				if _, ok := notAUUID[name]; ok {
					continue
				}

				if _, ok := notAUUID[ts.Name.Name+"."+name]; ok {
					continue
				}

				checked++
				if !strings.Contains(tag.Get("validate"), "uuid") {
					findings = append(findings, path+":"+strconv.Itoa(fset.Position(f.Pos()).Line)+" "+ts.Name.Name+"."+name)
				}
			}

			return true
		})
	}

	if checked == 0 {
		t.Fatal("no id fields found - this test would pass vacuously")
	}

	if len(findings) > 0 {
		t.Errorf("%d request field(s) holding an id with no uuid rule:\n  %s\n\n"+
			"Add `validate:\"omitempty,uuid\"` (or required,uuid), or name the field in notAUUID with a reason.",
			len(findings), strings.Join(findings, "\n  "))
	}
}
