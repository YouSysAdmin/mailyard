// Package user is the persistence + handler surface for the
// operator-console user record: the SQL store (store.go) and the
// /api/users CRUD handlers (endpoint.go), which are mounted behind
// requireAuth + requireAdmin in server/routes.go.
// The model lives in internal/models/user - the UserStore interface
// in internal/domain/store.
package user
