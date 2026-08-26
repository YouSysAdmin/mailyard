// Package store aggregates the per-domain persistence interfaces
// behind a single struct.
//
// Every method takes a context.Context first arg (rather than a
// fiber-bound RequestContext) so the interface stays usable from
// orchestrator goroutines and CLI commands as well as HTTP handlers.
package store

import (
	"context"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/keyset"

	"github.com/yousysadmin/mailyard/internal/models/analytics"
	"github.com/yousysadmin/mailyard/internal/models/apikey"
	"github.com/yousysadmin/mailyard/internal/models/audit"
	"github.com/yousysadmin/mailyard/internal/models/bounce"
	"github.com/yousysadmin/mailyard/internal/models/campaign"
	"github.com/yousysadmin/mailyard/internal/models/certificate"
	"github.com/yousysadmin/mailyard/internal/models/contact"
	"github.com/yousysadmin/mailyard/internal/models/domain"
	"github.com/yousysadmin/mailyard/internal/models/email"
	"github.com/yousysadmin/mailyard/internal/models/inbound"
	"github.com/yousysadmin/mailyard/internal/models/language"
	"github.com/yousysadmin/mailyard/internal/models/notification"
	"github.com/yousysadmin/mailyard/internal/models/oauthprovider"
	"github.com/yousysadmin/mailyard/internal/models/passkey"
	"github.com/yousysadmin/mailyard/internal/models/passwordreset"
	"github.com/yousysadmin/mailyard/internal/models/plan"
	"github.com/yousysadmin/mailyard/internal/models/project"
	"github.com/yousysadmin/mailyard/internal/models/relaynode"
	"github.com/yousysadmin/mailyard/internal/models/sandbox"
	"github.com/yousysadmin/mailyard/internal/models/sender"
	"github.com/yousysadmin/mailyard/internal/models/session"
	"github.com/yousysadmin/mailyard/internal/models/setting"
	"github.com/yousysadmin/mailyard/internal/models/signupverify"
	"github.com/yousysadmin/mailyard/internal/models/smtpcredential"
	"github.com/yousysadmin/mailyard/internal/models/smtpserver"
	"github.com/yousysadmin/mailyard/internal/models/stylesheet"
	"github.com/yousysadmin/mailyard/internal/models/subscriber"
	"github.com/yousysadmin/mailyard/internal/models/subscriberlist"
	"github.com/yousysadmin/mailyard/internal/models/suppression"
	"github.com/yousysadmin/mailyard/internal/models/template"
	"github.com/yousysadmin/mailyard/internal/models/unsubscribelist"
	"github.com/yousysadmin/mailyard/internal/models/user"
	"github.com/yousysadmin/mailyard/internal/models/webhook"
)

// Store bundles every per-domain store.
// Handlers depend on this struct, never on a concrete backend.
type Store struct {
	User            UserStore
	Project         ProjectStore
	SMTPServer      SMTPServerStore
	SharedSMTP      SharedSMTPStore
	Certificate     CertificateStore
	RelayNode       RelayNodeStore
	SMTPGroup       SMTPGroupStore
	Template        TemplateStore
	Stylesheet      StylesheetStore
	Language        LanguageStore
	Email           EmailStore
	APIKey          APIKeyStore
	AdminAPIKey     AdminAPIKeyStore
	Suppression     SuppressionStore
	Bounce          BounceStore
	AlertRecipients AlertRecipientStore
	Webhook         WebhookStore
	Subscriber      SubscriberStore
	SubscriberList  SubscriberListStore
	Campaign        CampaignStore
	Domain          DomainStore
	Inbound         InboundStore
	Sandbox         SandboxStore
	Plan            PlanStore
	Sender          SenderStore
	SMTPCredential  SMTPCredentialStore
	PasswordReset   PasswordResetStore
	SignupVerify    SignupVerifyStore
	Setting         SettingStore
	Audit           AuditStore
	Session         SessionStore
	Passkey         PasskeyStore
	Contact         ContactStore
	UnsubscribeList UnsubscribeListStore
	Analytics       AnalyticsStore
	OAuthProvider   OAuthProviderStore
	OAuthIdentity   OAuthIdentityStore
	Notification    NotificationStore
}

// NotificationStore persists in-app notifications. Addressed to a
// project, not a user, so read state is shared - see the model.
type NotificationStore interface {
	Get(ctx context.Context, projID, id string) (*notification.Notification, error)
	List(ctx context.Context, projID string, unreadOnly bool, limit, offset int) ([]*notification.Notification, error)
	CountUnread(ctx context.Context, projID string) (int, error)

	// Create reports false when a row with the same dedupe key
	// already exists, so a caller can tell a new condition from a
	// repeat and stay quiet about the repeat.
	Create(ctx context.Context, n *notification.Notification) (bool, error)
	MarkRead(ctx context.Context, projID, id string, at time.Time) error
	MarkAllRead(ctx context.Context, projID string, at time.Time) (int64, error)
	Delete(ctx context.Context, projID, id string) error
	PurgeOlderThan(ctx context.Context, before time.Time) (int64, error)
}

// OAuthProviderStore persists runtime-configured identity providers.
//
// Deliberately not project scoped, unlike every tenant store here:
// a provider decides who may sign in to the platform at all, which is
// not a tenant's decision to make. The gate is requireAdmin at route
// registration.
type OAuthProviderStore interface {
	Get(ctx context.Context, id string) (*oauthprovider.Provider, error)
	GetBySlug(ctx context.Context, slug string) (*oauthprovider.Provider, error)
	List(ctx context.Context) ([]*oauthprovider.Provider, error)
	ListLoginable(ctx context.Context) ([]*oauthprovider.Provider, error)
	Put(ctx context.Context, p *oauthprovider.Provider) error
	Delete(ctx context.Context, id string) error
	SlugTaken(ctx context.Context, slug, exceptID string) (bool, error)
}

