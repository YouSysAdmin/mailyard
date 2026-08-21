// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package session models a tracked sign-in.
//
// The session JWT stays the credential - a session row does not hold
// a secret and cannot be used to authenticate. It exists so a token
// can be revoked before it expires, and so a user can see where they
// are signed in. The token carries the row id as its jti claim.
package session

import "time"

// Session is one sign-in.
type Session struct {
	// ID is the JWT jti claim.
	ID     string `json:"id"`
	UserID string `json:"user_id"`

	// UserAgent and IP record where the sign-in came from, for the
	// "is this me?" question the list is meant to answer.
	UserAgent string    `json:"user_agent,omitempty"`
	IP        string    `json:"ip,omitempty"`
	CreatedAt time.Time `json:"created_at"`

	// LastSeenAt is refreshed lazily, not on every request - see the
	// store comment.
	LastSeenAt time.Time `json:"last_seen_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	Revoked    bool      `json:"revoked"`

	// AuthProviderID names the identity provider that authenticated
	// this sign-in, empty for a local password login.
	//
	// It is a record, not a control. Nothing compares against it to
	// decide access - a project does not decide how anybody signed in.
	// What it is good for is the sessions page, which says whether a
	// sign-in came through an identity provider, a thing worth
	// seeing when reviewing where an account is signed in.
	AuthProviderID string `json:"auth_provider_id,omitempty"`

	// Current marks the session making the request, filled by the
	// handler so the UI can label it and avoid offering to revoke it
	// as if it were somebody else's.
	Current bool `json:"current,omitzero"`
}

// Active reports whether the session can still authenticate at now.
func (s *Session) Active(now time.Time) bool {
	return !s.Revoked && now.Before(s.ExpiresAt)
}
