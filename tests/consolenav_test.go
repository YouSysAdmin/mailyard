// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tests

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The sidebar's icons, checked as data rather than looked at.
//
// TestEveryNavIconExists beside this catches a name that is not in the
// map, which is the typo. This one catches the mistake that is easy to
// make and impossible to report: two rows in the same section carrying
// the same glyph. Nothing breaks, so it survives until somebody reads
// the menu.
//
// An icon that does not distinguish the row beside it is worse than no
// icon, because a repeated glyph reads as a category and two rows look
// like one feature listed twice.
//
// Across sections a repeat is fine, and several are deliberate: `users`
// for Contacts, Members and platform Users, `settings` for a project's
// and the platform's, `server` for a project's SMTP servers and the
// shared pool. Those mean the same thing at a different scope, and
// giving each a private glyph would say they are unrelated. A section is
// the unit because a section is what a person scans at once.

// iconRefRE reads an icon name wherever the nav declares one.
var iconRefRE = regexp.MustCompile(`icon: '([a-z0-9-]+)'`)

// itemNameRE reads the label of a nav entry. Groups carry `title`, so
// `label` matches entries and nothing else.
var itemNameRE = regexp.MustCompile(`label: '([^']+)'`)

// iconCallRE reads the ARGUMENT of a getIcon call - which is how the
// project switcher renders its briefcase, and the sidebar its collapse
// control. The argument, not a bare name, because it is not always one:
// `getIcon(collapsed ? 'panel-open' : 'panel-shut')` names two, and a
// pattern that insisted on a lone literal reported both as unused. That
// is the shape of failure worth avoiding here, since the obvious fix for
// it is to delete the glyphs rather than the pattern.
var iconCallRE = regexp.MustCompile(`getIcon\(([^)]*)\)`)

// iconNameRE picks the glyph names out of such an argument.
var iconNameRE = regexp.MustCompile(`'([a-z0-9-]+)'`)

// sectionIDRE splits NAV_GROUPS into groups. Every group opens with its
// id and nothing else in the array carries that key.
var sectionIDRE = regexp.MustCompile(`id: '([a-z-]+)'`)

func TestNoTwoItemsInASectionShareAnIcon(t *testing.T) {
	sections := navSectionBlocks(t, readNavigation(t))

	// The count is asserted so that a rewrite of the array - a change of
	// quoting, a move to another file - fails here rather than passing
	// with nothing found. A parser that matches nothing agrees with every
	// rule there is.
	if len(sections) < 8 {
		t.Fatalf("read %d nav groups out of navigation.ts, expected at least 8 - "+
			"the parser has stopped finding them", len(sections))
	}

	for _, sec := range sections {
		// Paired by POSITION rather than by one regex spanning both
		// fields: the items are written three ways in this file - on one
		// line, over several, and over several with a projectSubpath
		// between the name and the icon - and a single pattern covering
		// the pair matches only the first shape while looking correct.
		names := itemNameRE.FindAllStringSubmatch(sec.body, -1)
		icons := iconRefRE.FindAllStringSubmatch(sec.body, -1)

		if len(names) != len(icons) {
			t.Errorf("section %q has %d names and %d icons - every item carries exactly one of "+
				"each, so this check is reading them out of step", sec.id, len(names), len(icons))

			continue
		}

		byIcon := map[string][]string{}
		for i := range names {
			byIcon[icons[i][1]] = append(byIcon[icons[i][1]], names[i][1])
		}

		for icon, rows := range byIcon {
			if len(rows) > 1 {
				sort.Strings(rows)
				t.Errorf("section %q gives %s to %s - two rows a person scans together must not "+
					"carry the same glyph, or they read as one feature listed twice",
					sec.id, icon, strings.Join(rows, " and "))
			}
		}
	}
}