// OAuthIdentityStore links external subjects to local users. Keyed on
// (provider, subject) because a subject is only unique within its
// issuer.
type OAuthIdentityStore interface {
	GetBySubject(ctx context.Context, providerID, subject string) (*oauthprovider.Identity, error)
	ListForUser(ctx context.Context, userID string) ([]*oauthprovider.Identity, error)
	Link(ctx context.Context, id *oauthprovider.Identity) error
	Unlink(ctx context.Context, userID, providerID string) error
}

// AnalyticsStore answers cross-table reporting questions. Read-only:
// nothing in it writes, and every method is project scoped.
type AnalyticsStore interface {
	Summary(ctx context.Context, projID string) (*analytics.Summary, error)

	// RecomputeDaily rebuilds the trend rollup for every project over a
	// window. Recomputed rather than incremented - see the method.
	RecomputeDaily(ctx context.Context, days int) error
	DailyCounts(ctx context.Context, projID string, from, to time.Time, status string) ([]analytics.DayCount, error)
	StatusBreakdown(ctx context.Context, projID string, from, to time.Time) (map[string]int, error)
}

// UnsubscribeListStore persists transactional opt-out scopes. GetAny
// serves the public unsubscribe page, where the signed token rather
// than a session is the authority.
type UnsubscribeListStore interface {
	Get(ctx context.Context, projID, id string) (*unsubscribelist.List, error)
	GetAny(ctx context.Context, id string) (*unsubscribelist.List, error)
	GetByName(ctx context.Context, projID, name string) (*unsubscribelist.List, error)
	List(ctx context.Context, projID string) ([]*unsubscribelist.List, error)
	Put(ctx context.Context, l *unsubscribelist.List) error
	Delete(ctx context.Context, projID, id string) error
}

// ContactStore persists auto-tracked recipient addresses. There is no
// Put: contacts are only ever written through RecordOutcome, which
// folds one terminal delivery result into the tallies atomically.
type ContactStore interface {
	Get(ctx context.Context, projID, id string) (*contact.Contact, error)
	GetByEmail(ctx context.Context, projID, email string) (*contact.Contact, error)
	List(ctx context.Context, projID, search string, limit, offset int) ([]*contact.Contact, error)
	Count(ctx context.Context, projID, search string) (int, error)
	RecordOutcome(ctx context.Context, projID, email, name string, sent bool, at time.Time) error
	PurgeForEmail(ctx context.Context, projID, email string) (int64, error)
	PurgeAll(ctx context.Context, projID string) (int64, error)
}

// SessionStore persists tracked sign-ins. Get is the auth-path
// lookup keyed by the token jti and is intentionally not user scoped - the
// token names the session. Every mutating method IS user
// scoped so one caller cannot revoke another's session.
type SessionStore interface {
	Get(ctx context.Context, id string) (*session.Session, error)
	ListForUser(ctx context.Context, userID string, now time.Time) ([]*session.Session, error)
	Put(ctx context.Context, m *session.Session) error
	Revoke(ctx context.Context, userID, id string) (bool, error)
	RevokeOthers(ctx context.Context, userID, keepID string) (int64, error)
	RevokeAllForUser(ctx context.Context, userID string) (int64, error)
	Touch(ctx context.Context, id string, t time.Time) error
	PurgeExpired(ctx context.Context, before time.Time) (int64, error)
}

// PasskeyStore persists enrolled WebAuthn credentials.
// GetByCredential is the sign-in lookup and is deliberately not user
// scoped: in a discoverable login the credential id is the identity
// claim, and the signature over it is what makes the claim true.
// Everything else is scoped by user.
type PasskeyStore interface {
	ListForUser(ctx context.Context, userID string) ([]*passkey.Passkey, error)
	GetByCredential(ctx context.Context, credentialID string) (*passkey.Passkey, error)
	Put(ctx context.Context, m *passkey.Passkey) error
	RecordUse(ctx context.Context, id, credential string, at time.Time) error
	Rename(ctx context.Context, userID, id, name string) (bool, error)
	Delete(ctx context.Context, userID, id string) (bool, error)
	DeleteAllForUser(ctx context.Context, userID string) (int, error)
	CountForUser(ctx context.Context, userID string) (int, error)
}

// AuditStore persists the operational and security trails. Reads are
// split by category: project rows are scoped by projID, security rows
// by actor.
type AuditStore interface {
	Put(ctx context.Context, e *audit.Event) error
	ListProject(ctx context.Context, projID string, limit, offset int) ([]*audit.Event, error)
	GetProject(ctx context.Context, projID, id string) (*audit.Event, error)
	ListForActor(ctx context.Context, actorID string, limit, offset int) ([]*audit.Event, error)
	ListSecurity(ctx context.Context, limit, offset int) ([]*audit.Event, error)
	ExportProject(ctx context.Context, projID string, from, to time.Time, limit int) ([]*audit.Event, error)
	ExportForActor(ctx context.Context, actorID string, from, to time.Time, limit int) ([]*audit.Event, error)
	ExportSecurity(ctx context.Context, from, to time.Time, limit int) ([]*audit.Event, error)
	PurgeOlderThan(ctx context.Context, before time.Time) (int64, error)
}

// SettingStore persists platform-wide setting overrides. Not
// project scoped - these are properties of the installation.
type SettingStore interface {
	All(ctx context.Context) ([]*setting.Setting, error)
	Put(ctx context.Context, m *setting.Setting) error
	Delete(ctx context.Context, key string) error
}

// PasswordResetStore persists single-use reset tokens. GetByHash is
// the redemption lookup and intentionally not user scoped - the
// token is the claim of identity.
type PasswordResetStore interface {
	GetByHash(ctx context.Context, hash string) (*passwordreset.Token, error)
	Put(ctx context.Context, t *passwordreset.Token) error

	// MarkUsed reports whether this call is the one that burned the
	// token, so a caller can refuse a losing race instead of
	// proceeding on a token somebody else already spent.
	MarkUsed(ctx context.Context, id string, at time.Time) (bool, error)
	InvalidateForUser(ctx context.Context, userID string, at time.Time) error
	CountRecentForUser(ctx context.Context, userID string, since time.Time) (int, error)
	DeleteExpired(ctx context.Context, before time.Time) (int64, error)
}

