// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tests

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// A PIECE OF UI THAT EXISTS AS A COMPONENT IS WRITTEN AS THAT COMPONENT.
//
// This is not tidiness. Every one of the patterns below was hand-written
// in thirty to sixty views, and each drifted in a way nobody could see
// from any single file:
//
//   - the dialog: closing one was three different things. 22 views used
//     useModalSafeClose, others a bare @click.self - which throws the
//     form away when a drag that started inside the box ends on the
//     overlay - and seven had no dismiss handling at all. Escape worked
//     in two of 29. Meanwhile the composable armed a document listener
//     for the life of the VIEW, so a page holding three dialogs ran
//     three Escape handlers whether or not anything was open.
//   - the status pill: ten copies of one switch, three byte-identical,
//     so a colour changed in one place meant the same status reading
//     differently on two pages.
//   - the empty state, the page header and the loading block: 160 copies
//     between them, and the header's two-children rule was kept by a
//     test that read the INDENTATION of the file.
//
// The components already existed for some of this - useModalSafeClose
// was there and 7 views did not call it - which is the argument for a
// check rather than a note in the README. An abstraction nothing
// enforces is one that half the tree uses.
//
// Each rule names the component that replaces it, because a failure here
// is a person about to write markup by hand, and the useful thing to
// tell them is what to write instead.
func TestTheConsoleUsesItsOwnComponents(t *testing.T) {
	root := consoleSrc(t)

	rules := []struct {
		// What the guard looks for in the markup.
		pattern *regexp.Regexp

		// The file that is allowed to contain it: the component itself.
		owner string

		// What to write instead.
		use string
	}{
		{regexp.MustCompile(`class="modal-overlay"`), "components/BaseModal.vue", "BaseModal"},
		{regexp.MustCompile(`class="modal[ "]`), "components/BaseModal.vue", "BaseModal"},
		{regexp.MustCompile(`class="modal-(header|body|footer)"`), "components/BaseModal.vue", "BaseModal"},
		{regexp.MustCompile(`class="empty-state`), "components/EmptyState.vue", "EmptyState"},
		{regexp.MustCompile(`class="loading-page`), "components/LoadingBlock.vue", "LoadingBlock"},
		// The `spinner` class on its own is not listed. A lone spinner
		// beside a card heading, or inside a button that is saving, is a
		// different thing from the block that stands in for a page while
		// it loads - and only the block was written out 57 times.
		{regexp.MustCompile(`class="page-header"`), "components/PageHeader.vue", "PageHeader"},
		{regexp.MustCompile(`class="page-actions"`), "components/PageHeader.vue", "PageHeader"},
		{regexp.MustCompile(`class="form-group"`), "components/FormField.vue", "FormField"},
		// Not `stat-block` or a bare `stat-value`. The campaign page
		// stacks plain labelled numbers with no glyph and no header,
		// which is a different shape - what StatCard owns is the CARD,
		// and the inbound page had written three of them with its own
		// inline svgs drawn on the nav grid rather than this one.
		{regexp.MustCompile(`class="stat-card"`), "components/StatCard.vue", "StatCard"},
		// The inline notice. Ten views wrote out the same four elements,
		// one of which - a wrapper for an icon none of them had - did
		// nothing at all, and the severity was two classes that had to
		// agree. The component takes the severity and the lead line as
		// props and the body as its slot.
		// The whole class, not a suffix of one: `card-notice` is a caller
		// placing the component, which is the sanctioned way to do it.
		{regexp.MustCompile(`class="(?:[^"]*\s)?notice(-[a-z]+)?[\s"]`), "components/Notice.vue", "Notice"},
		// not a bare `form-label`. A filter bar labels its select with
		// the same class and is not a form field - wrapping those in
		// FormField would add the field's bottom margin to a row of
		// filters.
		// Not the badge classes. A badge is also a plain label - "you",
		// "platform", "Required", the type of an identity provider - and
		// only the STATUS vocabularies belong to StatusBadge. What has to
		// stay in one place there is the status-to-colour table, which is
		// the test below.
	}

	var findings []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".vue") {
			return err
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)

		// Only the template. A scoped style block naming .modal or
		// .empty-state is styling, which is a separate rule.
		markup := string(body)
		if i := strings.Index(markup, "<style"); i >= 0 {
			markup = markup[:i]
		}

		for i, line := range strings.Split(markup, "\n") {
			for _, r := range rules {
				if r.owner == rel {
					continue
				}

				if r.pattern.MatchString(line) {
					findings = append(findings,
						rel+":"+strconv.Itoa(i+1)+" write "+r.use+" instead of "+strings.TrimSpace(line))
				}
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	if len(findings) > 0 {
		t.Errorf("%d places write markup a shared component owns:\n%s",
			len(findings), strings.Join(findings, "\n"))
	}
}

// A FIELD'S GUIDANCE GOES THROUGH FormField, not into its slot.
//
// FormField replaces the hint with the error, because two lines of
// guidance where one of them says the value is wrong reads as if both
// are still true. It can only do that for a hint it can SEE - one given
// through the `hint` prop or the `hint` slot. A <p class="form-hint">
// written into the default slot is markup the component never touches,
// so it goes on sitting under the error.
//
// The prop was written for exactly this and had ZERO callers. All 110 of
// them hand-wrote the markup instead, in three different tags - `p`,
// `span` and `small` - which is what a component whose better path
// nothing takes looks like from the outside. Converting them turned up
// three fields hand-rolling the error itself: a JSON parse failure
// painted red with a private class, a domain error, and a password
// length rule that was a hint with a red modifier on it. Those are
// `:error` now, and two scoped colour rules went with them.
//
// A form-hint OUTSIDE a FormField is untouched - a standalone paragraph
// introducing a group of fields is not a field's guidance, and fifteen
// of those are legitimate.
func TestAFieldsHintGoesThroughFormField(t *testing.T) {
	root := consoleSrc(t)

	// Depth over the file rather than a per-line match: the rule is
	// about being INSIDE a FormField, which no single line can answer.
	tag := regexp.MustCompile(`<FormField\b|</FormField>|class="form-hint"`)

	var findings []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".vue") {
			return err
		}

		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		// FormField renders the hint it is given, so it holds the only
		// legitimate form-hint inside itself.
		if rel == "components/FormField.vue" {
			return nil
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		markup := string(body)
		if i := strings.Index(markup, "<style"); i >= 0 {
			markup = markup[:i]
		}

		depth := 0
		for _, m := range tag.FindAllStringIndex(markup, -1) {
			switch markup[m[0]:m[1]] {
			case "<FormField":
				depth++
			case "</FormField>":
				depth--
			default:
				if depth > 0 {
					line := strconv.Itoa(strings.Count(markup[:m[0]], "\n") + 1)
					findings = append(findings, rel+":"+line)
				}
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	if len(findings) > 0 {
		t.Errorf("%d hints sit in a FormField's slot, where the error cannot replace them - "+
			"pass `hint` (plain text) or fill the `hint` slot (markup):\n%s",
			len(findings), strings.Join(findings, "\n"))
	}

	for _, f := range twoHints(t, root) {
		t.Errorf("%s carries both a `hint` prop and a `hint` slot. The slot wins, so the "+
			"prop's sentence renders nowhere - merge it into the slot or delete it", f)
	}
}

// twoHints finds fields given a hint twice.
//
// FormField renders `<slot name="hint">{{ hint }}</slot>`, so a filled
// slot shadows the prop entirely. Two of these existed on the project
// settings page and the shadowed text was not filler: one was the caveat
// that a bounce address does nothing on a provider which replaces the
// Return-Path, the other a paragraph explaining that owners always
// receive alerts whatever address is named. Neither had ever rendered.
//
// Both were made by the sweep that moved hints into the component: a
// <small> became the prop, and the sibling that carried markup became
// the slot.
func twoHints(t *testing.T, root string) []string {
	t.Helper()

	hasProp := regexp.MustCompile(`(?:^|\s):?hint=`)
	var found []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".vue") {
			return err
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		rel, _ := filepath.Rel(root, path)
		markup := string(body)
		if i := strings.Index(markup, "<style"); i >= 0 {
			markup = markup[:i]
		}

		for _, at := range formFieldOpens(markup) {
			tag := markup[at.open:at.tagEnd]
			if hasProp.MatchString(tag) && strings.Contains(markup[at.tagEnd:at.close], "#hint") {
				line := strconv.Itoa(strings.Count(markup[:at.open], "\n") + 1)
				found = append(found, filepath.ToSlash(rel)+":"+line)
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	return found
}

type formField struct{ open, tagEnd, close int }

// formFieldOpens locates each <FormField>, the end of its opening tag
// and the start of its matching close, respecting nesting.
func formFieldOpens(src string) []formField {
	var out []formField
	for _, m := range regexp.MustCompile(`<FormField\b`).FindAllStringIndex(src, -1) {
		tagEnd := attrEnd(src, m[0])
		depth, closeAt := 0, len(src)
		for _, t := range regexp.MustCompile(`<FormField\b|</FormField>`).FindAllStringIndex(src[m[0]:], -1) {
			if strings.HasPrefix(src[m[0]+t[0]:], "</") {
				depth--
				if depth == 0 {
					closeAt = m[0] + t[0]

					break
				}

				continue
			}

			depth++
		}

		out = append(out, formField{open: m[0], tagEnd: tagEnd, close: closeAt})
	}

	return out
}

// attrEnd is the index just past the ">" that ends an opening tag,
// skipping any ">" inside a quoted attribute value.
func attrEnd(src string, at int) int {
	var quote byte
	for i := at; i < len(src); i++ {
		c := src[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '>':
			return i + 1
		}
	}

	return len(src)
}

// Copying to the clipboard is CopyButton's job.
//
// Fourteen views wrote it out, and the same act reported itself
// differently depending on the page: some swapped the button's label,
// some raised a toast, some did neither, and a refused clipboard was
// silent in half of them. The component does the swap and takes an
// `announce` for the values a label cannot name.
func TestNothingElseTouchesTheClipboard(t *testing.T) {
	root := consoleSrc(t)

	// The sandbox reader FETCHES the raw message before it can copy it,
	// so what goes on the clipboard does not exist until the click. A
	// prop cannot carry that, and a `resolve` callback on CopyButton
	// would be API surface for one call site.
	allowed := map[string]bool{
		"components/CopyButton.vue":       true,
		"views/sandbox/SandboxReader.vue": true,
	}

	var findings []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".vue") {
			return err
		}

		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if allowed[rel] {
			return nil
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		for i, line := range strings.Split(string(body), "\n") {
			if strings.Contains(line, "navigator.clipboard") {
				findings = append(findings, rel+":"+strconv.Itoa(i+1)+" "+strings.TrimSpace(line))
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	if len(findings) > 0 {
		t.Errorf("the clipboard is written outside CopyButton:\n%s", strings.Join(findings, "\n"))
	}
}

// The colour of a status is decided in composables/statusBadge.ts and
// nowhere else. Ten views held a copy of that switch, and the three
// that spoke the same vocabulary had already been edited apart from one
// another - a status added to the API reached whichever of them somebody
// remembered.
func TestNothingElseDecidesWhatAStatusLooksLike(t *testing.T) {
	root := consoleSrc(t)
	owner := filepath.Join("composables", "statusBadge.ts")

	var findings []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}

		if !strings.HasSuffix(path, ".vue") && !strings.HasSuffix(path, ".ts") {
			return nil
		}

		rel, _ := filepath.Rel(root, path)
		if rel == owner {
			return nil
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		for i, line := range strings.Split(string(body), "\n") {
			if strings.Contains(line, "function statusBadgeClass") ||
				strings.Contains(line, "const statusBadgeClass") {
				findings = append(findings,
					filepath.ToSlash(rel)+":"+strconv.Itoa(i+1)+" "+strings.TrimSpace(line))
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	if len(findings) > 0 {
		t.Errorf("a status colour is decided outside composables/statusBadge.ts:\n%s\n"+
			"add the vocabulary to that file and render <StatusBadge :status scope=\"...\" />",
			strings.Join(findings, "\n"))
	}
}
