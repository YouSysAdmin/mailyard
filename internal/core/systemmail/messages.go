// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package systemmail

import (
	"fmt"
	"html"
	"strings"
)

// The platform's own messages are built here rather than through
// internal/core/render: they must work on an install with no
// templates, no project, and no database rows at all (a password
// reset happens before the user can log in to fix anything). Plain
// string building keeps them dependency-free and reviewable.

// Invitation renders the project invitation message.
func Invitation(projectName, inviterEmail, acceptURL string, expiresHours int) (subject, htmlBody, textBody string) {
	subject = fmt.Sprintf("You have been invited to the %s project", projectName)

	intro := fmt.Sprintf("%s invited you to join the %s project on Mailyard.",
		fallback(inviterEmail, "A project admin"), projectName)
	expiry := fmt.Sprintf("This invitation expires in %d hours.", expiresHours)

	htmlBody = layout(
		"Project invitation",
		intro,
		"Accept invitation",
		acceptURL,
		expiry,
	)
	textBody = plain(intro, acceptURL, expiry)

	return subject, htmlBody, textBody
}

// PasswordReset renders the password reset message.
func PasswordReset(resetURL string, expiresMinutes int) (subject, htmlBody, textBody string) {
	subject = "Reset your Mailyard password"

	intro := "Somebody asked to reset the password for this address."
	expiry := fmt.Sprintf(
		"The link expires in %d minutes and can be used once. If you did not ask for this, ignore this message - your password has not changed.",
		expiresMinutes)

	htmlBody = layout(
		"Password reset",
		intro,
		"Choose a new password",
		resetURL,
		expiry,
	)
	textBody = plain(intro, resetURL, expiry)

	return subject, htmlBody, textBody
}

// SignupVerification renders the confirm-your-address message sent
// after public self-registration.
func SignupVerification(verifyURL string, expiresHours int) (subject, htmlBody, textBody string) {
	subject = "Confirm your email address"

	intro := "An account on Mailyard was just created with this address. Confirm it to finish signing up."
	expiry := fmt.Sprintf(
		"The link expires in %d hours and can be used once. If you did not create this account, ignore this message and nothing will happen.",
		expiresHours)

	htmlBody = layout(
		"Confirm your email",
		intro,
		"Confirm email address",
		verifyURL,
		expiry,
	)
	textBody = plain(intro, verifyURL, expiry)

	return subject, htmlBody, textBody
}

// layout wraps one call to action in a minimal, table-free HTML
// document. Inline styles only, no external assets - this has to
// render in clients that block everything.
func layout(heading, intro, buttonLabel, buttonURL, footer string) string {
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8">`)
	b.WriteString(`<meta name="viewport" content="width=device-width,initial-scale=1"></head>`)
	b.WriteString(`<body style="margin:0;padding:24px;background:#f5f6f8;`)
	b.WriteString(`font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;color:#1f2329">`)
	b.WriteString(`<div style="max-width:520px;margin:0 auto;background:#ffffff;border-radius:8px;padding:32px">`)

	b.WriteString(`<h1 style="margin:0 0 16px;font-size:20px;font-weight:600">`)
	b.WriteString(html.EscapeString(heading))
	b.WriteString(`</h1>`)

	b.WriteString(`<p style="margin:0 0 24px;font-size:15px;line-height:1.5">`)
	b.WriteString(html.EscapeString(intro))
	b.WriteString(`</p>`)

	b.WriteString(`<p style="margin:0 0 24px"><a href="`)
	b.WriteString(html.EscapeString(buttonURL))
	b.WriteString(`" style="display:inline-block;padding:11px 20px;background:#2563eb;color:#ffffff;`)
	b.WriteString(`text-decoration:none;border-radius:6px;font-size:15px;font-weight:500">`)
	b.WriteString(html.EscapeString(buttonLabel))
	b.WriteString(`</a></p>`)

	b.WriteString(`<p style="margin:0 0 8px;font-size:13px;color:#5c6470">`)
	b.WriteString(`Or paste this link into your browser:</p>`)
	b.WriteString(`<p style="margin:0 0 24px;font-size:13px;word-break:break-all"><a href="`)
	b.WriteString(html.EscapeString(buttonURL))
	b.WriteString(`" style="color:#2563eb">`)
	b.WriteString(html.EscapeString(buttonURL))
	b.WriteString(`</a></p>`)

	b.WriteString(`<p style="margin:0;font-size:13px;color:#5c6470;line-height:1.5">`)
	b.WriteString(html.EscapeString(footer))
	b.WriteString(`</p></div></body></html>`)

	return b.String()
}

// plain renders the text/plain alternative.
func plain(intro, url, footer string) string {
	return intro + "\n\n" + url + "\n\n" + footer + "\n"
}

func fallback(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}

	return s
}
