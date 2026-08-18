// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package alertmail turns audit events and project alerts into mail.
//
// It exists because everything this installation knew about a problem
// stayed on a screen. The bounce-rate job raised an in-app notification
// and nothing else - every security event - a key created, a second factor
// turned off, a platform credential minted - went to the audit trail and
// no further. The only thing that ever mailed anybody about a problem was
// the certificate expiry sweep.
//
// One consumer of the audit stream (see audit.Recorder.Watch), because
// both trails already meet in its writer goroutine: Project and Security
// events go through the same queue, so a watcher there sees everything
// and nothing needs hooking twice. It is also already off the request
// path, which is what makes resolving recipients and sending mail safe to
// do from it.
//
// What gets mailed is an EXPLICIT list in events.go, never a rule over
// the stream: every successful mutating request produces an event.
package alertmail

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/safego"
	amodel "github.com/yousysadmin/mailyard/internal/models/audit"
	nmodel "github.com/yousysadmin/mailyard/internal/models/notification"
)

// Mailer is the part of platform mail this needs. Satisfied by
// systemmail.Sender, whose SendAsync is already a no-op when platform
// mail is unconfigured.
type Mailer interface {
	Enabled() bool
	SendAsync(to []string, subject, html, text string)
}

// Recipients resolves an audience. An interface rather than the stores
// themselves: this package sits in core and may not import the domain
// packages, and the resolution is three queries that belong beside the
// membership rules they read.
type Recipients interface {
	// ProjectAlert is the project's owners plus its alert address.
	ProjectAlert(ctx context.Context, projectID string) ([]string, error)

	// PlatformAdmins is every enabled administrator of the installation.
	PlatformAdmins(ctx context.Context) ([]string, error)

	// UserEmail is one account's address, empty when it is disabled or
	// gone.
	UserEmail(ctx context.Context, userID string) (string, error)
}

// collapseWindow is how long one (audience, event type) pair stays quiet
// after a mail.
//
// It exists because a script is a normal caller here: minting twenty API
// keys in a loop is one intention and twenty audit events, and twenty
// mails about it is how somebody comes to filter the whole channel. The
// trail keeps every one - this only decides how often somebody is
// nudged to look at it.
const collapseWindow = 10 * time.Minute

// Notifier composes and sends the alerts.
type Notifier struct {
	Mail       Mailer
	Recipients Recipients

	// Enabled gates the whole feature, read fresh so an administrator
	// turning it off takes effect without a restart.
	Enabled func() bool

	// ConsoleURL is the base a "look at the trail" link is built from.
	// Empty means the mail carries no link, which is the right answer on
	// an install with no public URL rather than a link to nowhere.
	ConsoleURL string
	Log        *slog.Logger

	mu   sync.Mutex
	sent map[string]time.Time
}

// OnAudit is the audit watcher. Cheap for the overwhelming majority of
// events: one map lookup and a return.
func (n *Notifier) OnAudit(e *amodel.Event) {
	if n == nil || e == nil || !n.on() {
		return
	}

	a, ok := Lookup(e.Type)
	if !ok {
		return
	}

	// Off the audit writer's goroutine from here: resolving recipients is
	// a database round trip, and this loop is what drains the trail.
	safego.Go(n.Log, "alertmail: audit", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		n.deliver(ctx, a, e)
	})
}

// OnNotification mails a project alert that was also raised in the
// console - a bounce rate over the threshold, a plan limit reached.
//
// A separate entry point rather than another audit type, because these
// are not requests: nobody did anything, a job noticed something. The
// composing and the collapsing are shared, which is the point of them
// living in one package.
func (n *Notifier) OnNotification(note *nmodel.Notification) {
	if n == nil || note == nil || !n.on() {
		return
	}

	// Only the ones that mean something is wrong. An informational
	// notification is what the console badge is for.
	if note.Severity != nmodel.SeverityWarning && note.Severity != nmodel.SeverityError {
		return
	}

	if note.ProjectID == "" {
		return
	}

	safego.Go(n.Log, "alertmail: notification", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		to, err := n.Recipients.ProjectAlert(ctx, note.ProjectID)
		if err != nil {
			n.Log.Error("alertmail: recipients", "project_id", note.ProjectID, "err", err)

			return
		}

		if len(to) == 0 {
			return
		}

		if !n.claim("note:" + note.ProjectID + ":" + note.Type) {
			return
		}

		subject, html, text := Message(note.Title, note.Body, "", "",
			n.link("/notifications"))
		n.Mail.SendAsync(to, subject, html, text)
		n.Log.Info("alertmail: project alert mailed",
			"project_id", note.ProjectID, "type", note.Type, "recipients", len(to))
	})
}