// SignupVerifyStore persists single-use signup verification tokens.
// The same contract as PasswordResetStore, over its own table.
type SignupVerifyStore interface {
	GetByHash(ctx context.Context, hash string) (*signupverify.Token, error)
	Put(ctx context.Context, t *signupverify.Token) error

	// MarkUsed reports whether this call is the one that burned the
	// token - see PasswordResetStore.MarkUsed.
	MarkUsed(ctx context.Context, id string, at time.Time) (bool, error)
	InvalidateForUser(ctx context.Context, userID string, at time.Time) error
	CountRecentForUser(ctx context.Context, userID string, since time.Time) (int, error)
	DeleteExpired(ctx context.Context, before time.Time) (int64, error)
}

// SMTPCredentialStore persists SMTP relay logins. GetByUsername is
// the relay auth lookup and intentionally not project scoped - the
// credential decides the project.
type SMTPCredentialStore interface {
	Get(ctx context.Context, projID, id string) (*smtpcredential.Credential, error)
	GetByUsername(ctx context.Context, username string) (*smtpcredential.Credential, error)
	List(ctx context.Context, projID string) ([]*smtpcredential.Credential, error)
	Put(ctx context.Context, c *smtpcredential.Credential) error
	Revoke(ctx context.Context, projID, id string) error
	Delete(ctx context.Context, projID, id string) error
	TouchLastUsed(ctx context.Context, id string, t time.Time) error
	Count(ctx context.Context, projID string) (int, error)
}

// SenderStore persists approved sender addresses.
type SenderStore interface {
	Get(ctx context.Context, projID, id string) (*sender.Sender, error)
	GetByEmail(ctx context.Context, projID, email string) (*sender.Sender, error)
	List(ctx context.Context, projID string) ([]*sender.Sender, error)
	Put(ctx context.Context, m *sender.Sender) error
	Delete(ctx context.Context, projID, id string) error
}

// PlanStore persists platform-wide usage plans (not tenant scoped).
type PlanStore interface {
	Get(ctx context.Context, id string) (*plan.Plan, error)
	GetDefault(ctx context.Context) (*plan.Plan, error)
	List(ctx context.Context) ([]*plan.Plan, error)
	Put(ctx context.Context, p *plan.Plan) error
	Delete(ctx context.Context, id string) error
}

// UserStore persists accounts. Not project scoped - an account belongs
// to the installation, and who may reach which project is
// ProjectStore's business.
//
// Get takes an EMAIL where every other store here keys on an id.
// GetByID is the id-keyed read.
type UserStore interface {
	Get(ctx context.Context, email string) (*user.User, error)
	GetByID(ctx context.Context, id string) (*user.User, error)
	Put(ctx context.Context, u *user.User) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context) ([]*user.User, error)
	Count(ctx context.Context) (int, error)
	TouchLastLogin(ctx context.Context, email string) error

	// ClaimTOTPStep burns a TOTP time-step for a user, reporting false
	// when that step (or a later one) was already used. Deliberately
	// not a field on the user model: Put would then race with it and
	// a stale in-memory value could hand a spent code back its
	// validity.
	ClaimTOTPStep(ctx context.Context, userID string, step uint64) (bool, error)

	// TOTPLockedUntil answers when a locked second factor reopens, or
	// nil when it is not locked. RecordTOTPFailure counts one wrong
	// code and reports whether that one locked the factor for lock -
	// the count is kept in the row for the same reason ClaimTOTPStep
	// is, so two nodes see one count. ClearTOTPFailures is the reset
	// on a right code.
	TOTPLockedUntil(ctx context.Context, userID string) (*time.Time, error)
	RecordTOTPFailure(ctx context.Context, userID string, limit int, lock time.Duration) (bool, error)
	ClearTOTPFailures(ctx context.Context, userID string) error

	// MarkEmailVerified flips the signup verification flag in place,
	// so the confirm handler cannot lose a concurrent Put.
	MarkEmailVerified(ctx context.Context, userID string) error

	// SetPassword and SetTOTP write their own columns and nothing else.
	// Put writes the whole row, so a handler that read the user, changed
	// one field and called Put undid whatever had changed in between -
	// an account disabled during its own password reset came back
	// enabled. Use Put only where writing the entire row IS the
	// intention.
	SetPassword(ctx context.Context, userID, hash string) error
	SetTOTP(ctx context.Context, userID, secret string, enabled bool) error
}

