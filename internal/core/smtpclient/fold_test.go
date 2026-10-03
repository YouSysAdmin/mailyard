// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpclient

import (
	"mime"
	"strings"
	"testing"
)

// headerBlock is the raw header section of a built message.
func headerBlock(t *testing.T, m *Message) string {
	t.Helper()
	raw, err := m.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	head, _, _ := strings.Cut(string(raw), "\r\n\r\n")

	return head
}

// field returns one header's raw text, continuation lines included.
func field(head, name string) string {
	lines := strings.Split(head, "\r\n")
	for i, l := range lines {
		if !strings.HasPrefix(l, name+": ") {
			continue
		}

		out := l
		for _, next := range lines[i+1:] {
			if !strings.HasPrefix(next, " ") && !strings.HasPrefix(next, "\t") {
				break
			}

			out += "\r\n" + next
		}

		return out
	}

	return ""
}

// No header line passes the limit RFC 5322 sets, and unfolding gives
// back the value that was asked for.
func TestALongHeaderIsFolded(t *testing.T) {
	spaced := strings.Repeat("token ", 600) + "end"
	unbroken := strings.Repeat("x", 4000)
	m := &Message{
		From: "a@example.com", To: []string{"b@example.com"}, Subject: "s", Text: "t",
		Headers: map[string]string{"X-Spaced": spaced, "X-Unbroken": unbroken},
	}
	head := headerBlock(t, m)

	for _, line := range strings.Split(head, "\r\n") {
		if len(line) > hardLineLen {
			t.Fatalf("a header line is %d octets, the limit is %d", len(line), hardLineLen)
		}
	}

	if got := strings.TrimPrefix(strings.ReplaceAll(field(head, "X-Spaced"), "\r\n", ""), "X-Spaced: "); got != spaced {
		t.Errorf("X-Spaced unfolds to a different value (%d bytes, want %d)", len(got), len(spaced))
	}

	for _, line := range strings.Split(field(head, "X-Spaced"), "\r\n") {
		if len(line) > softLineLen {
			t.Errorf("a foldable line was left at %d octets", len(line))
		}
	}

	dec := new(mime.WordDecoder)
	raw := strings.TrimPrefix(strings.ReplaceAll(field(head, "X-Unbroken"), "\r\n", ""), "X-Unbroken: ")
	got, err := dec.DecodeHeader(raw)
	if err != nil || got != unbroken {
		t.Errorf("X-Unbroken decodes to %d bytes, %v, want the %d sent", len(got), err, len(unbroken))
	}
}

// A non-ASCII custom value or display name leaves as RFC 2047 encoded
// words, never as raw UTF-8.
func TestNonASCIIHeadersAreEncoded(t *testing.T) {
	m := &Message{
		From: "Zoë <z@example.com>", To: []string{"b@example.com"}, HeaderTo: "Jürgen <j@example.com>",
		Subject: "s", Text: "t", Headers: map[string]string{"X-Note": "café au lait", "X-Plain": "ascii stays"},
	}
	head := headerBlock(t, m)

	if !isASCII(head) {
		t.Fatalf("the header block carries raw UTF-8:\n%s", head)
	}

	dec := new(mime.WordDecoder)
	for name, want := range map[string]string{"X-Note": "café au lait", "From": "Zoë <z@example.com>", "To": "Jürgen <j@example.com>"} {
		raw := strings.TrimPrefix(strings.ReplaceAll(field(head, name), "\r\n", ""), name+": ")
		if got, err := dec.DecodeHeader(raw); err != nil || got != want {
			t.Errorf("%s decodes to %q, %v, want %q", name, got, err, want)
		}
	}

	if field(head, "X-Plain") != "X-Plain: ascii stays" {
		t.Errorf("an ASCII value was rewritten: %q", field(head, "X-Plain"))
	}
}
