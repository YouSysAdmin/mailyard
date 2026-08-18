// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"context"

	"github.com/yousysadmin/mailyard/internal/core/smtpclient"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
)

// Route says which servers a message may go out through.
//
// ServerID pins one exactly and wins over everything else. GroupID
// names a pool. Both empty means the project's default group.
//
// Ids, not slugs. The slug is the caller-facing handle and is
// translated once, at accept time, where an unknown one can still be
// reported as a bad request - see Service.resolveRoute. By the time a
// message is queued the decision is a row id.
type Route struct {
	ServerID string
	GroupID  string
}

// ResolveCandidates returns every server that could carry a message
// from sender, in the order they should be tried.
//
// One function, because three callers ask the same question and their
// answers have to agree: Service.Validate at accept time (so a doomed
// send is refused with a useful error instead of sitting in the
// queue), the processor at delivery time (configuration may have
// changed in between), and failover walking the rest of the list.
// When the shared pool was first added only the processor knew about
// it, and Validate rejected every send it was supposed to enable.
//
// An empty result means nothing can carry this message.
func ResolveCandidates(ctx context.Context, st *store.Store, projID, sender string, route Route) ([]*ssmodel.Server, error) {
	if route.ServerID != "" {
		srv, err := st.SMTPServer.Get(ctx, projID, route.ServerID)
		if err != nil {
			return nil, err
		}

		// A pin is a pin: no falling back to the group or the pool. The
		// caller named this server, and quietly using a different one
		// would send from an address they did not choose.
		if srv == nil || srv.Status != ssmodel.StatusEnabled || !srv.AllowsSender(sender) {
			return nil, nil
		}

		return []*ssmodel.Server{srv}, nil
	}

	group, err := resolveGroup(ctx, st, projID, route.GroupID)
	if err != nil {
		return nil, err
	}

	var usable []*ssmodel.Server
	if group != nil {
		servers, err := st.SMTPServer.ListInGroup(ctx, projID, group.ID)
		if err != nil {
			return nil, err
		}

		for _, srv := range servers {
			if srv.Status == ssmodel.StatusEnabled && srv.AllowsSender(sender) {
				usable = append(usable, srv)
			}
		}
	}

	if len(usable) > 0 {
		return usable, nil
	}

	return resolveShared(ctx, st, projID, sender)
}

// ResolveServer is the single-answer form: the server a message would
// go out through right now, or nil when none can carry it.
func ResolveServer(ctx context.Context, st *store.Store, projID, sender string, route Route) (*ssmodel.Server, error) {
	candidates, err := ResolveCandidates(ctx, st, projID, sender, route)
	if err != nil || len(candidates) == 0 {
		return nil, err
	}

	return candidates[0], nil
}

// resolveGroup picks the group a send is routed to: the named one, or
// the project's default.
//
// A named group that has since been DELETED falls back to the default
// rather than failing. Deleting a group moves its servers into the
// default one, so that is where the message was always going to end
// up - refusing it would strand mail that was queued while the group
// still existed. An unknown group named by a caller is a different
// thing entirely and is rejected up front, at accept time.
//
// GetDefault rather than EnsureDefault: this runs on the delivery
// path, and a read should not be creating rows. Migration 00003
// backfilled every project and the write paths ensure it for projects
// created since, so nil here means a project with nothing configured,
// which resolveShared handles.
func resolveGroup(ctx context.Context, st *store.Store, projID, groupID string) (*ssmodel.Group, error) {
	if st.SMTPGroup == nil {
		return nil, nil
	}

	if groupID != "" {
		g, err := st.SMTPGroup.Get(ctx, projID, groupID)
		if err != nil || g != nil {
			return g, err
		}
	}

	return st.SMTPGroup.GetDefault(ctx, projID)
}

// resolveShared picks from the platform pool, and only for a project
// that owns no server rows at all.
//
// Owning none is the test, not owning none that WORKS. A project with
// a single disabled or misconfigured server gets a plain failure
// instead: quietly rerouting its mail through platform credentials
// would send from a different IP, under a different SPF record, than
// the operator configured, and they would find that out from a
// deliverability report weeks later rather than from an error now.
// Adding a server is the act of taking delivery over.
func resolveShared(ctx context.Context, st *store.Store, projID, sender string) ([]*ssmodel.Server, error) {
	if st.SharedSMTP == nil {
		return nil, nil
	}

	owned, err := st.SMTPServer.Count(ctx, projID)
	if err != nil {
		return nil, err
	}

	if owned > 0 {
		return nil, nil
	}

	pool, err := st.SharedSMTP.ListEnabled(ctx)
	if err != nil {
		return nil, err
	}

	var out []*ssmodel.Server
	for _, srv := range pool {
		// Reserved for the platform's own mail. A tenant must never be
		// routed through it, which is the whole point of the flag -
		// see systemmail.
		if srv.PlatformOnly {
			continue
		}

		if !srv.AllowsSender(sender) || !srv.AllowsDomain(sender) {
			continue
		}

		if srv.SecurityMode == ssmodel.SecurityStrict {
			ok, err := ownsVerifiedDomain(ctx, st, projID, sender)
			if err != nil {
				return nil, err
			}

			if !ok {
				continue
			}
		}

		// The embedded Server is what delivery needs. Its ProjectID is
		// empty, which is correct - this row belongs to the platform,
		// and callers use that emptiness to tell the two apart.
		out = append(out, &srv.Server)
	}

	return out, nil
}

// RequireVerifiedSender is the one answer to "may this project send
// as this address", and every surface that accepts or delivers mail
// asks it: Service.Validate at accept time, the processor at delivery
// time, and the campaign endpoint before it queues an audience.
//
// Three callers, one function, for the same reason ResolveCandidates
// has three: a rule about who may send as whom is worthless if two
// places disagree about it.
//
// Delivery re-asks rather than trusting the accept-time answer,
// because a scheduled send can sit in the queue for days and a domain
// can be unverified or deleted in between.
//
// The message is identical whether the domain is unknown or belongs
// to another project. Saying which would let any tenant enumerate the
// domains its neighbours have registered, and cross-project access is
// supposed to look like a missing resource.
func RequireVerifiedSender(ctx context.Context, st *store.Store, projID, sender string) error {
	host := senderDomain(sender)
	if host == "" {
		return reqErrf("sender %q has no domain", sender)
	}

	ok, err := ownsVerifiedDomain(ctx, st, projID, sender)
	if err != nil {
		return err
	}

	if !ok {
		return reqErrf(
			"domain %q is not verified by this project - add it under Domains and complete DNS verification before sending as %s",
			host, sender)
	}

	return nil
}

// ownsVerifiedDomain reports whether the SENDING project has proved
// ownership of the sender's domain.
//
// This is what strict mode buys. Domain names are globally unique
// here, so without the project comparison any tenant could relay as
// another tenant's domain through platform credentials - the same
// hole signerFor closes for DKIM, and it matters more on a shared
// server, where the platform's own reputation carries the message.
func ownsVerifiedDomain(ctx context.Context, st *store.Store, projID, sender string) (bool, error) {
	host := senderDomain(sender)
	if host == "" {
		return false, nil
	}

	// Covering, not exact: a verified apex owns its subdomains.
	d, err := st.Domain.GetVerifiedCovering(ctx, host)
	if err != nil {
		return false, err
	}

	return d != nil && d.ProjectID == projID, nil
}

// senderAddress is the envelope form of a From header, shared by the
// callers of ResolveCandidates so they normalize identically.
func senderAddress(from string) string { return smtpclient.EnvelopeAddress(from) }