// ProjectStore persists tenancy: projects, memberships, and
// invitations. Get-style methods return (nil, nil) for missing rows.
type ProjectStore interface {
	Get(ctx context.Context, id string) (*project.Project, error)
	GetBySlug(ctx context.Context, slug string) (*project.Project, error)
	Put(ctx context.Context, w *project.Project) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context) ([]*project.Project, error)
	ListForUser(ctx context.Context, userID string) ([]*project.Project, error)
	CreateWithOwner(ctx context.Context, w *project.Project, ownerID string) error
	EnsureDefault(ctx context.Context) (*project.Project, error)

	// GetMember resolves the membership AND the permissions it carries,
	// applying the project's default role when the row names none. One
	// round trip, because this runs on every project-scoped request.
	GetMember(ctx context.Context, projID, userID string) (*project.Member, error)

	// PutMember ensures the row exists and writes neither role_id nor
	// owner on conflict - each has exactly one writer below, so
	// re-adding a member, accepting an invitation or an OIDC sign-in
	// cannot silently reassign or demote anybody.
	PutMember(ctx context.Context, m *project.Member) error

	// SetMemberRole is the only writer of project_members.role_id.
	// false means no such member, or no such role in this project
	// (deliberately the same answer).
	SetMemberRole(ctx context.Context, projID, userID, roleID string) (bool, error)

	// SetMemberOwner is the only writer of project_members.owner, and
	// refuses to demote the last owner. false is either that or no
	// such member.
	SetMemberOwner(ctx context.Context, projID, userID string, owner bool) (bool, error)

	// RemoveMember refuses to remove the last owner, same as demoting
	// one. false is either that or no such member.
	RemoveMember(ctx context.Context, projID, userID string) (bool, error)
	ListMembers(ctx context.Context, projID string) ([]*project.Member, error)
	MembershipsForUser(ctx context.Context, userID string) ([]*project.Member, error)

	// Project roles: the permission lists a project writes for itself.
	// DeleteRole refuses while members carry the role or the project
	// names it as its default, and reports which.
	GetRole(ctx context.Context, projID, id string) (*project.Role, error)
	ListRoles(ctx context.Context, projID string) ([]*project.Role, error)
	PutRole(ctx context.Context, role *project.Role) error
	DeleteRole(ctx context.Context, projID, id string) (bool, int, bool, error)

	// SetDefaultRole is the only writer of projects.default_role_id.
	// It refuses a role from another project, which is what stops a
	// project pointing at something it cannot resolve.
	SetDefaultRole(ctx context.Context, projID, roleID string) (bool, error)

	PutInvitation(ctx context.Context, inv *project.Invitation) error
	GetInvitationByToken(ctx context.Context, token string) (*project.Invitation, error)
	ListInvitations(ctx context.Context, projID string) ([]*project.Invitation, error)
	DeleteInvitation(ctx context.Context, projID, id string) error
}

// SMTPServerStore persists per-project SMTP servers. Reads return
// the password decrypted, Put encrypts it. Every method is scoped by
// projID - a foreign project's server is (nil, nil).
type SMTPServerStore interface {
	Get(ctx context.Context, projID, id string) (*smtpserver.Server, error)

	// GetAny is the one unscoped read here, for a caller that
	// legitimately has a server id and no project - see the store.
	GetAny(ctx context.Context, id string) (*smtpserver.Server, error)
	List(ctx context.Context, projID string) ([]*smtpserver.Server, error)
	Put(ctx context.Context, s *smtpserver.Server) error
	Delete(ctx context.Context, projID, id string) error
	SetStatus(ctx context.Context, projID, id, status, validationErr string, validatedAt *time.Time) error
	PickEnabled(ctx context.Context, projID, senderEmail string) (*smtpserver.Server, error)

	// ListInGroup returns one group's servers in pick order. The
	// delivery path walks this list, so the order must be total.
	ListInGroup(ctx context.Context, projID, groupID string) ([]*smtpserver.Server, error)
	Count(ctx context.Context, projID string) (int, error)
}

// SMTPGroupStore persists the named server pools a send can be routed
// to, and which one a project uses by default.
type SMTPGroupStore interface {
	Get(ctx context.Context, projID, id string) (*smtpserver.Group, error)
	GetBySlug(ctx context.Context, projID, slug string) (*smtpserver.Group, error)
	GetDefault(ctx context.Context, projID string) (*smtpserver.Group, error)

	// EnsureDefault is the write-path form: it creates the default
	// group for a project that has none rather than returning nil.
	EnsureDefault(ctx context.Context, projID string) (*smtpserver.Group, error)
	List(ctx context.Context, projID string) ([]*smtpserver.Group, error)

	// Put writes the group's own fields. It does not move is_default -
	// SetDefault is the only writer of that column, so that "exactly one
	// default" cannot be broken by an upsert carrying a stale flag.
	Put(ctx context.Context, g *smtpserver.Group) error

	// SetDefault promotes one group and demotes the rest in one
	// transaction, reporting whether the group was there. False means
	// nothing changed at all, including the old default.
	SetDefault(ctx context.Context, projID, id string) (bool, error)

	// Delete moves the group's servers to defaultGroupID and removes
	// the group, in one transaction. Refuses the default group.
	Delete(ctx context.Context, projID, id, defaultGroupID string) error
	SlugTaken(ctx context.Context, projID, slug, exceptID string) (bool, error)
}

// SharedSMTPStore persists the platform-owned SMTP pool: the fallback
// for projects that own no server at all.
//
// Note the absence of projID. These rows belong to the platform, not
// to a tenant, which is why they live in their own table - see
// smtpserver.Shared. Nothing on a project-scoped route may return one
// of these: a project gets DELIVERY through the pool, never sight of
// it.
type SharedSMTPStore interface {
	Get(ctx context.Context, id string) (*smtpserver.Shared, error)
	List(ctx context.Context) ([]*smtpserver.Shared, error)

	// ListEnabled is the delivery-side read, already filtered to
	// servers eligible to send and ordered by priority.
	ListEnabled(ctx context.Context) ([]*smtpserver.Shared, error)
	Put(ctx context.Context, s *smtpserver.Shared) error
	Delete(ctx context.Context, id string) error
	SetStatus(ctx context.Context, id, status, validationErr string, validatedAt *time.Time) error
	Count(ctx context.Context) (int, error)
}

// CertificateStore persists certificates and their sealed private
// halves. Get decrypts, every listing path deliberately does not -
// see the package doc in domain/certificate.
type CertificateStore interface {
	Get(ctx context.Context, scope, name string) (*certificate.Certificate, error)

	// GetPublic is Get without the private half, for a caller that
	// only needs what is already public - publishing an authority so
	// it can be installed, for one. Decrypting a key to answer that
	// would be holding one in memory for nothing.
	GetPublic(ctx context.Context, scope, name string) (*certificate.Certificate, error)
	ListScope(ctx context.Context, scope string) ([]*certificate.Certificate, error)
	Put(ctx context.Context, c *certificate.Certificate) error
	Delete(ctx context.Context, scope, name string) error

	// PutIfAbsent writes only when the key is free and says whether it
	// won. Two nodes generating one shared pair converge through it.
	PutIfAbsent(ctx context.Context, c *certificate.Certificate) (bool, error)
	ExpiringBefore(ctx context.Context, t time.Time) ([]*certificate.Certificate, error)
}

