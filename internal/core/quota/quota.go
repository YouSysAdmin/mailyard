// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package quota enforces per-project plan limits. It sits in core
// (not domain/plan) so the email service and resource handlers can
// depend on it without import cycles through env.Runtime.
//
// RESOURCE counts still come from the primary tables - api keys, servers,
// domains and subscribers are bounded by what a person made, so counting
// them is a single indexed lookup.
//
// VOLUME comes from email_volume, a per-minute counter written in the same
// statement as the email row. That is the one place a derived count was
// too expensive to keep deriving: two COUNT(*) over the emails table on
// every send, 45ms together at 1.2M rows and growing with the project's
// own traffic. The counter is only ever incremented and only at accept
// time, so unlike a per-status counter it has no transition that could
// make it drift.
package quota

import (
	"context"
	"fmt"
	"time"

	"github.com/yousysadmin/mailyard/internal/domain/store"
	pmodel "github.com/yousysadmin/mailyard/internal/models/plan"
)

// Error marks a quota rejection. The HTTP surface maps it to 429 and
// the SMTP relay to a transient 452 - the caller can try again after
// the window rolls or the operator raises the plan.
type Error struct{ msg string }

// Error renders the failure for a log or a caller.
func (e *Error) Error() string { return e.msg }

func errf(format string, args ...any) *Error {
	return &Error{msg: fmt.Sprintf(format, args...)}
}

// Resource names for HoldResource.
const (
	ResAPIKeys     = "api keys"
	ResSMTPServers = "smtp servers"
	ResDomains     = "domains"
	ResSubscribers = "subscribers"

	// ResTemplateAssetBytes is measured in bytes, not rows: adding is
	// the size of the image about to be stored.
	ResTemplateAssetBytes = "bytes of template images"
)

// planFor resolves the project's effective plan: explicit
// assignment first, the default plan otherwise, nil meaning
// unlimited.
func planFor(ctx context.Context, st *store.Store, projID string) (*pmodel.Plan, error) {
	w, err := st.Project.Get(ctx, projID)
	if err != nil || w == nil {
		return nil, err
	}

	if w.PlanID != "" {
		return st.Plan.Get(ctx, w.PlanID)
	}

	return st.Plan.GetDefault(ctx)
}

// Observer is told what a volume check saw.
//
// It exists because the check is the only place that already knows the
// count AND the limit, so warning before the wall and reporting the wall
// itself cost nothing here and would cost a second pair of COUNT queries
// anywhere else. Optional - nil means the check just answers.
//
// Called with the window's own numbers, never a percentage: whoever
// decides what "close" means should see both.
type Observer func(window string, used, limit int, plan string)

// Window names, as an Observer receives them.
const (
	WindowHour = "hour"
	WindowDay  = "day"
)

// Volume is the send limits a plan sets, zero meaning unbounded, for
// the insert that has to hold them.
type Volume struct {
	Hourly int
	Daily  int
	Plan   string
}

// Bounded reports whether either window has a limit.
func (v Volume) Bounded() bool { return v.Hourly > 0 || v.Daily > 0 }

// Refused is the error for a send the insert refused in window, one of
// WindowHour or WindowDay.
func (v Volume) Refused(window string) error {
	if window == WindowHour {
		return errf("hourly email limit reached (%d per hour on plan %q)", v.Hourly, v.Plan)
	}

	return errf("daily email limit reached (%d per day on plan %q)", v.Daily, v.Plan)
}

// CheckSend enforces the hourly and daily email volume limits for
// one new send, and returns the limits so the insert can hold them.
//
// This read refuses early and feeds the observer. It cannot hold the
// limit alone - sends arriving together all read limit-1 - so a bounded
// plan's insert goes through EmailStore.PutWithin, which counts again
// under a per-project lock in the same transaction as the insert.
func CheckSend(ctx context.Context, st *store.Store, projID string, obs Observer) (Volume, error) {
	p, err := planFor(ctx, st, projID)
	if err != nil {
		return Volume{}, err
	}

	if p == nil || (p.HourlyEmailLimit <= 0 && p.DailyEmailLimit <= 0) {
		return Volume{}, nil
	}

	vol := Volume{Hourly: max(p.HourlyEmailLimit, 0), Daily: max(p.DailyEmailLimit, 0), Plan: p.Name}

	now := time.Now().UTC()
	if p.HourlyEmailLimit > 0 {
		n, err := st.Email.AcceptedSince(ctx, projID, now.Add(-time.Hour))
		if err != nil {
			return vol, err
		}

		if obs != nil {
			obs(WindowHour, n, p.HourlyEmailLimit, p.Name)
		}

		if n >= p.HourlyEmailLimit {
			return vol, vol.Refused(WindowHour)
		}
	}

	if p.DailyEmailLimit > 0 {
		n, err := st.Email.AcceptedSince(ctx, projID, now.Add(-24*time.Hour))
		if err != nil {
			return vol, err
		}

		if obs != nil {
			obs(WindowDay, n, p.DailyEmailLimit, p.Name)
		}

		if n >= p.DailyEmailLimit {
			return vol, vol.Refused(WindowDay)
		}
	}

	return vol, nil
}

