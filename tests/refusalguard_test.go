// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tests

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// A REFUSAL HELPER'S RESULT IS RETURNED, NEVER TESTED.
//
// `response.BadRequest(c, msg)` is `c.Status(400).JSON(...)`, and that
// returns the JSON encoder's error - which is NIL. So a helper that
// refuses by returning one of those returns nil, and a caller written as
//
//	if err := h.checkSomething(c, ...); err != nil {
//	    return err
//	}
//
// Never fires. The handler carries on past the refusal, does the work,
// and writes a second response over the first - so the CALLER sees the
// success. Not a 500, not a silent no-op: a 201 with the created body,
// after a 404 had already been written into the same fasthttp response.
//
// This has cost five bugs. Four were found one at a time and each was
// fixed in place with a note - verifySession, passkeySelf,
// enrolmentScope, refuseCAOverAnAssignedName. The fifth was found by
// looking for the shape rather than for its symptoms, and it was four
// call sites at once:
//
//   - template.CreateVersion and UpdateVersion, where checkStylesheet is
//     the only thing verifying the stylesheet belongs to this project
//   - campaign.Create and Update, where validateCampaignRefs is the only
//     thing checking the template exists, has an active version, belongs
//     to this project, that the subscriber list and SMTP group exist, and
//     that the A/B splits add up. Every one of those refusals was
//     unreachable, and the campaign RUNNER acts on the row that results.
//
// Four notes in four files did not stop the fifth, which is why this is a
// test.
//
// The rule is about the CALL, not the definition. A helper that renders
// the response and is the caller's last statement is correct and there
// are nine of them (sendFailure, accessFailure, transition, respondWith,
// setStatus, runImport, refusePermission, checkPermission, store) - what
// is wrong is TESTING one of those results and continuing. So: a
// function that returns a lone error and answers with `response.*` may
// only be called as `return f(...)`.
//
// Fiber handlers are exempt by construction: `func(c fiber.Ctx) error`
// returns the refusal AS the response, which is the whole convention.
//
// Package scoped, and that is load-bearing rather than tidy:
// relaynode.setStatus propagates a real store error while
// smtpserver.setStatus renders a response, and matching on the bare name
// reported the honest one.
func TestARefusalHelperIsReturnedAndNotTested(t *testing.T) {
	root := repoRoot(t)
	fset := token.NewFileSet()

	type pkgFiles struct {
		src      map[string]string
		refusers map[string]bool
	}
	pkgs := map[string]*pkgFiles{}

	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}

		body, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}

		dir := filepath.Dir(path)
		if pkgs[dir] == nil {
			pkgs[dir] = &pkgFiles{src: map[string]string{}, refusers: map[string]bool{}}
		}

		rel, _ := filepath.Rel(root, path)
		pkgs[dir].src[rel] = string(body)

		file, perr := parser.ParseFile(fset, path, body, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", rel, perr)
		}

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}

			if !returnsLoneError(fn) || isFiberHandler(fn) {
				continue
			}

			if answersWithResponse(fn.Body) {
				pkgs[dir].refusers[fn.Name.Name] = true
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	if len(pkgs) < 30 {
		t.Fatalf("only walked %d packages - the walk is broken, not the code", len(pkgs))
	}

	var refuserCount int
	var findings []string
	for _, pf := range pkgs {
		refuserCount += len(pf.refusers)
		for name := range pf.refusers {
			// `if <anything> name(` - the shape that tests the result
			// instead of returning it.
			tested := regexp.MustCompile(`\bif (?:[\w, ]+:?= )?(?:\w+\.)?` + regexp.QuoteMeta(name) + `\(`)
			for rel, src := range pf.src {
				for _, loc := range tested.FindAllStringIndex(src, -1) {
					line := strings.Count(src[:loc[0]], "\n") + 1
					findings = append(findings, rel+":"+strconv.Itoa(line)+" tests the result of "+name+
						", which refuses by returning response.* - and that is nil")
				}
			}
		}
	}

	if refuserCount < 5 {
		t.Fatalf("found only %d refusal helpers - the detection is broken, not the code", refuserCount)
	}

	sort.Strings(findings)
	if len(findings) > 0 {
		t.Errorf("%d call site(s) testing a refusal that is always nil:\n  %s\n\n"+
			"response.* writes the status and returns nil, so the guard never fires and\n"+
			"the handler continues past the refusal. Return the helper's result, or give it\n"+
			"a (bool, error) signature and branch on the bool.",
			len(findings), strings.Join(findings, "\n  "))
	}
}

// returnsLoneError reports whether the function's only result is an error.
func returnsLoneError(fn *ast.FuncDecl) bool {
	// LAST result is error, however many precede it. It was "the only
	// result", and relaynode.projectNode returned (node, server,
	// error) with the same trap in the third slot: every caller tested
	// it, fell through the refusal, and dereferenced a nil node. Found
	// by the live permission audit as a 500, not by this test.
	res := fn.Type.Results
	if res == nil || len(res.List) == 0 {
		return false
	}

	last := res.List[len(res.List)-1]
	if len(last.Names) > 1 {
		return false
	}

	id, ok := last.Type.(*ast.Ident)

	return ok && id.Name == "error"
}

// isFiberHandler reports whether the signature is exactly a fiber
// handler, whose refusal IS its response.
func isFiberHandler(fn *ast.FuncDecl) bool {
	params := fn.Type.Params
	if params == nil || len(params.List) != 1 {
		return false
	}

	// Not a StarExpr: Fiber v3's Ctx is an INTERFACE, so a handler takes
	// it by value. Left matching on the pointer, this predicate answered
	// false for every handler in the tree and the exemption below it was
	// dead - each one would have been collected as a refusal helper.
	sel, ok := params.List[0].Type.(*ast.SelectorExpr)

	return ok && sel.Sel.Name == "Ctx"
}

// answersWithResponse reports whether the body returns a response.*
// helper directly, which is what makes its error result always nil.
func answersWithResponse(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		ret, ok := n.(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			return true
		}

		call, ok := ret.Results[0].(*ast.CallExpr)
		if !ok {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "response" {
			found = true
		}

		return true
	})

	return found
}
