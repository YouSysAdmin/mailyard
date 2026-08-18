// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package sestopics answers "is this SNS topic one of ours".
//
// In core rather than beside the SES receiver because two very
// different places need it: the receiver asks the question, and the
// SMTP server endpoints have to drop the cache when somebody edits a
// topic. Runtime can hold this - it cannot hold a domain package.
package sestopics

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"github.com/yousysadmin/mailyard/internal/database"
)

// Allowlist is the cached set of configured topics.
//
// It reads from the server rows rather than from config, because a
// topic belongs to the SES account of one server - and a tenant with
// their own SES has no way to edit an operator's config file.
//
// Cached, and that is not an optimization. This endpoint is public
// and unauthenticated: the topic check is the first thing a request
// reaches, so a database query per call would let anyone who found
// the URL generate database load at will. The cache turns that into
// one query a minute however hard it is hit.
type Allowlist struct {
	database.Base

	// TTL bounds staleness. Topics change when somebody edits a
	// server, which is rare - a minute is generous and still means a
	// newly configured topic works almost immediately.
	TTL time.Duration

	mu      sync.RWMutex
	topics  map[string]bool
	fetched time.Time

	// now is a test seam. Nil means time.Now.
	now func() time.Time
}

// NewAllowlist builds a Allowlist on db.
func NewAllowlist(db *sql.DB) *Allowlist {
	return &Allowlist{Base: database.NewBase(db), TTL: time.Minute}
}

func (a *Allowlist) clock() time.Time {
	if a.now != nil {
		return a.now()
	}

	return time.Now()
}

// topicSelect gathers every configured topic from both server tables.
//
// UNION, not UNION ALL: the same AWS account can publish several
// identities to one topic, so duplicates across rows are ordinary and
// the set is what matters.
const topicSelect = `
SELECT ses_topic_arn FROM smtp_servers WHERE ses_topic_arn <> ''
UNION
SELECT ses_topic_arn FROM shared_smtp_servers WHERE ses_topic_arn <> ''`

// Allowed reports whether arn is configured on any server.
//
// An empty arn is never allowed, and neither is anything when no
// server has a topic. Empty means accept nothing - the same rule the
// config list had, and the reason this endpoint can safely exist
// whether or not anybody uses SES.
func (a *Allowlist) Allowed(ctx context.Context, arn string) (bool, error) {
	if arn == "" {
		return false, nil
	}

	topics, err := a.load(ctx)
	if err != nil {
		return false, err
	}

	return topics[arn], nil
}

func (a *Allowlist) load(ctx context.Context) (map[string]bool, error) {
	a.mu.RLock()
	if a.topics != nil && a.clock().Sub(a.fetched) < a.ttl() {
		out := a.topics
		a.mu.RUnlock()

		return out, nil
	}

	a.mu.RUnlock()

	rows, err := a.Query(ctx, topicSelect)
	if err != nil {
		// Deliberately not falling back to the cached set. A read
		// failure is not evidence that a topic is ours, and answering
		// from a stale map here would be answering an authorization
		// question with a guess.
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	fresh := map[string]bool{}
	for rows.Next() {
		var arn string
		if err := rows.Scan(&arn); err != nil {
			return nil, err
		}

		fresh[arn] = true
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	a.mu.Lock()
	a.topics, a.fetched = fresh, a.clock()
	a.mu.Unlock()

	return fresh, nil
}

func (a *Allowlist) ttl() time.Duration {
	if a.TTL <= 0 {
		return time.Minute
	}

	return a.TTL
}

// Invalidate drops the cached set, so a server saved in the console
// takes effect at once rather than after the TTL.
func (a *Allowlist) Invalidate() {
	a.mu.Lock()
	a.topics = nil
	a.mu.Unlock()
}
