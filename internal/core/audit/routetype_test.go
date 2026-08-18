// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package audit

import "testing"

func TestRouteType(t *testing.T) {
	const uid = "b4f9bd4a-f184-4e93-a40c-14e7f7181242"

	cases := []struct {
		method, path, want string
	}{
		{"POST", "/api/api-keys/", "apikey.created"},
		{"DELETE", "/api/api-keys/" + uid, "apikey.deleted"},
		{"POST", "/api/api-keys/" + uid + "/revoke", "apikey.revoke"},
		{"POST", "/api/smtp-credentials/", "smtpcredential.created"},
		{"POST", "/api/smtp-servers/" + uid + "/test", "smtpserver.test"},
		{"PATCH", "/api/smtp-servers/" + uid, "smtpserver.updated"},
		{"POST", "/api/templates/", "template.created"},
		{"PUT", "/api/settings", "setting.updated"},
		{"POST", "/api/projects/" + uid + "/members", "project.member.created"},
		{"DELETE", "/api/projects/" + uid + "/invitations/" + uid, "project.invitation.deleted"},
		{"POST", "/api/campaigns/" + uid + "/send", "campaign.send"},
		{"POST", "/api/v1/emails/send", "email.send"},
		{"DELETE", "/api/subscriber-lists/" + uid + "/members/" + uid, "subscriberlist.member.deleted"},
		// Numeric ids are treated as identifiers too.
		{"DELETE", "/api/languages/42", "language.deleted"},
	}
	for _, c := range cases {
		if got := RouteType(c.method, c.path); got != c.want {
			t.Errorf("RouteType(%s %s) = %q, want %q", c.method, c.path, got, c.want)
		}
	}
}

func TestSingular(t *testing.T) {
	cases := map[string]string{
		"api-keys":         "apikey",
		"smtp-servers":     "smtpserver",
		"templates":        "template",
		"settings":         "setting",
		"subscriber-lists": "subscriberlist",
		"stylesheets":      "stylesheet",
		// Words already singular must survive untouched.
		"usage":  "usage",
		"health": "health",
	}
	for in, want := range cases {
		if got := singular(in); got != want {
			t.Errorf("singular(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLooksLikeID(t *testing.T) {
	yes := []string{
		"b4f9bd4a-f184-4e93-a40c-14e7f7181242",
		"42",
		"0",
		"5cfaeef2be5565a23016ed0684a09717",
	}
	no := []string{"revoke", "test", "members", "send", "enable", "attachments"}
	for _, s := range yes {
		if !looksLikeID(s) {
			t.Errorf("looksLikeID(%q) = false, want true", s)
		}
	}

	for _, s := range no {
		if looksLikeID(s) {
			t.Errorf("looksLikeID(%q) = true, want false", s)
		}
	}
}
