// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package domain owns the request-scoped types shared across domain
// handlers.
package domain

import (
	"log/slog"

	"github.com/gofiber/fiber/v3"

	akmodel "github.com/yousysadmin/mailyard/internal/models/apikey"
	"github.com/yousysadmin/mailyard/internal/models/permission"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
	usermodel "github.com/yousysadmin/mailyard/internal/models/user"
)

// ContextKey is the c.Locals slot the server middleware stores the
// per-request *RequestContext under.
const ContextKey = "AppReqContext"

// RequestContext provides per-request scoped values required for HTTP
// handlers: app identity for building absolute URLs, client/request
// metadata for logging, and the authenticated user once requireAuth
// has resolved it (nil before auth / on open endpoints / when auth is
// disabled).
//
// It embeds fiber.Ctx so handlers holding the RequestContext keep
// the full Fiber surface. The Path field shadows the embedded Path()
// method - use rc.Ctx.Path() if you need the live value.
type RequestContext struct {
	fiber.Ctx
	AppName    string          `json:"app_name"`
	AppVersion string          `json:"app_version"`
	ClientIP   string          `json:"client_ip"`
	User       *usermodel.User `json:"user,omitempty"`

	// SessionID is the jti of the session that authenticated this
	// request. Empty for API-key callers and for tokens minted before
	// session tracking existed.
	SessionID string `json:"-"`
	Path      string `json:"path"`
	RequestID string `json:"request_id"`
	SSL       bool   `json:"ssl"`

	// Project is resolved by requireProject on tenant-scoped routes
	// (nil elsewhere). Handlers behind that middleware read Project.ID
	// for store scoping and never trust a project id from the request
	// directly.
	Project *projmodel.Project `json:"project,omitempty"`

	// ProjectOwner is the one thing about this caller that is not a
	// permission: they own the project, so they may delete it and
	// rewrite its SSO policy - the acts the permission catalogue
	// cannot name.
	ProjectOwner bool `json:"project_owner,omitzero"`

	// Permissions is what this caller may do in Project, and it is
	// what every tenant gate consults.
	//
	// Resolved once from the membership read that already happens -
	// the member's own role, else the project's default, else nothing.
	// Kept as a set rather than recomputed at each gate so the answer
	// cannot differ between two checks inside one request, and so the
	// console can be handed the same set the server enforced.
	Permissions permission.Set `json:"-"`

	// APIKey is set by machineAuth on the project-key branch of a
	// /api/v1 request. nil when a session made the call.
	APIKey *akmodel.Key `json:"api_key,omitempty"`

	// AdminAPIKey is set on the platform-key branch. A caller holding
	// one is admin-tier without being a user, which is why requireAdmin
	// and the maintenance-mode exemption both have to ask for it
	// separately - User stays nil on this path.
	AdminAPIKey *akmodel.Admin `json:"admin_api_key,omitempty"`
}

// IsPlatformAdmin reports whether the caller may act on the
// installation: an admin-tier user, or a platform credential.
//
// One method because three places ask - requireAdmin, the
// maintenance-mode exemption, and project access - and a fourth that
// forgot the key branch would let an admin credential be refused by
// something it plainly should pass.
func (rc *RequestContext) IsPlatformAdmin() bool {
	if rc == nil {
		return false
	}

	return rc.AdminAPIKey != nil || (rc.User != nil && rc.User.IsAdmin())
}

// Actor names whoever made the request, for a record of who changed
// something: the user's id and address, or the credential's name when a
// key made the call. id is empty for a key, which has no account.
func (rc *RequestContext) Actor() (id, label string) {
	switch {
	case rc == nil:
		return "", ""
	case rc.AdminAPIKey != nil:
		return "", "admin api key " + rc.AdminAPIKey.Name
	case rc.APIKey != nil:
		return "", "api key " + rc.APIKey.Name
	case rc.User != nil:
		return rc.User.ID, rc.User.Email
	}

	return "", ""
}

// IsSandboxCredential reports that everything this caller sends is
// captured rather than delivered.
//
// A property of the CREDENTIAL, never of the request: swapping a key
// instead of editing code is the whole value, and a body field ends up
// left true on a production deploy. A browser session is never one -
// the sandbox is for an application under test, not for a person
// clicking Send in the console.
func (rc *RequestContext) IsSandboxCredential() bool {
	return rc != nil && rc.APIKey != nil && rc.APIKey.Sandbox
}

// LogValue makes RequestContext a slog.LogValuer so
// log.With("context", rc) (or slogfiber.AddCustomAttributes with
// slog.Any) emits one structured group. Evaluated lazily at log
// time, so a User resolved after the attribute was attached still
// shows up in the access log.
func (rc *RequestContext) LogValue() slog.Value {
	attrs := []slog.Attr{
		slog.String("app_name", rc.AppName),
		slog.String("app_version", rc.AppVersion),
		slog.String("client_ip", rc.ClientIP),
		slog.String("path", rc.Path),
		slog.String("request_id", rc.RequestID),
		slog.Bool("ssl", rc.SSL),
	}
	if rc.User != nil {
		attrs = append(attrs,
			slog.String("user_email", rc.User.Email),
			slog.Bool("admin", rc.User.IsAdmin()),
		)
	}

	if rc.Project != nil {
		attrs = append(attrs,
			slog.String("project_id", rc.Project.ID),
			slog.String("project_slug", rc.Project.Slug),
			slog.Bool("project_owner", rc.ProjectOwner),
		)
	}

	if rc.AdminAPIKey != nil {
		attrs = append(attrs, slog.String("admin_api_key_id", rc.AdminAPIKey.ID))
	}

	if rc.APIKey != nil {
		attrs = append(attrs, slog.String("api_key_id", rc.APIKey.ID))
	}

	return slog.GroupValue(attrs...)
}

// GetRequestContext returns the RequestContext the server middleware
// stored on this request, or nil when the middleware isn't installed
// (e.g. a bare unit-test app).
func GetRequestContext(c fiber.Ctx) *RequestContext {
	rc, _ := c.Locals(ContextKey).(*RequestContext)

	return rc
}
