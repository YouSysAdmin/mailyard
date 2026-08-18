// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tracking

import "testing"

// The list has to separate prefetch from display, and getting it wrong
// is silent in both directions: a scanner in the "human" column
// invents opens nobody made, and a display proxy in the "bot" column
// throws real ones away. This pins both sides with the user agents
// that actually turn up.
func TestIsBotUA(t *testing.T) {
	// Fetched because a person opened the message. These are opens.
	humans := map[string]string{
		"Gmail image proxy": "Mozilla/5.0 (Windows NT 5.1; rv:11.0) Gecko Firefox/11.0 (via ggpht.com GoogleImageProxy)",
		"Yahoo mail proxy":  "Mozilla/5.0 YahooMailProxy; https://help.yahoo.com/kb/yahoo-mail-proxy-SLN28749.html",
		"Apple Mail macOS":  "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko)",
		"iPhone Mail":       "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15",
		"Outlook on Edge":   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120 Safari/537.36 Edg/120",
		"Thunderbird":       "Mozilla/5.0 (X11; Linux x86_64; rv:115.0) Gecko/20100101 Thunderbird/115.0",
	}
	for name, ua := range humans {
		if IsBotUA(ua) {
			t.Errorf("%s classified as a bot (matched %q) - real opens would be discarded",
				name, BotReason(ua))
		}
	}

	// Fetched on the machine's own account, before or without anyone
	// reading the message. Counting these fabricates opens.
	bots := map[string]string{
		"Proofpoint":  "Mozilla/5.0 (compatible; proofpoint-urldefense)",
		"Barracuda":   "Barracuda Sentinel",
		"Mimecast":    "Mimecast-Attachment-Protect",
		"curl":        "curl/8.4.0",
		"wget":        "Wget/1.21.4",
		"Go client":   "Go-http-client/2.0",
		"python":      "python-requests/2.31.0",
		"headless":    "Mozilla/5.0 HeadlessChrome/120.0.0.0",
		"generic bot": "Some-Random-Bot/1.0",
		"empty":       "",
	}
	for name := range bots {
		if !IsBotUA(bots[name]) {
			t.Errorf("%s classified as human - it would fabricate an open", name)
		}
	}
}

// BotReason has to name the match, because the tracking handler logs
// it as the only explanation an operator gets for a discarded open.
func TestBotReasonNamesTheMatch(t *testing.T) {
	if got := BotReason("curl/8.4.0"); got != "curl/" {
		t.Errorf("BotReason(curl) = %q, want %q", got, "curl/")
	}

	if got := BotReason(""); got != "empty user agent" {
		t.Errorf("BotReason(empty) = %q", got)
	}

	if got := BotReason("Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)"); got != "" {
		t.Errorf("BotReason(iPhone) = %q, want empty", got)
	}
}