// HoldResource enforces a resource cap before creating adding more of
// the named resource, and keeps the cap held until release is called.
// adding is how much the caller is about to create: 1 for a single
// create, N for an import, the size for a resource counted in bytes.
//
// The count and the create are two statements in two places, so on a
// bounded plan the count is taken under a per-project lock for that
// resource and the caller creates BEFORE releasing it - otherwise
// concurrent creates each see room for one more. release is never nil
// and is safe to call on every path, refusal included.
func HoldResource(ctx context.Context, st *store.Store, projID string, resource string, adding int64) (func(), error) {
	noop := func() {}

	p, err := planFor(ctx, st, projID)
	if err != nil {
		return noop, err
	}

	if p == nil {
		return noop, nil
	}

	var limit int64
	var count func(context.Context, string) (int64, error)
	switch resource {
	case ResAPIKeys:
		limit, count = int64(p.MaxAPIKeys), counting(st.APIKey.Count)
	case ResSMTPServers:
		limit, count = int64(p.MaxSMTPServers), counting(st.SMTPServer.Count)
	case ResDomains:
		limit, count = int64(p.MaxDomains), counting(st.Domain.Count)
	case ResSubscribers:
		limit, count = int64(p.MaxSubscribers), counting(st.Subscriber.Count)
	case ResTemplateAssetBytes:
		limit, count = p.MaxTemplateAssetBytes, st.Template.AssetBytes
	default:
		return noop, fmt.Errorf("unknown quota resource %q", resource)
	}

	if limit <= 0 {
		return noop, nil
	}

	release := noop
	if st.Locks != nil {
		held, err := st.Locks.Hold(ctx, "quota."+resource, projID)
		if err != nil {
			return noop, err
		}

		release = held
	}

	current, err := count(ctx, projID)
	if err != nil {
		release()

		return noop, err
	}

	if current+adding > limit {
		release()

		return noop, errf("plan %q allows at most %d %s (currently %d)", p.Name, limit, resource, current)
	}

	return release, nil
}

// counting widens a row counter to the type a byte counter already has.
func counting(fn func(context.Context, string) (int, error)) func(context.Context, string) (int64, error) {
	return func(ctx context.Context, projID string) (int64, error) {
		n, err := fn(ctx, projID)

		return int64(n), err
	}
}

// Sandbox is what a project's plan allows its sandbox: the ring buffer,
// and the CEILING on the retention window the project may choose.
//
// Here rather than in the sandbox service because this is the one place
// that knows how a project gets a plan (its own, else the default, else
// none) - and a second answer to that would be a second thing to keep
// true. Zero means unbounded, exactly as it does for every other limit in
// this package.
func Sandbox(ctx context.Context, st *store.Store, projID string) (maxMessages, maxRetentionDays int, err error) {
	p, err := planFor(ctx, st, projID)
	if err != nil || p == nil {
		return 0, 0, err
	}

	return p.MaxSandboxMessages, p.MaxSandboxRetentionDays, nil
}

// Counts is what a project has consumed right now.
//
// A declared struct rather than a map[string]int. The dashboard's limit
// tiles read it, so it is a wire shape, and as a map the console
// document describes GET /usage as nothing at all - leaving the three
// generated clients with an untyped object for the one endpoint that
// says how close a project is to being refused.
//
// The json names are unchanged, and match PlanUsage in
// web/src/api/plans.ts field for field.
type Counts struct {
	SandboxMessages int `json:"sandbox_messages"`
	EmailsLastHour  int `json:"emails_last_hour"`
	EmailsLastDay   int `json:"emails_last_day"`
	APIKeys         int `json:"api_keys"`
	SMTPServers     int `json:"smtp_servers"`
	Domains         int `json:"domains"`
	Subscribers     int `json:"subscribers"`

	// TemplateAssetBytes is the builder image storage in use, in bytes.
	TemplateAssetBytes int64 `json:"template_asset_bytes"`
}

// Usage is the read model behind GET /api/usage: what the project has
// used, and the plan those numbers are judged against.
//
// The plan is returned separately and may be nil, which means unlimited
// - no explicit assignment and no default plan. The handler is what
// turns the pair into a response body, so the wire type stays in the
// domain that serves it.
func Usage(ctx context.Context, st *store.Store, projID string) (Counts, *pmodel.Plan, error) {
	var out Counts
	p, err := planFor(ctx, st, projID)
	if err != nil {
		return out, nil, err
	}

	now := time.Now().UTC()
	hourly, err := st.Email.AcceptedSince(ctx, projID, now.Add(-time.Hour))
	if err != nil {
		return out, nil, err
	}

	daily, err := st.Email.AcceptedSince(ctx, projID, now.Add(-24*time.Hour))
	if err != nil {
		return out, nil, err
	}

	keys, err := st.APIKey.Count(ctx, projID)
	if err != nil {
		return out, nil, err
	}

	servers, err := st.SMTPServer.Count(ctx, projID)
	if err != nil {
		return out, nil, err
	}

	domains, err := st.Domain.Count(ctx, projID)
	if err != nil {
		return out, nil, err
	}

	subs, err := st.Subscriber.Count(ctx, projID)
	if err != nil {
		return out, nil, err
	}

	sandboxKept, err := st.Sandbox.Count(ctx, projID, store.SandboxFilter{})
	if err != nil {
		return out, nil, err
	}

	assetBytes, err := st.Template.AssetBytes(ctx, projID)
	if err != nil {
		return out, nil, err
	}

	out = Counts{
		SandboxMessages:    sandboxKept,
		EmailsLastHour:     hourly,
		EmailsLastDay:      daily,
		APIKeys:            keys,
		SMTPServers:        servers,
		Domains:            domains,
		Subscribers:        subs,
		TemplateAssetBytes: assetBytes,
	}

	return out, p, nil
}
