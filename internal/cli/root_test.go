// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/yousysadmin/mailyard/pkg"
)

func run(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()

	var out, errOut bytes.Buffer
	root := NewRoot()
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(args)
	err = root.Execute()

	return out.String(), errOut.String(), err
}

func TestVersionGoesToStdoutOnly(t *testing.T) {
	stdout, stderr, err := run(t, "version")
	if err != nil {
		t.Fatalf("version: %v", err)
	}

	if want := pkg.AppName + " " + pkg.Version + "\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}

	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
}

func TestVersionFlag(t *testing.T) {
	stdout, _, err := run(t, "--version")
	if err != nil {
		t.Fatalf("--version: %v", err)
	}

	if !strings.Contains(stdout, pkg.Version) {
		t.Errorf("stdout = %q, want it to contain %q", stdout, pkg.Version)
	}
}

// Nothing but main prints an error, so the streams must stay empty.
func TestUsageErrorsExitTwoAndPrintNothing(t *testing.T) {
	cases := map[string][]string{
		"unknown command":       {"bogus"},
		"unknown flag":          {"serve", "--nope"},
		"stray argument":        {"version", "extra"},
		"stray nested argument": {"tls", "status", "extra"},
		"bad flag value":        {"export-api-spec", "--surface", "nope"},
		"missing flag":          {"tls", "assign"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			stdout, stderr, err := run(t, args...)
			if err == nil {
				t.Fatal("expected an error")
			}

			if got := ExitCode(err); got != 2 {
				t.Errorf("ExitCode = %d, want 2 for %v", got, err)
			}

			if stdout != "" || stderr != "" {
				t.Errorf("command printed the error itself: stdout=%q stderr=%q", stdout, stderr)
			}
		})
	}
}

func TestExitCodeForOtherErrors(t *testing.T) {
	_, _, err := run(t, "tls", "status", "--config", t.TempDir()+"/absent.yaml")
	if err == nil {
		t.Skip("config resolved without a file; nothing to classify")
	}

	if got := ExitCode(err); got != 1 {
		t.Errorf("ExitCode = %d, want 1 for %v", got, err)
	}

	if got := ExitCode(nil); got != 0 {
		t.Errorf("ExitCode(nil) = %d, want 0", got)
	}
}

func TestNoArgsPrintsHelp(t *testing.T) {
	stdout, _, err := run(t)
	if err != nil {
		t.Fatalf("bare root: %v", err)
	}

	if !strings.Contains(stdout, "mailyard [command]") {
		t.Errorf("stdout = %q, want the help text", stdout)
	}
}

func TestHelpGroupsCommands(t *testing.T) {
	stdout, _, err := run(t, "--help")
	if err != nil {
		t.Fatalf("--help: %v", err)
	}

	for _, want := range []string{"Run a node:", "Recover an installation:", "Inspect this build:"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help lacks the %q section:\n%s", want, stdout)
		}
	}

	node := strings.Index(stdout, "Run a node:")
	serve := strings.Index(stdout, "\n  serve ")
	api := strings.Index(stdout, "\n  api ")
	if !(node < serve && serve < api) {
		t.Errorf("serve should lead its group:\n%s", stdout)
	}
}
