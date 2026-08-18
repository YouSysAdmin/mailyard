// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// A rule about the console, checked by reading web/src - which is why it
// lives beside it. These sat in internal/server and
// internal/domain/trackingpage, packages that have nothing to do with
// what they check and were simply where somebody was working at the time.

package tests

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The design system is plain CSS with a handful of repeated pieces -
// .card, .tabs, .btn*, .form-*, and a 4px spacing scale - and its
// whole value is that a page gets its look by using them rather than
// by tuning each element.
//
// It had drifted badly: 186 static style attributes across 37 views.
// Thirty-one were vertical margins on stacked cards, in three
// different values, so two cards sat 24px apart on one page and 20px
// on another. The rest re-invented flex rows, text weights, filter
// widths (320, 300, 280 for the same search box) and even colours -
// two views coloured text with --primary-600, which the top of
// styles.css forbids because it does not reach 4.5:1.
//
// So: no static style attribute in a view. A DYNAMIC :style is
// untouched - a bar chart's height and a computed offset are data,
// not design.
var staticStyleAttr = regexp.MustCompile(`(^|[^:\w-])style="`)

func TestTheConsoleKeepsItsStylingInTheStylesheet(t *testing.T) {
	root := consoleSrc(t)
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
		for i, line := range strings.Split(string(body), "\n") {
			if staticStyleAttr.MatchString(line) {
				findings = append(findings, rel+":"+strconv.Itoa(i+1)+" "+strings.TrimSpace(line))
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	if len(findings) > 0 {
		t.Errorf("%d element(s) style themselves inline:\n  %s\n\n"+
			"Use a utility from styles.css - the spacing scale is 4px per step, so\n"+
			"mt-2 is 8px - or give the view a scoped class. If the value is computed\n"+
			"from data, bind it with :style instead.",
			len(findings), strings.Join(findings, "\n  "))
	}
}

// Three more ways the console had of doing one thing twice. Each was
// visible to an operator, which is why they are worth a test rather
// than a style note.
func TestTheConsoleDoesOneThingOneWay(t *testing.T) {
	root := consoleSrc(t)

	checks := []struct {
		name    string
		pattern *regexp.Regexp
		skip    string
		why     string
	}{
		{
			name:    "a browser confirm() instead of the console's own dialog",
			pattern: regexp.MustCompile(`(?:window\.)?\bconfirm\(['"` + "`" + `]`),
			why: "useConfirm renders the styled modal every other page uses - a native\n" +
				"dialog shows the hostname and cannot say which button is destructive",
		},
		{
			name:    "a local copy of the date formatter",
			pattern: regexp.MustCompile(`function (?:formatDate|when)\s*\([^)]*\)[^{]*\{[^}]*toLocaleString\(\)`),
			why: "import { formatDate } from composables/formatDate. Thirty-three copies\n" +
				"rendered a missing date as \"-\", \"Never\" and \"never\" on different pages",
		},
		{
			// The check is the path, because that is the greppable half of
			// "a session boundary crosses a document boundary".
			name:    "the console's own path, written outside composables/session.ts",
			pattern: regexp.MustCompile(`['"` + "`" + `]/app/login`),
			skip:    "composables/session.ts",
			why: "enterConsole, leaveConsole and sessionExpired are the three ways in and\n" +
				"out, and they build the path from import.meta.env.BASE_URL. Signing out\n" +
				"with router.push kept every store alive, so the next person to sign in\n" +
				"got the previous one's projects, menu and a 400 per page",
		},
		{
			name:    "a hand-rolled refresh timer",
			pattern: regexp.MustCompile(`setInterval\(`),
			skip:    "composables/useAutoRefresh.ts",
			why: "useAutoRefresh is the one refresher: it skips a background tab, never\n" +
				"stacks two requests, refreshes on returning to the tab, and stops on\n" +
				"unmount. A local setInterval gets none of that, and the version in the\n" +
				"layout polled every open tab forever for a badge nobody could see",
		},
		{
			name:    "class=\"table\", which no rule matches",
			pattern: regexp.MustCompile(`<table class="table"`),
			why:     "bare <table> is styled - the class was inert on 18 views",
		},
	}

	var findings []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		// .ts as well as .vue: the api client and the stores are where two
		// of these rules were broken, and a rule that stops at the template
		// is a rule with a hole the size of src/api.
		if err != nil || info.IsDir() ||
			(!strings.HasSuffix(path, ".vue") && !strings.HasSuffix(path, ".ts")) {
			return err
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		rel, _ := filepath.Rel(root, path)
		for _, c := range checks {
			if c.skip != "" && strings.HasSuffix(rel, c.skip) {
				continue
			}

			if loc := c.pattern.FindIndex(body); loc != nil {
				line := 1 + strings.Count(string(body[:loc[0]]), "\n")
				findings = append(findings,
					rel+":"+strconv.Itoa(line)+" "+c.name+"\n      "+c.why)
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	if len(findings) > 0 {
		t.Errorf("%d place(s) doing something the console already does another way:\n  %s",
			len(findings), strings.Join(findings, "\n  "))
	}
}

// The design system styles CLASSES, never bare elements: .form-label,
// .form-input, .form-select, .form-textarea. So a control that carries
// no class is not styled at all - it gets browser defaults, which for a
// label means inline instead of block and for a text input means a
// 150px box in whatever font the surface inherited.
//
// It does not look like a missing class. It looks like a broken form:
// the shared SMTP dialog had "SES topic ARN" sitting on the same line
// as a stub of an input, on a screen where every other field was fine,
// and the markup around it was correct.
//
// A checkbox or radio is deliberately unclassed - .checkbox-label
// styles the pair from the outside.
func TestEveryFormControlCarriesItsClass(t *testing.T) {
	root := consoleSrc(t)

	need := map[string]string{
		"input":    "form-input",
		"select":   "form-select",
		"textarea": "form-textarea",
		// Any class at all: a label is also styled as .checkbox-label
		// and .switch-label, and all three make it a block.
		"label": "",
	}
	bare := map[string]bool{"checkbox": true, "radio": true, "hidden": true, "file": true}

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
		src, first := templateOf(string(body))
		for tag, class := range need {
			for _, at := range attrsOf(src, tag) {
				if ty := attrValue(at.text, "type"); bare[ty] {
					continue
				}

				got := attrValue(at.text, "class")
				if got == "hidden" {
					// display:none. The password managers' username
					// hint beside a password field is one.
					continue
				}

				if got == "" {
					findings = append(findings, rel+":"+strconv.Itoa(first+at.line)+
						" <"+tag+"> carries no class, so nothing styles it")
					continue
				}

				if class != "" && !strings.Contains(got, class) {
					findings = append(findings, rel+":"+strconv.Itoa(first+at.line)+
						" <"+tag+" class=\""+got+"\"> - wants ."+class)
				}
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	if len(findings) > 0 {
		t.Errorf("%d unstyled form control(s):\n  %s",
			len(findings), strings.Join(findings, "\n  "))
	}
}

// templateOf returns the markup half of a single-file component, plus
// the number of lines before it - a finding reported at the line it
// sits at inside the template names a line the file does not have.
//
// The script half is skipped because it mentions these tags in
// comments and in strings, and a scanner that reads those reports a
// line nobody can fix.
func templateOf(src string) (markup string, lineOffset int) {
	i := strings.Index(src, "<template>")
	j := strings.LastIndex(src, "</template>")
	if i == -1 || j <= i {
		return "", 0
	}

	start := i + len("<template>")

	return src[start:j], strings.Count(src[:start], "\n")
}

type tagAttrs struct {
	text string
	line int
}

// attrsOf returns the attribute text of every <name ...> in src.
//
// Quote-aware, because an attribute value is where a > lives:
// v-else-if="list.length > 0" ends the tag early for anything that
// scans for the next angle bracket, and the class that follows it then
// reads as absent.
func attrsOf(src, name string) []tagAttrs {
	var out []tagAttrs
	for i := 0; ; {
		k := strings.Index(src[i:], "<"+name)
		if k == -1 {
			return out
		}

		start := i + k
		j := start + 1 + len(name)
		// <inputs> is not <input>.
		if j < len(src) && (isNameByte(src[j])) {
			i = j
			continue
		}

		var quote byte
		end := j
		for ; end < len(src); end++ {
			c := src[end]
			switch {
			case quote != 0:
				if c == quote {
					quote = 0
				}
			case c == '"' || c == '\'':
				quote = c
			case c == '>':
				quote = 0
			}

			if quote == 0 && c == '>' {
				break
			}
		}

		out = append(out, tagAttrs{
			text: src[j:min(end, len(src))],
			line: 1 + strings.Count(src[:start], "\n"),
		})
		i = end
	}
}

func isNameByte(b byte) bool {
	return b == '-' || b == '_' ||
		(b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

func attrValue(attrs, name string) string {
	re := regexp.MustCompile(`\b` + name + `="([^"]*)"`)
	m := re.FindStringSubmatch(attrs)
	if m == nil {
		return ""
	}

	return m[1]
}

// Every nav item's icon has to exist in the map getIcon reads.
//
// It ends with an or-fallback to the empty string, so a name that is
// not in the map renders nothing - the item appears with a blank where
// every sibling has a glyph, and no error anywhere. Same shape as a
// permission string nobody spelled right: includes() just returns
// false.
func TestEveryNavIconExists(t *testing.T) {
	// The map and the names that use it are two files: icons.ts holds
	// the glyphs, navigation.ts names them.
	defined := map[string]bool{}
	for _, m := range regexp.MustCompile(`'([a-z-]+)':\s*'<svg`).FindAllStringSubmatch(readIcons(t), -1) {
		defined[m[1]] = true
	}

	if len(defined) == 0 {
		t.Fatal("no icons found - this test would pass vacuously")
	}

	var missing []string
	for _, m := range regexp.MustCompile(`icon:\s*'([a-z-]+)'`).FindAllStringSubmatch(readNavigation(t), -1) {
		if !defined[m[1]] {
			missing = append(missing, m[1])
		}
	}

	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("nav icon(s) with no glyph: %s\n\ngetIcon ends `icons[name] || ''`, so these render blank.",
			strings.Join(missing, ", "))
	}
}

// A class in the markup has to be a class something styles.
//
// This is the rule the Refresh control broke, visibly: `page-actions`
// existed in the stylesheet nowhere, so on Campaigns the button dropped
// onto a second line on top of the control, and on the sandbox the
// control sat between the title and Empty sandbox. It was not new -
// SandboxMessage.vue had been asking for that same class since the
// sandbox shipped, with its two buttons stacked the whole time.
//
// Thirteen more were found the day this was written, and the pattern
// behind most of them is one rule copied into three or four scoped
// blocks: whoever wrote the fifth page copied the markup and not the
// style, so `.alert` was a bare sentence on two auth pages, `.btn-block`
// was a narrow button, `.load-more` sat against the edge of the sandbox
// and a PEM was rendered in the body font. All five now have one
// definition in styles.css.
//
// Bound classes (:class) are not checked - those are computed, and this
// is about the static markup where a typo is invisible.
func TestEveryClassInTheMarkupIsStyled(t *testing.T) {
	root := consoleSrc(t)
	shared, err := os.ReadFile(filepath.Join(root, "assets", "styles.css"))
	if err != nil {
		t.Fatalf("read the stylesheet: %v", err)
	}

	known := classNames(string(shared))
	if len(known) < 100 {
		t.Fatalf("only found %d classes in the stylesheet - this test would pass vacuously", len(known))
	}

	// Plus whatever a component reaches into its slots for.
	//
	// A caller fills a slot with markup of its own, and that markup is
	// compiled in the CALLER's scope - so a class the component styles
	// through :deep() has no rule in the file that renders it. That is
	// not a missing rule, it is the one mechanism Vue provides for
	// styling slotted content, and MessageReader uses it for the facts
	// the two readers put in its envelope.
	for c := range deepClassNames(t, root) {
		known[c] = true
	}

	staticClass := regexp.MustCompile(`\sclass="([^"{}]*)"`)
	styleBlock := regexp.MustCompile(`(?s)<style[^>]*>(.*?)</style>`)

	var findings []string
	files := 0
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".vue") {
			return err
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		files++
		// A view may style its own classes, and most do.
		own := map[string]bool{}
		for _, m := range styleBlock.FindAllStringSubmatch(string(body), -1) {
			for c := range classNames(m[1]) {
				own[c] = true
			}
		}

		rel, _ := filepath.Rel(root, path)
		for _, m := range staticClass.FindAllStringSubmatch(string(body), -1) {
			for tok := range strings.FieldsSeq(m[1]) {
				if known[tok] || own[tok] {
					continue
				}

				findings = append(findings, rel+": "+tok)
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	if files < 50 {
		t.Fatalf("only walked %d views - the tree was not found", files)
	}

	for _, f := range findings {
		t.Errorf("%s is styled by nothing. Add the rule to assets/styles.css if other "+
			"pages will want it, put it in this file's own <style> if it is local, or "+
			"drop the class - markup asking for styling that does not exist renders "+
			"as whatever the browser felt like", f)
	}
}

// classNames collects every class selector in a stylesheet.
// deepClassNames collects every class any component styles through
// :deep(), across the whole console.
//
// Not per-file: which component fills whose slot is not something a
// walker can answer, and writing :deep() is a deliberate act - somebody
// reaching past their own scope on purpose. What it is NOT is a blanket
// exemption: a plain scoped selector naming a class this file does not
// render stays a failure, which is the test below.
func deepClassNames(t *testing.T, root string) map[string]bool {
	t.Helper()

	deep := regexp.MustCompile(`:deep\(([^)]*)\)`)
	styleBlock := regexp.MustCompile(`(?s)<style[^>]*>(.*?)</style>`)
	out := map[string]bool{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".vue") {
			return err
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		for _, block := range styleBlock.FindAllStringSubmatch(string(body), -1) {
			for _, m := range deep.FindAllStringSubmatch(block[1], -1) {
				for c := range classNames(m[1]) {
					out[c] = true
				}
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	return out
}

func classNames(css string) map[string]bool {
	out := map[string]bool{}
	for _, m := range regexp.MustCompile(`\.(-?[A-Za-z_][A-Za-z0-9_-]*)`).FindAllStringSubmatch(css, -1) {
		out[m[1]] = true
	}

	return out
}

// A page header holds a title and one group of actions.
//
// `.page-header` is `justify-content: space-between`, which puts its
// first child left and its last child right. A third child lands in the
// middle of the page, so a control added beside an existing button ends
// up between the title and that button, or drops onto a second line on
// top of it.
//
// The rule is structural rather than checked here:
// components/PageHeader.vue renders the title wrapper and the actions
// group itself, so a slotted control cannot become a third child, and
// TestTheConsoleUsesItsOwnComponents refuses a hand-written
// `class="page-header"`. Counting children by reading indentation is
// only worth doing while every view builds its own header.

// A SCOPED RULE REACHES A CHILD COMPONENT'S ROOT ELEMENT AND NOTHING
// DEEPER, so a component may not style another one's insides.
//
// Vue compiles a scoped block by stamping every selector with that
// component's id - `.mobile-menu-btn` becomes
// `.mobile-menu-btn[data-v-ef4e2b30]` - and stamping the same id onto
// every element the component itself renders. A child component's ROOT
// carries both ids, which is why styling a child's outermost element
// works and is used deliberately here. Everything inside that child
// carries only the CHILD's id, so a parent's rule for it compiles to a
// selector that matches nothing.
//
// Nothing reports that. It is not a Vue warning, not a type error, not
// a build failure, and not a broken page - the rule is simply absent
// and the element renders with whatever else applies.
//
// It cost the console its navigation on a phone. Splitting the shell
// into AppSidebar and AppTopbar left the layout holding
// `@media (max-width: 1024px) { .mobile-menu-btn { display: flex } }`
// for a button that had moved into the topbar. Below 1024px the rail
// hides itself and that button is the only way to the drawer, so the
// whole menu was unreachable: no rail, no button, no way to leave the
// page you were on. Found by measuring a computed style, not by looking
// at the screen, because a missing control looks like a design.
func TestNoComponentStylesAnotherOnesInsides(t *testing.T) {
	root := consoleSrc(t)

	styleBlock := regexp.MustCompile(`(?s)<style([^>]*)>(.*?)</style>`)
	templateBlock := regexp.MustCompile(`(?s)<template>\s*(.*?)\s*</template>\s*(?:<style|\z)`)
	cssComment := regexp.MustCompile(`(?s)/\*.*?\*/`)
	deepSelector := regexp.MustCompile(`:deep\([^)]*\)`)
	markupComment := regexp.MustCompile(`(?s)<!--.*?-->`)
	componentImport := regexp.MustCompile(`import\s+(\w+)\s+from\s+'[^']*/([\w-]+)\.vue'`)

	type file struct {
		rel      string
		markup   map[string]bool
		scoped   map[string]bool
		rootCls  map[string]bool
		imported []string
	}

	var files []*file
	byName := map[string]*file{}
	usedIn := map[string][]string{}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".vue") {
			return err
		}

		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		body := string(raw)
		rel, _ := filepath.Rel(root, path)
		f := &file{rel: rel, scoped: map[string]bool{}, rootCls: map[string]bool{}}

		// Everything outside the style blocks is markup and script. A
		// class named in a comment is not styling anything, in either.
		f.markup = markupClasses(markupComment.ReplaceAllString(styleBlock.ReplaceAllString(body, " "), " "))

		for _, m := range styleBlock.FindAllStringSubmatch(body, -1) {
			// An unscoped block is global and may style anything.
			if !strings.Contains(m[1], "scoped") {
				continue
			}

			// :deep() is EXCLUDED, and that is the whole difference
			// this test is about. A plain scoped selector reaching a
			// child's insides compiles to a selector that matches
			// nothing, silently. :deep() is Vue's one mechanism for
			// styling markup a caller put in your slot, and writing it
			// is somebody saying so on purpose - MessageReader styles
			// the facts the two readers hand it that way.
			css := deepSelector.ReplaceAllString(cssComment.ReplaceAllString(m[2], " "), " ")
			for c := range classNames(css) {
				f.scoped[c] = true
			}
		}

		// The root element is what a parent is allowed to reach, so it
		// is read on its own: the opening tag of the template block.
		if m := templateBlock.FindStringSubmatch(body); m != nil {
			if end := strings.Index(m[1], ">"); end > 0 {
				f.rootCls = markupClasses(" " + m[1][:end+1])
			}
		}

		for _, m := range componentImport.FindAllStringSubmatch(body, -1) {
			f.imported = append(f.imported, m[2])
		}

		files = append(files, f)
		byName[strings.TrimSuffix(filepath.Base(rel), ".vue")] = f
		for c := range f.markup {
			usedIn[c] = append(usedIn[c], rel)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	if len(files) < 50 {
		t.Fatalf("only walked %d components - the tree was not found", len(files))
	}

	for _, f := range files {
		// What this component may style: its own markup, plus the root
		// element of every component it renders.
		reachable := map[string]bool{}
		for c := range f.markup {
			reachable[c] = true
		}
		for _, name := range f.imported {
			child, ok := byName[name]
			if !ok {
				continue
			}

			for c := range child.rootCls {
				reachable[c] = true
			}
		}

		for c := range f.scoped {
			if reachable[c] {
				continue
			}

			// A class nothing renders is a different problem, and a
			// transition class is generated rather than written. Only a
			// class that belongs to ANOTHER component is this one.
			owners := usedIn[c]
			if len(owners) == 0 {
				continue
			}

			sort.Strings(owners)
			t.Errorf("%s styles .%s, which is rendered by %s and by nothing here. A scoped "+
				"rule reaches a child component's ROOT element and nothing deeper, so this "+
				"compiles to a selector that matches nothing and the rule is simply absent. "+
				"Move it into the component that renders the element",
				f.rel, c, strings.Join(owners, ", "))
		}
	}
}

// markupClasses collects the classes a template asks for: the static
// ones, plus the keys of an object `:class` and the literals in a bound
// one, since those are what a state rule is written against.
func markupClasses(markup string) map[string]bool {
	out := map[string]bool{}
	for _, m := range regexp.MustCompile(`\sclass="([^"{}]*)"`).FindAllStringSubmatch(markup, -1) {
		for tok := range strings.FieldsSeq(m[1]) {
			out[tok] = true
		}
	}

	for _, m := range regexp.MustCompile(`:class="([^"]*)"`).FindAllStringSubmatch(markup, -1) {
		for _, q := range regexp.MustCompile(`'([\w-]+)'`).FindAllStringSubmatch(m[1], -1) {
			out[q[1]] = true
		}

		for _, k := range regexp.MustCompile(`[{,]\s*([A-Za-z_][\w-]*)\s*:`).FindAllStringSubmatch(m[1], -1) {
			out[k[1]] = true
		}
	}

	return out
}
