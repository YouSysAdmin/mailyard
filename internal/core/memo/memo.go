// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package memo holds one computed value for a short while, so a burst of
// identical requests costs one computation instead of one each.
//
// It exists for the two open endpoints that touched the database on
// every call - the readiness probe and the login page's auth posture.
// Neither needs a credential, so a flood of either turned HTTP
// concurrency straight into connection-pool pressure, and the requests
// that did carry a credential queued behind it. A one-second memo makes
// the flood cost what one request costs.
package memo

import (
	"sync"
	"time"
)

// Value memoizes the result of a function for TTL. The zero value is
// unusable - construct with New.
type Value[T any] struct {
	ttl time.Duration
	now func() time.Time

	mu  sync.Mutex
	val T
	err error
	at  time.Time
}

// New builds a Value that keeps an answer for ttl.
func New[T any](ttl time.Duration) *Value[T] {
	return &Value[T]{ttl: ttl, now: time.Now}
}

// Get answers from the memo when it is fresh and calls compute
// otherwise. An ERROR IS MEMOIZED TOO: the readiness probe failing is
// exactly the moment a hundred probes arrive at once, and a hundred
// pings of an unreachable database is the stampede this is for.
//
// compute runs under the lock, so concurrent callers wait for one
// answer rather than each computing their own.
func (v *Value[T]) Get(compute func() (T, error)) (T, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	now := v.now()
	if !v.at.IsZero() && now.Sub(v.at) < v.ttl {
		return v.val, v.err
	}

	v.val, v.err = compute()
	v.at = now

	return v.val, v.err
}