// RelayNodeStore persists the identity and liveness of enrolled relay
// nodes. Separate from the delivery tables because a node attaches to
// either of them - see domain/relaynode.
type RelayNodeStore interface {
	Get(ctx context.Context, id string) (*relaynode.Node, error)
	GetByServer(ctx context.Context, serverID string) (*relaynode.Node, error)

	// List is scoped by project. The empty string is the platform's
	// own nodes, which is a real scope here and not a wildcard.
	List(ctx context.Context, projID string) ([]*relaynode.Node, error)
	ListAll(ctx context.Context) ([]*relaynode.Node, error)
	Put(ctx context.Context, n *relaynode.Node) error
	Heartbeat(ctx context.Context, id, publicIP string, at time.Time, b relaynode.Beat) error

	// TouchInbound records an accepted forward. Separate from
	// Heartbeat because this one is OBSERVED - the node does not get
	// to say when it last succeeded.
	TouchInbound(ctx context.Context, id string, at time.Time) error
	Delete(ctx context.Context, id string) error
}

// TemplateStore persists templates, their versions, and per-language
// localizations. Version and localization access joins through the
// templates table so projID scopes the whole chain.
type TemplateStore interface {
	Get(ctx context.Context, projID, id string) (*template.Template, error)
	GetByName(ctx context.Context, projID, name string) (*template.Template, error)
	List(ctx context.Context, projID string) ([]*template.Template, error)
	Put(ctx context.Context, t *template.Template) error
	Delete(ctx context.Context, projID, id string) error
	SetActiveVersion(ctx context.Context, projID, id, versionID string) error

	GetVersion(ctx context.Context, projID, templateID, versionID string) (*template.Version, error)
	ListVersions(ctx context.Context, projID, templateID string) ([]*template.Version, error)
	PutVersion(ctx context.Context, projID string, v *template.Version) error
	DeleteVersion(ctx context.Context, projID, templateID, versionID string) error

	GetLocalization(ctx context.Context, projID, versionID, language string) (*template.Localization, error)
	GetLocalizationByID(ctx context.Context, projID, id string) (*template.Localization, error)
	ListLocalizations(ctx context.Context, projID, versionID string) ([]*template.Localization, error)
	PutLocalization(ctx context.Context, projID string, l *template.Localization) error
	DeleteLocalization(ctx context.Context, projID, id string) error

	PutAttachment(ctx context.Context, a *template.Attachment) error
	ListAttachments(ctx context.Context, projID, templateID string) ([]*template.Attachment, error)

	// Offloaded attachment keys, read before the rows cascade away with their template or their project.
	StorageKeysForProject(ctx context.Context, projID string) ([]string, error)
	StorageKeysForTemplate(ctx context.Context, projID, templateID string) ([]string, error)
	GetAttachment(ctx context.Context, projID, templateID, id string) (*template.Attachment, error)
	DeleteAttachment(ctx context.Context, projID, templateID, id string) error
}

// StylesheetStore persists reusable CSS blocks.
type StylesheetStore interface {
	Get(ctx context.Context, projID, id string) (*stylesheet.Stylesheet, error)
	List(ctx context.Context, projID string) ([]*stylesheet.Stylesheet, error)
	Put(ctx context.Context, s *stylesheet.Stylesheet) error
	Delete(ctx context.Context, projID, id string) error
}

// EmailFilter narrows EmailStore.List. Zero values mean "no constraint".
//
// Before and BeforeID are one cursor, `(created_at, id)`. created_at
// alone loses rows: two messages can share a timestamp, and when the tie
// straddles a page boundary the next page asks for `created_at < value`
// and skips every row sharing it - they appear on neither page.
//
// BeforeID may be empty, which keeps the older `?before=` contract
// working rather than turning it into an error.
type EmailFilter struct {
	Status   string
	Before   *time.Time
	BeforeID string
	Limit    int

	// Search matches a RECIPIENT address or the SUBJECT, and nothing
	// else. Not the body: the log is how somebody answers "did this
	// person get their receipt", and a body scan is a different
	// feature with a different cost.
	Search string
}

