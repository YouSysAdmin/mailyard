// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// A rule about the console, checked by reading web/src - which is why it
// lives beside it. These sat in internal/server and
// internal/domain/trackingpage, packages that have nothing to do with
// what they check and were simply where somebody was working at the time.

package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The console must never render a stored body with a raw iframe.
//
// Render `html_body` into an <iframe srcdoc> and the browser fetches the
// open pixel this package serves. The operator who came to see whether a
// message was opened is then the one opening it, and the project's open
// rate counts its own staff.
//
// It cannot be fixed on the server. The preview frame is sandbox="" with
// no allow-same-origin, so its origin is opaque and the pixel request is
// cross-site: no session cookie arrives to recognise it by, and a real
// webmail's fetch looks identical. Making the frame credentialed to
// create a signal would hand a session to markup written by whoever sent
// the message, which is worse than a wrong statistic.
//
// So the rule is a display rule, and this test keeps it from being one
// view's private habit: every preview goes through
// components/HtmlPreview.vue, which strips our own tracking markup. A
// new page hand-writing an iframe would start counting again with
// nothing else in the tree noticing.
func TestEveryHTMLPreviewGoesThroughTheComponent(t *testing.T) {
	const component = "components/HtmlPreview.vue"
	root := consoleSrc(t)

	var offenders []string
	seen := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() || !strings.HasSuffix(path, ".vue") {
			return nil
		}

		rel := filepath.ToSlash(strings.TrimPrefix(path, root+string(filepath.Separator)))
		if rel == component {
			seen++

			return nil
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		if strings.Contains(string(body), "srcdoc") {
			offenders = append(offenders, rel)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	if seen != 1 {
		t.Fatalf("%s was not found - this test would pass vacuously, and every "+
			"preview in the console would be unguarded", component)
	}

	for _, f := range offenders {
		t.Errorf("%s renders an iframe srcdoc directly. Use HtmlPreview instead: a stored "+
			"body carries our open pixel, and the browser fetching it records an open "+
			"against a message the operator was only looking at", f)
	}
}

// And the component has to actually remove the two things it exists to
// remove. Pinned as strings because they are the shapes the message
// builder emits - a rename there without a change here would leave the
// component looking correct and stripping nothing.
func TestThePreviewComponentStripsOurTrackingMarkup(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(repoRoot(t), "web", "src",
		"components", "HtmlPreview.vue"))
	if err != nil {
		t.Fatalf("read the preview component: %v", err)
	}

	src := string(body)
	// The ESCAPED forms, as they appear inside the regexes. The plain
	// paths appear in the comments too, so matching those would pass on a
	// component that only talks about stripping - which is what the first
	// cut of this test did.
	for _, want := range []string{`\/tracking\/open\/`, `\/tracking\/click\/`} {
		if !strings.Contains(src, want) {
			t.Errorf("HtmlPreview.vue has no pattern matching %s, so it strips nothing - "+
				"the console would record an open every time somebody looked at a message", want)
		}
	}

	// An empty sandbox, which is what denies the frame an origin. Checked
	// as the ATTRIBUTE: a comment explaining that same-origin is not
	// granted contains the words, so grepping for allow-same-origin
	// failed on a component that was already correct.
	if !strings.Contains(src, `sandbox=""`) {
		t.Error(`HtmlPreview.vue does not render sandbox="" - sender-authored markup would ` +
			`share the console's origin, and its session with it`)
	}

	if strings.Contains(src, `sandbox="allow`) {
		t.Error("HtmlPreview.vue grants sandbox permissions. The body inside was written by " +
			"whoever sent the message, not by us")
	}
}