// An icon nobody renders is dead weight in the largest file in the
// console, and it is invisible: a stale key looks exactly like one in
// use. Both directions matter - the sibling test catches a name with no
// glyph, this one a glyph with no name.
func TestEveryIconInTheMapIsUsed(t *testing.T) {
	// THE WHOLE CONSOLE, not the four files the menu is built from.
	// The nav declares its icons as data and the shell calls getIcon for
	// the few that are not nav entries, and while that was all of them
	// this read those four files by name. It stopped being all of them
	// the moment the confirm dialog took its glyph from the map instead
	// of drawing three of its own - and a walker pointed at the wrong
	// place reports the new ones as orphans, which is a failure that
	// argues for deleting exactly the code that fixed the problem.
	referenced := consoleSources(t)

	used := map[string]bool{}
	for _, m := range iconRefRE.FindAllStringSubmatch(referenced, -1) {
		used[m[1]] = true
	}

	for _, call := range iconCallRE.FindAllStringSubmatch(referenced, -1) {
		for _, name := range iconNameRE.FindAllStringSubmatch(call[1], -1) {
			used[name[1]] = true
		}
	}

	defined := regexp.MustCompile(`'([a-z0-9-]+)':\s*'<svg`).FindAllStringSubmatch(readIcons(t), -1)
	if len(defined) < 20 {
		t.Fatalf("read %d icons out of icons.ts, expected at least 20 - the map has moved or "+
			"changed shape and this check is looking at the wrong thing", len(defined))
	}

	var orphans []string
	for _, m := range defined {
		if !used[m[1]] {
			orphans = append(orphans, m[1])
		}
	}

	sort.Strings(orphans)

	if len(orphans) > 0 {
		t.Errorf("getIcon defines %s and nothing renders them", strings.Join(orphans, ", "))
	}
}

type navSection struct {
	id   string
	body string
}

// navSectionBlocks cuts the navSections array into one chunk per section.
func navSectionBlocks(t *testing.T, layout string) []navSection {
	t.Helper()

	_, after, found := strings.Cut(layout, "export const NAV_GROUPS: NavGroup[] = [")
	if !found {
		t.Fatal("navigation.ts no longer declares NAV_GROUPS")
	}

	block, _, found := strings.Cut(after, "\n]\n")
	if !found {
		t.Fatal("could not find the end of the NAV_GROUPS array")
	}

	marks := sectionIDRE.FindAllStringSubmatchIndex(block, -1)
	out := make([]navSection, 0, len(marks))
	for i, m := range marks {
		end := len(block)
		if i+1 < len(marks) {
			end = marks[i+1][0]
		}

		out = append(out, navSection{id: block[m[2]:m[3]], body: block[m[0]:end]})
	}

	return out
}

// readNavigation reads the menu model - the sidebar's data, split out
// of the layout so the nav is editable without opening the shell.
func readNavigation(t *testing.T) string {
	return readConsoleFile(t, "layouts", "navigation.ts")
}

// readIcons reads the glyph map.
func readIcons(t *testing.T) string {
	return readConsoleFile(t, "layouts", "icons.ts")
}

// consoleSources concatenates every source file under web/src, so a
// check about "does anything reference this" asks the whole console
// rather than a list of filenames that has to be kept up to date.
func consoleSources(t *testing.T) string {
	t.Helper()

	root := consoleSrc(t)

	var all strings.Builder
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		if !strings.HasSuffix(path, ".vue") && !strings.HasSuffix(path, ".ts") {
			return nil
		}

		// icons.ts is the DEFINITION. Reading it as a reference would
		// make every glyph its own justification.
		if filepath.Base(path) == "icons.ts" {
			return nil
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		all.Write(body)
		all.WriteByte('\n')

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	if all.Len() < 100_000 {
		t.Fatalf("read %d bytes of console source, expected far more - the walk is looking at "+
			"the wrong place and every check over it would pass vacuously", all.Len())
	}

	return all.String()
}

func readConsoleFile(t *testing.T, parts ...string) string {
	t.Helper()

	path := filepath.Join(append([]string{consoleSrc(t)}, parts...)...)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Join(parts...), err)
	}

	return string(body)
}