func (n *Notifier) on() bool {
	if n.Mail == nil || n.Recipients == nil {
		return false
	}

	if n.Enabled != nil && !n.Enabled() {
		return false
	}

	// Asked here rather than trusted to SendAsync's own no-op: without
	// platform mail there is no point resolving an audience.
	return n.Mail.Enabled()
}

func (n *Notifier) deliver(ctx context.Context, a Alert, e *amodel.Event) {
	to, key, err := n.audience(ctx, a, e)
	if err != nil {
		n.Log.Error("alertmail: recipients", "type", e.Type, "err", err)

		return
	}

	if len(to) == 0 {
		// Ordinary, not an error: a project with no owner and no alert
		// address, or an account event for a user that has been deleted.
		return
	}

	if !n.claim(key) {
		n.Log.Debug("alertmail: collapsed", "type", e.Type, "key", key)

		return
	}

	actor := e.ActorEmail
	if actor == "" {
		actor = e.ActorID
	}

	subject, html, text := Message(a.Heading, a.Note, actor,
		strings.TrimSpace(e.Method+" "+e.Path), n.trailLink(a.Tier))
	n.Mail.SendAsync(to, subject, html, text)
	n.Log.Info("alertmail: sent", "type", e.Type, "recipients", len(to))
}

// audience resolves who hears about this event, and the key its
// collapsing is counted against.
func (n *Notifier) audience(ctx context.Context, a Alert, e *amodel.Event) ([]string, string, error) {
	switch a.Tier {
	case TierAccount:
		if e.ActorID == "" {
			return nil, "", nil
		}

		addr, err := n.Recipients.UserEmail(ctx, e.ActorID)
		if err != nil || addr == "" {
			return nil, "", err
		}

		return []string{addr}, "user:" + e.ActorID + ":" + e.Type, nil
	case TierProject:
		// An event with no project cannot be told who to tell. It happens:
		// the /projects routes decide access in handlers, and one of them
		// is deleting the project itself.
		if e.ProjectID == "" {
			return nil, "", nil
		}

		to, err := n.Recipients.ProjectAlert(ctx, e.ProjectID)

		return to, "project:" + e.ProjectID + ":" + e.Type, err
	default:
		to, err := n.Recipients.PlatformAdmins(ctx)

		return to, "platform:" + e.Type, err
	}
}

// claim reports whether this audience may be mailed now, and records the
// send when it may.
func (n *Notifier) claim(key string) bool {
	now := time.Now()
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.sent == nil {
		n.sent = map[string]time.Time{}
	}

	if last, ok := n.sent[key]; ok && now.Sub(last) < collapseWindow {
		return false
	}

	// Swept here rather than on a timer: the map is only ever touched
	// from this function, and an install quiet enough not to reach it
	// does not need the memory back.
	for k, t := range n.sent {
		if now.Sub(t) > collapseWindow {
			delete(n.sent, k)
		}
	}

	n.sent[key] = now

	return true
}

// link builds a console URL, empty when this installation does not know
// its own address.
func (n *Notifier) link(path string) string {
	if n.ConsoleURL == "" {
		return ""
	}

	return strings.TrimRight(n.ConsoleURL, "/") + path
}

// trailLink points at whichever log recorded the event.
func (n *Notifier) trailLink(t Tier) string {
	if t == TierAccount {
		return n.link("/security")
	}

	return n.link("/audit-log")
}

// Message renders one alert. Exported so the wording can be read in a
// test without a Notifier, a mailer or a database.
func Message(heading, note, actor, action, linkURL string) (subject, html, text string) {
	var detail []string
	if actor != "" {
		detail = append(detail, "Who: "+actor)
	}

	if action != "" {
		detail = append(detail, "What: "+action)
	}

	detail = append(detail, fmt.Sprintf("When: %s", time.Now().UTC().Format(time.RFC1123)))

	return heading, alertHTML(heading, note, detail, linkURL), alertText(heading, note, detail, linkURL)
}
