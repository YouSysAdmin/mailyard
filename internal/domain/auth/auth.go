// Package auth handles operator-console authentication: local
// (email + password) and OIDC SSO, plus the session cookie that
// gates the CRUD APIs.
package auth

// SessionCookie is the name of the cookie carrying the session JWT.
// The console reads nothing from it - it is HttpOnly - so this name
// only ever appears server side.
const (
	SessionCookie = "mailyard_session"
)
