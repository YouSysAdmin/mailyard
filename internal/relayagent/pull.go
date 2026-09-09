// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package relayagent

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

// Pull mode: the node fetches its mail instead of listening for it.
//
// A listener node is dialled by the delivery worker over mutual TLS. A
// node behind NAT, or one that may only egress through a proxy, cannot
// be dialled - so the platform assigns it messages and the node claims
// them over the same HTTPS control channel it heartbeats on, which is
// plain outbound and honours HTTPS_PROXY. What arrives is the finished
// message - signed, headers set - and goes into the same spool the
// listener would have put it in, so delivery, retries, reporting and
// the lifetime rule are one code path whichever way the bytes came.

// claimWait is how long one claim parks on the platform when nothing
// is assigned. The platform caps it (relay_nodes.claim_wait_max) and
// wakes the poll on an assignment, so this bounds latency only when a
// wake is lost.
const claimWait = 30 * time.Second

// claimRetry paces claims after a control error.
const claimRetry = 5 * time.Second

func (a *Agent) pullLoop(ctx context.Context) {
	for ctx.Err() == nil {
		holding, err := a.spool.EmailIDs()
		if err != nil {
			a.log.Error("relay node: could not list the spool", "err", err)
		}

		max := a.cfg.DeliveryConcurrency * 2
		if max < 1 {
			max = 8
		}

		msgs, err := a.ctrl.Claim(ctx, a.nodeID, a.token, max, claimWait, holding)
		if err != nil {
			if ctx.Err() != nil {
				return
			}

			if ce, ok := errors.AsType[*ControlError](err); ok && ce.Fatal() {
				a.log.Error("relay node: the control plane refuses this node's claims - it will deliver what it has and take nothing new",
					"err", err)

				return
			}

			a.log.Warn("relay node: claim failed, will retry", "err", err)
			select {
			case <-time.After(claimRetry):
			case <-ctx.Done():
			}

			continue
		}

		spooled := 0
		for _, m := range msgs {
			n, err := a.spoolClaimed(m)
			if err != nil {
				a.log.Error("relay node: could not spool a claimed message", "email_id", m.ID, "err", err)
				continue
			}

			spooled += n
		}

		if spooled > 0 {
			a.log.Info("relay node: claimed", "messages", len(msgs), "spooled", spooled)
			a.Deliverer.Wake()
		}
	}
}

// spoolClaimed puts one claimed message into the spool, one entry per
// recipient domain the way the listener does, and reports how many it
// wrote. An entry that is already there is left alone: a claim returns
// the same rows until they are reported, and overwriting one mid-flight
// would hand its accepted recipients back to it.
func (a *Agent) spoolClaimed(m ClaimedMessage) (int, error) {
	body, err := base64.StdEncoding.DecodeString(m.RawB64)
	if err != nil {
		return 0, err
	}

	byDomain := map[string][]string{}
	for _, rcpt := range m.Recipients {
		_, domain, ok := strings.CutLast(rcpt, "@")
		if !ok || domain == "" {
			continue
		}

		domain = strings.ToLower(domain)
		byDomain[domain] = append(byDomain[domain], rcpt)
	}

	now := time.Now().UTC()
	written := 0
	for domain, rcpts := range byDomain {
		id := claimedID(m.ID, domain)
		if held, err := a.spool.Has(id); err != nil {
			return written, err
		} else if held {
			continue
		}

		msg := &Message{
			ID:           id,
			EmailID:      m.ID,
			EnvelopeFrom: m.EnvelopeFrom,
			Domain:       domain,
			Recipients:   rcpts,
			AcceptedAt:   now,
			NextAttempt:  now,
		}
		if err := a.spool.Put(msg, body); err != nil {
			return written, err
		}

		written++
	}

	return written, nil
}

// claimedID is the spool id of one claimed message for one domain.
// Deterministic, so a second claim of the same message finds its
// entries instead of making new ones. The domain is hashed: the id is
// a file name, and a domain is not.
func claimedID(emailID, domain string) string {
	sum := sha256.Sum256([]byte(domain))

	return emailID + "-" + hex.EncodeToString(sum[:8])
}
