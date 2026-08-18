// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tests

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// AN IMPORT NOTHING USES IS A CLAIM THAT SOMETHING STILL DEPENDS ON IT.
//
// Nothing in the toolchain catches these. `noUnusedLocals` is not set,
// so vue-tsc says nothing, prettier only reformats them, and a `.vue`
// file's script and template are two halves that neither tool checks
// against each other for this.
//
// Sixteen had accumulated. Fourteen were mine, left behind by pulling
// dialogs and cards out of two campaign pages - Campaigns.vue still
// imported four api modules, four types and two components for a form
// that had moved to its own file. The other two were older: authApi and
// LoginProvider on the project settings page, left when project-level
// single sign-on was removed, along with the paragraph of comments that
// described the card they belonged to.
//
// The cost is not bundle size, which the build strips. It is that
// somebody reading the imports learns what a file depends on, and every
// one of those said the page still talks to a subsystem it does not.
func TestTheConsoleImportsNothingItDoesNotUse(t *testing.T) {
	root := consoleSrc(t)

	// A binding from a named list, a default import, or a namespace.
	// `import type { X }` yields X, and the `type` keyword inside a
	// braced list is stripped rather than treated as a name.
	named := regexp.MustCompile(`(?s)import\s+(?:type\s+)?\{([^}]*)\}\s+from`)
	def := regexp.MustCompile(`(?m)^\s*import\s+(?:type\s+)?([A-Za-z_$][\w$]*)\s*(?:,|from)`)

	var findings []string
	seen := 0
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}

		ext := filepath.Ext(path)
		if ext != ".vue" && ext != ".ts" {
			return nil
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		src := string(body)
		script, rest := src, ""
		// In a .vue the template and style may use a binding the script
		// only imports - a component, most of all - so both halves count.
		if ext == ".vue" {
			open := strings.Index(src, "<script setup")
			if open < 0 {
				return nil
			}

			start := strings.Index(src[open:], ">")
			end := strings.Index(src, "</script>")
			if start < 0 || end < 0 {
				return nil
			}

			start += open + 1
			script, rest = src[start:end], src[:open]+src[end:]
		}

		seen++
		// Everything but the import statements, so an import cannot
		// count as its own use.
		body2 := stripImportLines(script) + rest

		var names []string
		for _, m := range named.FindAllStringSubmatch(script, -1) {
			for _, part := range strings.Split(m[1], ",") {
				n := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(part), "type "))
				// `X as Y` binds Y.
				if at := strings.LastIndex(n, " as "); at >= 0 {
					n = strings.TrimSpace(n[at+4:])
				}

				if n != "" {
					names = append(names, n)
				}
			}
		}

		for _, m := range def.FindAllStringSubmatch(script, -1) {
			if m[1] != "type" {
				names = append(names, m[1])
			}
		}

		rel, _ := filepath.Rel(root, path)
		for _, n := range names {
			used := regexp.MustCompile(`\b` + regexp.QuoteMeta(n) + `\b`)
			if !used.MatchString(body2) {
				findings = append(findings, filepath.ToSlash(rel)+": "+n)
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	if seen < 100 {
		t.Fatalf("only read %d files - the tree was not found", seen)
	}

	for _, f := range findings {
		t.Errorf("%s is imported and never used - delete the import, or the file is "+
			"claiming a dependency it does not have", f)
	}
}

// stripImportLines removes the import statements from a script, keeping
// everything else. A braced list may span lines, so it runs to the line
// carrying `from`.
func stripImportLines(src string) string {
	var out []string
	inside := false
	for _, line := range strings.Split(src, "\n") {
		if inside {
			if strings.Contains(line, "from '") || strings.Contains(line, "from \"") {
				inside = false
			}

			continue
		}

		if strings.HasPrefix(strings.TrimSpace(line), "import ") {
			inside = strings.Contains(line, "{") && !strings.Contains(line, "}")

			continue
		}

		out = append(out, line)
	}

	return strings.Join(out, "\n")
}
