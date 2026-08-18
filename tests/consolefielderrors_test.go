// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

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

var (
	// `fieldErrors.alert_email` in a view, or `errors.alert_email` - the
	// composable is destructured under both names and half the console
	// uses each, so checking one name checked half the bindings.
	consoleFieldRef = regexp.MustCompile(`(?:^|[^a-zA-Z.])(?:fieldErrors|errors)\.([a-z][a-z0-9_]*)`)

	// Anything that puts a captured message on screen: a keyed read in
	// this file's own markup, or the whole map handed to a child that
	// renders it.
	consoleFieldSink = regexp.MustCompile(`(?:fieldErrors|errors)[.\[]|:errors="`)

	// The composable being asked to place a refusal on a field.
	consoleFieldCapture = regexp.MustCompile(`\bcapture\(`)

	// A struct tag the server validates: only those can come back in the
	// `fields` array, because Humanize builds it from validator errors.
	serverFieldTag = regexp.MustCompile("json:\"([a-z0-9_]+)\"[^`]*validate:\"")
)

// A FIELD ERROR IS KEYED BY THE NAME THE SERVER REFUSES IT UNDER.
//
// The console binds `:error="fieldErrors.x"` and the server answers with
// `fields: [{field: "x", ...}]` built from the json tag - so the two
// halves agree by convention and nothing connects them. A view that
// binds `hostname` where the request sends `host` renders no error at
// all: the key is simply absent from the map, the field stays quiet, and
// the reader is told nothing. There is no failure to notice, which is
// what makes it worth a test.
//
// The check is deliberately loose about which endpoint: a view is not
// tied to one handler here, and `name` is validated by twenty domains.
// What it catches is a key no handler anywhere could produce - a typo, a
// local ref name that was never the wire name, or a field the server
// stopped validating.
func TestEveryFieldErrorKeyIsOneTheServerRefuses(t *testing.T) {
	root := repoRoot(t)

	// Every json name carrying a validate rule, across the domains.
	known := map[string]bool{}
	domains := filepath.Join(root, "internal", "domain")
	err := filepath.Walk(domains, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		for _, m := range serverFieldTag.FindAllStringSubmatch(string(body), -1) {
			known[m[1]] = true
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", domains, err)
	}

	if len(known) < 50 {
		t.Fatalf("read %d validated field names out of internal/domain, expected at least 50 - "+
			"the tags have moved and this check is looking at the wrong thing", len(known))
	}

	var findings []string
	console := consoleSrc(t)
	err = filepath.Walk(console, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".vue") {
			return err
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		rel, _ := filepath.Rel(console, path)
		for i, line := range strings.Split(string(body), "\n") {
			for _, m := range consoleFieldRef.FindAllStringSubmatch(line, -1) {
				if !known[m[1]] {
					findings = append(findings, filepath.ToSlash(rel)+":"+strconv.Itoa(i+1)+
						" binds fieldErrors."+m[1]+", which no handler validates")
				}
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", console, err)
	}

	sort.Strings(findings)

	if len(findings) > 0 {
		t.Errorf("%d field error key(s) name something the server never refuses:\n%s\n"+
			"the key is the json name in the request body, not the name of the ref "+
			"holding it",
			len(findings), strings.Join(findings, "\n"))
	}
}

// A BOUND KEY MUST ALSO BE ONE THE REQUEST CARRIES.
//
// The check above knows the key is a name SOME handler refuses. It
// cannot know it is the name this form sends, and that gap is not
// theoretical: the create-project dialog bound `language` while the
// body carries `default_language`. `language` is a real field - the
// send-email endpoint validates it - so the first check passed, and the
// dialog would have gone on silently refusing to explain itself.
//
// The payload is built either in the view or in the api module it calls,
// so both are read. Keys are collected loosely, which makes this a check
// for a key that exists NOWHERE rather than a proof that it belongs to
// this endpoint. That is the failure worth catching: a bound key nothing
// sends renders nothing, forever, with no error to notice.
func TestEveryFieldErrorKeyIsOneTheFormSends(t *testing.T) {
	console := consoleSrc(t)
	key := regexp.MustCompile(`([a-z_][a-z0-9_]*)\??\s*:`)

	// Per api module, because the union of all of them is no test at
	// all: `language` is a real key in emails.ts, which is exactly how
	// the create-project dialog got away with binding it.
	apiDir := filepath.Join(console, "api")
	moduleKeys := map[string]map[string]bool{}
	entries, err := os.ReadDir(apiDir)
	if err != nil {
		t.Fatalf("read %s: %v", apiDir, err)
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".ts") {
			continue
		}

		body, err := os.ReadFile(filepath.Join(apiDir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}

		keys := map[string]bool{}
		for _, m := range key.FindAllStringSubmatch(string(body), -1) {
			keys[m[1]] = true
		}

		moduleKeys[strings.TrimSuffix(e.Name(), ".ts")] = keys
	}

	imports := regexp.MustCompile(`from '[^']*api/([a-zA-Z]+)'`)
	// `types` mirrors every model in the product and `client` is the
	// transport - neither describes what a form SENDS, and counting
	// them puts every name in the console back in scope.
	notABody := map[string]bool{"types": true, "client": true, "eventstream": true}

	var findings []string
	err = filepath.Walk(console, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".vue") {
			return err
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		text := string(body)
		// Only where the map is MADE. A child that takes `errors` as a
		// prop renders keys its parent sends, and the request it belongs
		// to is not visible from here at all - CampaignFields binds six
		// of them and imports no api module, which is not a finding.
		if !strings.Contains(text, "useFieldErrors(") || !consoleFieldRef.MatchString(text) {
			return nil
		}

		rel, _ := filepath.Rel(console, path)

		// Keys the view itself builds, plus those of the api modules it
		// actually calls.
		local := map[string]bool{}
		for _, m := range imports.FindAllStringSubmatch(text, -1) {
			if notABody[m[1]] {
				continue
			}

			for k := range moduleKeys[m[1]] {
				local[k] = true
			}
		}

		if i := strings.Index(text, "<template>"); i > 0 {
			for _, m := range key.FindAllStringSubmatch(text[:i], -1) {
				local[m[1]] = true
			}

			for _, m := range regexp.MustCompile(`payload\.([a-z_][a-z0-9_]*)`).FindAllStringSubmatch(text[:i], -1) {
				local[m[1]] = true
			}
		}

		for i, line := range strings.Split(text, "\n") {
			for _, m := range consoleFieldRef.FindAllStringSubmatch(line, -1) {
				if !local[m[1]] {
					findings = append(findings, filepath.ToSlash(rel)+":"+strconv.Itoa(i+1)+
						" binds fieldErrors."+m[1]+", which nothing here sends")
				}
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", console, err)
	}

	sort.Strings(findings)

	if len(findings) > 0 {
		t.Errorf("%d field error key(s) name nothing the console sends:\n%s",
			len(findings), strings.Join(findings, "\n"))
	}
}

// A CAPTURED REFUSAL MUST HAVE SOMEWHERE TO GO.
//
// `capture(err)` files the server's messages by field and answers whether
// it filed any - and every call site reads that answer as "the message is
// on screen, so do not toast as well". In a component that renders no
// field errors it is not: the map is written, nothing reads it, the toast
// is suppressed, and the request fails in complete silence.
//
// Three did exactly that. Attaching a file to a template refused a
// filename over 255 characters and the page simply carried on; importing
// subscribers and importing a template did the same. None of the three
// could have rendered the message anyway - the refusal names a leaf field
// of a pasted document, and there is no input on screen for it - which is
// the point: capture is the wrong tool wherever the form has no field to
// put the answer under, and the server's summary line already reads well.
//
// Passing the map to a child counts, since that is how the campaign forms
// render theirs.
func TestACapturedFieldErrorHasSomewhereToGo(t *testing.T) {
	console := consoleSrc(t)

	var findings []string
	err := filepath.Walk(console, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".vue") {
			return err
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		text := string(body)
		if !consoleFieldCapture.MatchString(text) || consoleFieldSink.MatchString(text) {
			return nil
		}

		rel, _ := filepath.Rel(console, path)
		findings = append(findings, filepath.ToSlash(rel))

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", console, err)
	}

	sort.Strings(findings)

	if len(findings) > 0 {
		t.Errorf("%d component(s) call capture() with nowhere to render what it captures:\n  %s\n"+
			"a captured refusal that nothing displays suppresses the toast as well, so the "+
			"request fails silently - either bind the key on a FormField, pass :errors to the "+
			"child that renders it, or drop capture and report apiErrorMessage plainly",
			len(findings), strings.Join(findings, "\n  "))
	}
}
