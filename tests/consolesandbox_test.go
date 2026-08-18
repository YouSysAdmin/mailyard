// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tests

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/yousysadmin/mailyard/internal/models/permission"
)

// The console's read-only view of an API key shows what the key can
// actually do, and for a sandbox key that is not the stored list: the
// flag carries sandbox read, write and delete on its own, and the
// column holds an empty array. So the view adds them back, from a
// constant in ApiKeyDetail.vue, and this pins that constant to ForKey.
//
// Without it the two drift silently in the direction that already
// caused trouble once: a key whose real access the console understates
// is exactly what made the flag look like it did nothing.
func TestTheConsoleNamesWhatTheSandboxFlagGrants(t *testing.T) {
	view := filepath.Join(consoleSrc(t), "views", "apikeys", "ApiKeyDetail.vue")

	body, err := os.ReadFile(view)
	if err != nil {
		t.Fatalf("read %s: %v", view, err)
	}

	m := regexp.MustCompile(`SANDBOX_GRANTS = \[([^\]]*)\]`).FindSubmatch(body)
	if m == nil {
		t.Fatalf("no SANDBOX_GRANTS in %s - if the view stopped adding them, "+
			"a sandbox key now reads as holding nothing", view)
	}

	var console []string
	for part := range strings.SplitSeq(string(m[1]), ",") {
		if s := strings.Trim(strings.TrimSpace(part), "'\"\n\t "); s != "" {
			console = append(console, s)
		}
	}

	// What the server actually grants a key that holds nothing but the
	// flag, which is the case the view has to describe.
	var server []string
	for p := range permission.ForKey(nil, true) {
		server = append(server, string(p))
	}

	sort.Strings(console)
	sort.Strings(server)
	if strings.Join(console, ",") != strings.Join(server, ",") {
		t.Errorf("the console says a sandbox key holds %v, ForKey grants %v", console, server)
	}
}
