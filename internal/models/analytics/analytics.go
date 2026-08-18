// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package analytics holds the reporting shapes returned by the
// dashboard and trend endpoints. Pure data: the queries that build
// these live in internal/domain/analytics.
package analytics

// Summary is the project dashboard readout.
type Summary struct {
	// Emails counts rows by delivery status.
	Emails map[string]int `json:"emails"`

	// TotalEmails counts total emails
	TotalEmails int `json:"total_emails"`

	// FailureRate is failed over finalized (sent + failed), as a
	// percentage. Queued and scheduled mail is excluded: counting
	// not-yet-attempted messages as successes would flatter the
	// number, and as failures would slander it.
	FailureRate float64 `json:"failure_rate"`

	// Inbound counts received mail by status.
	Inbound map[string]int `json:"inbound"`

	// Resources counts the configured objects in the project.
	Resources map[string]int `json:"resources"`

	// Engagement is opens and clicks, which nothing reported before.
	// Every one of these was being RECORDED - the pixel marks the row
	// and writes a tracking event - and no endpoint outside a single
	// campaign read any of it back, so a project with tracking on had
	// no way to see an open.
	Engagement Engagement `json:"engagement"`
}

// Engagement is how much of the mail that COULD be measured was.
//
// The denominator is TrackedSent, never total sends, and that is the
// whole reason this is a struct rather than two floats. Mail sent with
// tracking off can never register an open, so dividing by everything
// makes a correctly configured project look ignored - and it would move
// the rate for a reason nobody could see, by sending one transactional
// message that opted out.
//
// Both counts are UNIQUE messages, not events: a reader who reopens a
// newsletter four times is one person who read it. The per-message
// totals are on the email row itself (open_count, click_count).
type Engagement struct {
	// TrackedSent is sent mail that carried a pixel or a wrapped link,
	// which is what the two rates are out of. Reported so a page can
	// say "of 120 tracked sends" rather than showing a percentage of
	// something invisible.
	TrackedSent int `json:"tracked_sent"`
	Opened      int `json:"opened"`
	Clicked     int `json:"clicked"`

	// Percentages, 0 when nothing tracked has been sent - which a page
	// must show as "no tracked sends yet" rather than as 0%, since the
	// two mean opposite things.
	OpenRate  float64 `json:"open_rate"`
	ClickRate float64 `json:"click_rate"`
}

// DayCount is one bucket of the delivery trend.
type DayCount struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}