// EmailStore persists the email log, which doubles as the delivery
// queue. The project-scoped half serves handlers, the unscoped
// queue half (ClaimDue / Requeue / Finalize / RecoverStuck) is
// consumed by core/queue as its Source.
type EmailStore interface {
	Get(ctx context.Context, projID, id string) (*email.Email, error)
	GetAny(ctx context.Context, id string) (*email.Email, error)
	List(ctx context.Context, projID string, f EmailFilter) ([]*email.Email, error)
	Put(ctx context.Context, e *email.Email) error
	Reset(ctx context.Context, projID, id string) (bool, error)

	// Open and click tallies. Not project scoped: the tracking endpoints
	// are authorized by the signature in the URL, which names the email
	// and nothing else. MarkOpened reports whether this was the first
	// open, the number people quote.
	//
	// Both return the running count too, so the caller can stop writing
	// tracking_events rows for a signed URL being replayed - the pixel is
	// unauthenticated and lives in the recipient's mailbox forever.
	//
	// createdAt is in the predicate because `emails` is range partitioned
	// by it; without it Postgres opens an Update node on every live
	// partition. The tracking handler has already read the row.
	MarkOpened(ctx context.Context, id string, createdAt, at time.Time) (first bool, opens int64, err error)
	MarkClicked(ctx context.Context, id string, createdAt, at time.Time) (clicks int64, err error)
	CountByStatus(ctx context.Context, projID string) (map[string]int, error)
	CountCreatedSince(ctx context.Context, projID string, since time.Time) (int, error)

	// AcceptedSince reads the per-minute volume counter, which is what
	// the plan limits ask - see the email store for why it is not a
	// COUNT over the emails table any more.
	AcceptedSince(ctx context.Context, projID string, since time.Time) (int, error)
	PruneVolumeBefore(ctx context.Context, before time.Time) (int64, error)
	CountAllByStatus(ctx context.Context) (map[string]int, error)

	ClaimDue(ctx context.Context, now time.Time, limit int) ([]*email.Email, error)
	Requeue(ctx context.Context, id string, createdAt time.Time, next time.Time, errMsg string) error

	// createdAt prunes to one partition of the weekly-partitioned
	// emails table - see the store method.
	Finalize(ctx context.Context, id string, createdAt time.Time, status, errMsg, deliveredVia string, sentAt *time.Time) error
	RecoverStuck(ctx context.Context, olderThan time.Time) (int, error)

	// Retention sweep, unscoped by project.
	StorageKeysOlderThan(ctx context.Context, before time.Time) ([]string, error)
	PurgeOlderThan(ctx context.Context, before time.Time) (int64, error)
	ClearBodiesOlderThan(ctx context.Context, before time.Time) (int64, error)
	ClearAttachmentsOlderThan(ctx context.Context, before time.Time) (int64, error)

	// Erasure, project scoped. Each purge has a key query beside it
	// whose WHERE clause matches it exactly, because a blob is named
	// only by the row about to be deleted - drop the row first and the
	// object is unreachable for good.
	StorageKeysForProject(ctx context.Context, projID string) ([]string, error)
	StorageKeysForProjectOlderThan(ctx context.Context, projID string, before time.Time) ([]string, error)
	StorageKeysForAddress(ctx context.Context, projID, email string) ([]string, error)
	PurgeProjectOlderThan(ctx context.Context, projID string, before time.Time) (int64, error)
	PurgeForAddress(ctx context.Context, projID, email string) (int64, error)
}

// SuppressionFilter narrows the do-not-send list.
//
// Cursor rather than offset: this table grows per message - one row per
// permanent rejection, kept indefinitely - and OFFSET makes page N cost
// N. See internal/core/keyset.
//
// Search is the field that matters. Paging walks a million rows in
// twenty thousand clicks; search answers the question people arrive
// with, which is whether one address is blocked.
type SuppressionFilter struct {
	Kind string

	// Search matches the start of an address.
	Search string
	Limit  int
	Cursor keyset.Cursor
}

// SuppressionStore persists the do-not-send list. FilterSuppressed is
// the send pipeline's pre-flight split of recipients.
type SuppressionStore interface {
	List(ctx context.Context, projID string, f SuppressionFilter) ([]*suppression.Suppression, error)
	Upsert(ctx context.Context, s *suppression.Suppression) error

	// Delete lifts the global block, DeleteForList lifts one list
	// opt-out, and PurgeForAddress takes every scope for the erasure
	// path. Three questions, three statements - see the comments on
	// each.
	Delete(ctx context.Context, projID, email string) (bool, error)
	PurgeForAddress(ctx context.Context, projID, email string) (int64, error)
	IsSuppressed(ctx context.Context, projID, email string) (bool, error)
	IsSuppressedForList(ctx context.Context, projID, email, listID string) (bool, error)
	FilterSuppressed(ctx context.Context, projID string, emails []string) (allowed, blocked []string, err error)
	FilterSuppressedForList(ctx context.Context, projID, listID string, emails []string) (allowed, blocked []string, err error)
	CountForList(ctx context.Context, projID, listID string) (int, error)
	DeleteForList(ctx context.Context, projID, email, listID string) (bool, error)
}

// BounceFilter narrows the bounce log. Same keyset reasoning as
// SuppressionFilter: a few percent of a large send is still a lot of
// rows a day.
type BounceFilter struct {
	// Type is hard, soft or complaint.
	Type string

	// Search matches the start of a recipient address.
	Search string
	Limit  int
	Cursor keyset.Cursor
}

// AlertRecipientStore answers who hears about an alert. Read-only, and
// in the project domain because two of the three questions are about who
// is accountable for a project - which is what membership and ownership
// already decide.
type AlertRecipientStore interface {
	ProjectAlert(ctx context.Context, projectID string) ([]string, error)
	PlatformAdmins(ctx context.Context) ([]string, error)
	UserEmail(ctx context.Context, userID string) (string, error)
}

// BounceStore persists delivery failure records.
type BounceStore interface {
	Put(ctx context.Context, b *bounce.Bounce) error
	List(ctx context.Context, projID string, f BounceFilter) ([]*bounce.Bounce, error)
	HasHardBounce(ctx context.Context, projID, email string) (bool, error)

	// DeleteByEmail removes every report for one address and returns how
	// many went. By ADDRESS rather than by row id: one mailbox that did
	// not exist yet has a report per attempt, and deleting them one at a
	// time leaves HasHardBounce answering yes until the last.
	DeleteByEmail(ctx context.Context, projID, email string) (int, error)
}

// SubscriberStore persists the marketing audience. ListPage is the
// stable iterator dynamic segments evaluate over.
type SubscriberStore interface {
	Get(ctx context.Context, projID, id string) (*subscriber.Subscriber, error)
	GetByEmail(ctx context.Context, projID, email string) (*subscriber.Subscriber, error)
	List(ctx context.Context, projID, status, query string, limit, offset int) ([]*subscriber.Subscriber, error)
	ListPage(ctx context.Context, projID string, limit, offset int) ([]*subscriber.Subscriber, error)

	// Count is every subscriber in the project, which is what the plan
	// quota measures. CountMatching is the total behind one list page.
	// The two are not interchangeable: reporting the first beside a
	// filtered page is what made a search matching one row out of five
	// thousand claim fifty pages, forty-nine of them empty.
	Count(ctx context.Context, projID string) (int, error)
	CountMatching(ctx context.Context, projID, status, query string) (int, error)
	Put(ctx context.Context, s *subscriber.Subscriber) error
	Delete(ctx context.Context, projID, id string) error
	SetStatusByEmail(ctx context.Context, projID, email, status string) (bool, error)
}

