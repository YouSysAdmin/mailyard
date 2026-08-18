// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package sessioncache keeps the session revocation check off the
// database on the hot path.
//
// Without it every authenticated request costs a row read, putting a
// query behind the auth middleware on every call the console makes.
// With it, a session is verified once and trusted for a short window.
//
// The window is the honest cost: a session revoked on one node stays
// usable on other nodes for up to TTL. The node that served the
// revoke calls Invalidate and is immediately consistent, which covers
// the common single-node case exactly. TTL is deliberately short so
// the multi-node lag stays in "seconds", not "until the token
// expires" - which is where we were before sessions existed at all.
package sessioncache

import (
	"sync"
	"time"
)

// TTL is how long a positive verification is trusted.
const TTL = 15 * time.Second

type entry struct {
	userID    string
	expiresAt time.Time // session expiry, not cache expiry
	checkedAt time.Time
}

// Cache is a small map of verified sessions. The zero value is not
// usable - call New.
type Cache struct {
	mu      sync.RWMutex
	entries map[string]entry

	// lastSweep bounds how often eviction walks the map.
	lastSweep time.Time
}

// New builds an empty, usable Cache. The zero value is not - writing
// to a nil map panics, and Store writes on the first verified session.
func New() *Cache {
	return &Cache{entries: map[string]entry{}, lastSweep: time.Now()}
}

// Lookup reports whether the session was verified recently enough to
// trust without re-reading it. A false result means "ask the
// database", never "reject".
//
// It returns the user id and nothing else. A project does not pin an
// identity provider, so nothing needs the authenticating provider back
// from a cache hit, and an entry carrying a value no caller reads is a
// field the next reader has to work out is dead.
func (c *Cache) Lookup(id string, now time.Time) (userID string, ok bool) {
	if c == nil || id == "" {
		return "", false
	}

	c.mu.RLock()
	e, found := c.entries[id]
	c.mu.RUnlock()
	if !found {
		return "", false
	}

	if now.Sub(e.checkedAt) > TTL || !now.Before(e.expiresAt) {
		return "", false
	}

	return e.userID, true
}

// Store records a verified session.
func (c *Cache) Store(id, userID string, sessionExpiry, now time.Time) {
	if c == nil || id == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[id] = entry{
		userID:    userID,
		expiresAt: sessionExpiry,
		checkedAt: now,
	}
	if now.Sub(c.lastSweep) > time.Minute {
		c.evictLocked(now)
		c.lastSweep = now
	}
}

// Invalidate drops a session so the next request re-reads it. Called
// on revoke by the node that served it.
func (c *Cache) Invalidate(id string) {
	if c == nil || id == "" {
		return
	}

	c.mu.Lock()
	delete(c.entries, id)
	c.mu.Unlock()
}

// InvalidateAll clears the whole cache, for a change that affects
// many sessions at once.
func (c *Cache) InvalidateAll() {
	if c == nil {
		return
	}

	c.mu.Lock()
	c.entries = map[string]entry{}
	c.mu.Unlock()
}

func (c *Cache) evictLocked(now time.Time) {
	for id, e := range c.entries {
		if now.Sub(e.checkedAt) > TTL || !now.Before(e.expiresAt) {
			delete(c.entries, id)
		}
	}
}