// SubscriberListStore persists campaign audiences: static membership,
// dynamic rules, and per-list opt-outs. ResolveRecipients returns the
// send-ready audience (subscribed, not opted out).
type SubscriberListStore interface {
	Get(ctx context.Context, projID, id string) (*subscriberlist.List, error)
	List(ctx context.Context, projID string) ([]*subscriberlist.List, error)
	Put(ctx context.Context, l *subscriberlist.List) error
	Delete(ctx context.Context, projID, id string) error

	AddMember(ctx context.Context, projID, listID, subscriberID string) error
	RemoveMember(ctx context.Context, projID, listID, subscriberID string) error
	ListMembers(ctx context.Context, projID, listID string, limit, offset int) ([]*subscriber.Subscriber, error)
	CountMembers(ctx context.Context, projID, listID string) (int, error)

	Unsubscribe(ctx context.Context, projID, listID, subscriberID, reason string) error
	Resubscribe(ctx context.Context, projID, listID, subscriberID string) error
	UnsubscribedIDs(ctx context.Context, projID, listID string) (map[string]struct{}, error)

	ResolveRecipients(ctx context.Context, subs SubscriberStore, projID string, l *subscriberlist.List) ([]*subscriber.Subscriber, error)
}

// CampaignStore persists campaigns and their per-recipient messages.
// The unscoped half (GetAny, ClaimDue, PromoteScheduled, message
// mutations) serves the runner.
type CampaignStore interface {
	Get(ctx context.Context, projID, id string) (*campaign.Campaign, error)
	GetAny(ctx context.Context, id string) (*campaign.Campaign, error)
	List(ctx context.Context, projID string) ([]*campaign.Campaign, error)
	Put(ctx context.Context, c *campaign.Campaign) error
	Delete(ctx context.Context, projID, id string) error
	TransitionStatus(ctx context.Context, projID, id, to string, from ...string) (bool, error)

	// Launch stamps the launch columns and leaves draft or scheduled in
	// one guarded statement. Put deliberately cannot touch the
	// lifecycle columns - see the store method.
	Launch(ctx context.Context, projID, id, status string, scheduledAt, startedAt, nextBatchAt *time.Time) (bool, error)

	// from is required on both of these. An unguarded status write from
	// the runner is what let a batch already in flight put a paused
	// campaign back to sending - see the store method.
	SetRunState(ctx context.Context, id, status string, startedAt, completedAt, nextBatchAt *time.Time, from ...string) (bool, error)
	Status(ctx context.Context, projID, id string) (string, error)
	PromoteScheduled(ctx context.Context, now time.Time) (int, error)
	ClaimDue(ctx context.Context, now time.Time, leaseFor time.Duration) (*campaign.Campaign, error)

	BulkCreateMessages(ctx context.Context, msgs []*campaign.Message) error
	HasMessages(ctx context.Context, campaignID string) (bool, error)
	PendingDue(ctx context.Context, campaignID string, now time.Time, limit int) ([]*campaign.Message, error)
	CountPending(ctx context.Context, campaignID string) (int, error)
	NextDeliverAt(ctx context.Context, campaignID string) (*time.Time, error)
	UpdateMessage(ctx context.Context, id, status, errMsg, emailID string) error
	MarkMessageByEmail(ctx context.Context, emailID, status, errMsg string) error
	SkipPending(ctx context.Context, campaignID, reason string) (int, error)
	ListMessages(ctx context.Context, projID, campaignID string, limit, offset int) ([]*campaign.Message, error)
	MessageStats(ctx context.Context, campaignID string) (map[string]int, map[string]map[string]int, error)

	GetMessageAny(ctx context.Context, id string) (*campaign.Message, error)

	// GetMessageByEmail is how tracking, which keys on the email id,
	// finds the campaign message behind one. Nil for a plain send.
	GetMessageByEmail(ctx context.Context, emailID string) (*campaign.Message, error)

	// MarkOpened reports whether a row changed, so the caller can
	// tell an already-open message from an id that names nothing.
	MarkOpened(ctx context.Context, messageID string, t time.Time) (bool, error)
	MarkClicked(ctx context.Context, messageID string, t time.Time) error
	UpsertTrackedLink(ctx context.Context, l *campaign.TrackedLink) error
	GetTrackedLink(ctx context.Context, projID, campaignID, hash string) (*campaign.TrackedLink, error)
	ListTrackedLinks(ctx context.Context, campaignID string) ([]*campaign.TrackedLink, error)

	// TrackedLinkURLs resolves link hashes to their original
	// destinations project wide - the preview's question, where no
	// scope is known. See the store method.
	TrackedLinkURLs(ctx context.Context, projID string, hashes []string) (map[string]string, error)
	IncrementLinkClicks(ctx context.Context, id string) error
	InsertTrackingEvent(ctx context.Context, ev *campaign.TrackingEvent) error
	EngagementStats(ctx context.Context, campaignID string) (opened, clicked int, err error)
	EventSeries(ctx context.Context, campaignID, eventType string) ([]campaign.DayCount, error)

	// Retention sweep, unscoped by project.
	PurgeTrackingEventsOlderThan(ctx context.Context, before time.Time) (int64, error)
}

// WebhookStore persists outgoing webhooks and their delivery log.
// List + RecordDelivery double as the dispatcher's Sink.
type WebhookStore interface {
	Get(ctx context.Context, projID, id string) (*webhook.Webhook, error)
	List(ctx context.Context, projID string) ([]*webhook.Webhook, error)
	Put(ctx context.Context, h *webhook.Webhook) error
	Delete(ctx context.Context, projID, id string) error
	RecordDelivery(ctx context.Context, d *webhook.Delivery) error
	ListDeliveries(ctx context.Context, projID, webhookID string, limit int, cur keyset.Cursor) ([]*webhook.Delivery, error)

	// Retention sweep, unscoped by project.
	PurgeDeliveriesOlderThan(ctx context.Context, before time.Time) (int64, error)
}

// AdminAPIKeyStore persists platform credentials.
//
// No project argument anywhere, which is the whole difference: these
// govern the installation, so there is nothing to scope them by. Its
// own interface for the same reason its own table exists - a method
// taking no projID cannot be added to APIKeyStore without inviting one
// that forgets to pass it.
type AdminAPIKeyStore interface {
	Get(ctx context.Context, id string) (*apikey.Admin, error)
	GetByPrefix(ctx context.Context, prefix string) (*apikey.Admin, error)
	List(ctx context.Context) ([]*apikey.Admin, error)
	Put(ctx context.Context, k *apikey.Admin) error
	Revoke(ctx context.Context, id string) error
	Delete(ctx context.Context, id string) error
	TouchLastUsed(ctx context.Context, id string, t time.Time) error
}

// APIKeyStore persists machine API keys. GetByPrefix is the auth
// lookup and intentionally not project scoped - the key decides
// the project.
type APIKeyStore interface {
	Get(ctx context.Context, projID, id string) (*apikey.Key, error)
	GetByPrefix(ctx context.Context, prefix string) (*apikey.Key, error)
	List(ctx context.Context, projID string) ([]*apikey.Key, error)
	Put(ctx context.Context, k *apikey.Key) error
	Revoke(ctx context.Context, projID, id string) error
	Delete(ctx context.Context, projID, id string) error
	TouchLastUsed(ctx context.Context, id string, t time.Time) error
	Count(ctx context.Context, projID string) (int, error)
}

// DomainStore persists inbound routing domains. Neither verified
// lookup is project scoped - the domain row decides the project.
//
// GetVerifiedCovering is the one every ownership question should ask:
// it accepts a subdomain of a verified domain, which is what the
// bounce-address and sending checks mean by "do you own this name".
// GetVerifiedByName is the exact-match primitive underneath it.
type DomainStore interface {
	Get(ctx context.Context, projID, id string) (*domain.Domain, error)
	GetVerifiedByName(ctx context.Context, name string) (*domain.Domain, error)
	GetVerifiedCovering(ctx context.Context, name string) (*domain.Domain, error)
	GetByName(ctx context.Context, name string) (*domain.Domain, error)
	List(ctx context.Context, projID string) ([]*domain.Domain, error)

	// VerifiedNames is the accept list a platform relay node running
	// an MX caches. Installation-wide, names only. VerifiedNamesIn is
	// the same list for a project's own node.
	VerifiedNames(ctx context.Context) ([]string, error)
	VerifiedNamesIn(ctx context.Context, projID string) ([]string, error)
	Put(ctx context.Context, d *domain.Domain) error
	SetVerified(ctx context.Context, projID, id string, verified bool, at time.Time) error
	Delete(ctx context.Context, projID, id string) error
	Count(ctx context.Context, projID string) (int, error)
}

// InboundStore persists mail received by the MX listener.
type InboundStore interface {
	Get(ctx context.Context, projID, id string) (*inbound.Email, error)
	List(ctx context.Context, projID string, f InboundFilter) ([]*inbound.Email, error)
	Put(ctx context.Context, e *inbound.Email) error
	Delete(ctx context.Context, projID, id string) error
	FindByDedupHash(ctx context.Context, projID, hash string) (*inbound.Email, error)
	CountByStatus(ctx context.Context, projID string) (map[string]int, error)

	// Retention sweep, unscoped by project.
	StorageKeysOlderThan(ctx context.Context, before time.Time) ([]string, error)
	PurgeOlderThan(ctx context.Context, before time.Time) (int64, error)
	ClearContentOlderThan(ctx context.Context, before time.Time) (int64, error)

	// Project deletion, where the rows go by cascade. A blob is named
	// only by the row cascading away, so the keys are read first.
	StorageKeysForProject(ctx context.Context, projID string) ([]string, error)
}

// SandboxStore holds mail that was captured instead of delivered.
//
// A separate table from emails on purpose - see the migration. The
// consequence for this interface is that it carries its own retention
// verbs rather than reusing the email ones: sandbox mail expires per
// message, and it is capped per project.
type SandboxStore interface {
	Get(ctx context.Context, projID, id string) (*sandbox.Email, error)
	Raw(ctx context.Context, projID, id string) ([]byte, error)
	List(ctx context.Context, projID string, limit, offset int) ([]*sandbox.Email, error)
	Count(ctx context.Context, projID string) (int, error)
	Put(ctx context.Context, e *sandbox.Email) error
	Delete(ctx context.Context, projID, id string) error
	Clear(ctx context.Context, projID string) (int64, error)

	// Trim keeps at most keep messages, dropping the oldest. Called on
	// every capture, so it must stay one statement.
	Trim(ctx context.Context, projID string, keep int) (int64, error)

	// Retention sweep, unscoped by project.
	PurgeExpired(ctx context.Context, now time.Time) (int64, error)
}

// InboundFilter narrows inbound listings.
//
// Before and BeforeID are one cursor, `(received_at, id)` - see
// EmailFilter for why the timestamp alone drops rows rather than
// repeating them, which here means received mail that appears on no page
// of the inbound log.
type InboundFilter struct {
	Status   string
	Limit    int
	Before   *time.Time
	BeforeID string
}

// LanguageStore persists the per-project language registry.
type LanguageStore interface {
	Get(ctx context.Context, projID, id string) (*language.Language, error)
	GetByCode(ctx context.Context, projID, code string) (*language.Language, error)
	List(ctx context.Context, projID string) ([]*language.Language, error)
	Put(ctx context.Context, l *language.Language) error
	Delete(ctx context.Context, projID, id string) error
}
