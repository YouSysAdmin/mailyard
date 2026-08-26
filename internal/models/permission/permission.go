// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package permission is what a project member may do, as a closed
// catalogue of resource plus action. It is the only vocabulary: a
// project's roles are rows built out of it, not tiers in front of it.
//
// Two axes rather than one rank. A total order cannot say "may read the
// mail log but not the SMTP credentials" - every role is a prefix of the
// one above, so the lowest rung that sees anything sees everything.
// There are no built-in roles either: a project writes its own.
//
// The catalogue is CLOSED. A resource not in Registry does not exist,
// since an entry nothing enforces is a lie to whoever reads it. A
// resource lands here with the route group that declares it, and a guard
// test fails the build otherwise.
package permission

import (
	"slices"
	"strings"
)

// Action is what is being done to a resource.
//
// Delete is its own action rather than a kind of write, because "may
// edit but not remove" is the shape projects most often want from a role
// they write themselves, and there is no built-in admin tier left to
// borrow for it.
//
// The mapping is mechanical so nobody has to guess: the DELETE method
// takes the delete action. A POST that erases - /sandbox/clear, the two
// data erasures - declares delete explicitly, the same way
// /templates/preview declares read. The action is a token on the route,
// never derived from the verb.
type Action string

// The three actions, and there is no fourth. Read is any retrieval,
// write is create and update alike, delete is removal - see Action for
// why write is not split.
const (
	ActionRead   Action = "read"
	ActionWrite  Action = "write"
	ActionDelete Action = "delete"
)

// Resource is one governed area of a project.
//
// The unit is the area a person recognises in the menu, not the table
// and not the URL. Templates, stylesheets and languages are one
// resource because "may edit templates" is the sentence somebody
// actually says - nobody grants stylesheets separately.
type Resource string

// The resource catalogue, and it is CLOSED: a project role can only
// name something listed here, so a permission that does not appear
// below cannot be granted by anybody. Grouped by what each governs -
// the mail-facing work, the infrastructure that carries it, and
// governance of the project itself.
const (
	// Mail-facing: what the project exists to do.
	ResourceEmails        Resource = "emails"
	ResourceCampaigns     Resource = "campaigns"
	ResourceTemplates     Resource = "templates"
	ResourceContacts      Resource = "contacts"
	ResourceSubscribers   Resource = "subscribers"
	ResourceSuppressions  Resource = "suppressions"
	ResourceBounces       Resource = "bounces"
	ResourceInbound       Resource = "inbound"
	ResourceAnalytics     Resource = "analytics"
	ResourceNotifications Resource = "notifications"
	ResourceSandbox       Resource = "sandbox"

	// Infrastructure: how mail physically leaves, and who may send it.
	ResourceDomains Resource = "domains"
	ResourceSenders Resource = "senders"
	ResourceSMTP    Resource = "smtp"
	ResourceAPIKeys Resource = "apikeys"
	ResourceWebhook Resource = "webhooks"

	// ResourceRelay is enrolling a relay node, kept apart from SMTP
	// rather than folded into it.
	//
	// It arrived as the scope relay:enroll, whose own comment gave the
	// reason it was not admin: a machine holding it receives the
	// content of the project's outbound mail to deliver, and a
	// credential handed to a deployment script for that one job should
	// not also read the email log. Granting it through smtp:write -
	// which is server configuration - would lose exactly that
	// distinction, so the scope became a resource rather than
	// disappearing into one.
	ResourceRelay Resource = "relay"

	// Governance.
	ResourceMembers  Resource = "members"
	ResourceSettings Resource = "settings"
	ResourceAudit    Resource = "audit"
	ResourceData     Resource = "data"
)

// All is the wildcard an owner or an admin holds. A literal star
// rather than an enumeration of every resource, so a new resource is
// covered the day it is added and nobody has to remember to widen the
// two presets that were always meant to hold everything.
const All = "*"

// Permission is the stored form, "resource:action".
type Permission string

// Of builds a permission.
func Of(r Resource, a Action) Permission { return Permission(string(r) + ":" + string(a)) }

// Definition describes one resource for the console and the docs.
type Definition struct {
	Resource Resource `json:"resource"`

	// Label is what a person is shown in the permission grid.
	Label string `json:"label"`

	// Description says what granting it actually opens up.
	Description string `json:"description"`

	// Actions are the actions this resource actually has, and the grid
	// renders a cell only where one is listed.
	//
	// Not every resource has all three, and pretending otherwise is
	// not a harmless simplification. The catalogue already advertised
	// three permissions no route has ever asked for - analytics:write,
	// contacts:write, audit:write - because the shape was assumed
	// rather than declared. Adding a blanket delete column would have
	// taken that to eighteen, and a grid where half the boxes do
	// nothing teaches people to stop reading it.
	//
	// TestEveryDeclaredActionIsEnforced pins this against the router in
	// both directions.
	Actions []Action `json:"actions"`

	// Infrastructure marks the resources that decide how mail leaves
	// and who may send it. The console groups them apart, because
	// granting one of these is a different kind of decision from
	// granting access to a mailing list.
	Infrastructure bool `json:"infrastructure"`
}

// The action sets, named so the registry below reads as a table
// rather than as twenty-one literals.
var (
	actR   = []Action{ActionRead}
	actRW  = []Action{ActionRead, ActionWrite}
	actRD  = []Action{ActionRead, ActionDelete}
	actRWD = []Action{ActionRead, ActionWrite, ActionDelete}
	actW   = []Action{ActionWrite}
)

// Registry is the full catalogue. Order is the order the console
// renders.
var Registry = []Definition{
	{ResourceEmails, "Emails", "The delivery log, message detail, and sending.", actRW, false},
	{ResourceCampaigns, "Campaigns", "Bulk sends and their per-recipient results.", actRWD, false},
	{ResourceTemplates, "Templates", "Templates, their versions, stylesheets and languages.", actRWD, false},

	// No write by design: the delivery worker writes these rows, not
	// an operator, and the store has no Put for the API to reach - a
	// tally somebody can edit is a number nobody can trust. Delete is a
	// person: a project that has mailed for years holds addresses it
	// will never mail again, and the next delivery recreates a contact
	// anyway, so removing one loses a tally and nothing else.
	{ResourceContacts, "Contacts", "Addresses the project has delivered to, and their tallies.", actRD, false},
	{ResourceSubscribers, "Subscribers", "Subscribers and the lists campaigns send to.", actRWD, false},
	{ResourceSuppressions, "Suppressions", "Blocked addresses and unsubscribe lists.", actRWD, false},

	// Write is the provider posting a bounce report, not a person.
	// Delete is a person: an address that bounced because the mailbox
	// did not exist YET keeps failing verification afterwards, and the
	// bounce is the record that says so. Separate from suppressions on
	// purpose - clearing the block and erasing the history are two acts,
	// and a credential may be trusted with one and not the other.
	{ResourceBounces, "Bounces", "Bounce reports.", actRWD, false},
	{ResourceInbound, "Inbound mail", "Mail received at the MX listener.", actRWD, false},
	{ResourceAnalytics, "Analytics", "The dashboard, delivery trend and plan usage.", actR, false},
	{ResourceNotifications, "Notifications", "In-project alerts and their read state.", actRWD, false},
	{ResourceSandbox, "Email sandbox", "Captured test mail, and the credentials that fill it.", actRWD, false},

	{ResourceDomains, "Domains", "Sending domains, their verification and DKIM keys.", actRWD, true},
	{ResourceSenders, "Sender addresses", "The approved From addresses.", actRWD, true},
	{ResourceSMTP, "SMTP servers", "Servers, server groups and the project's own relay nodes.", actRWD, true},
	{ResourceAPIKeys, "API keys", "API keys and SMTP submission credentials.", actRWD, true},
	{ResourceWebhook, "Webhooks", "Outgoing webhooks and their delivery history.", actRWD, true},

	// Enrolment only. There is nothing to read: a node that has
	// enrolled is administered through the SMTP resource.
	{ResourceRelay, "Relay node enrolment", "Enrolling a machine that carries this project's outbound mail.", actW, true},

	{ResourceMembers, "Members", "Who belongs to the project, its roles, and invitations. " +
		"A holder may only grant permissions they hold themselves.", actRWD, true},
	{ResourceSettings, "Project settings", "Project configuration.", actRW, true},
	{ResourceAudit, "Audit log", "The record of who changed what.", actR, true},

	// Export reads everything the project holds, erasure removes it.
	// There is nothing in between, so there is no write.
	{ResourceData, "Data export and erasure", "The full project export, and bulk erasure.", actRD, true},
}

// Lookup returns the definition for a resource.
func Lookup(res Resource) (Definition, bool) {
	for _, d := range Registry {
		if d.Resource == res {
			return d, true
		}
	}

	return Definition{}, false
}

// Allows reports whether this resource has action a at all. Parse
// consults it, so "contacts:write" is refused as firmly as a misspelt
// resource - both name something that does not exist.
func (d Definition) Allows(a Action) bool { return slices.Contains(d.Actions, a) }

// Set is what one caller holds. Membership is by exact string, plus the wildcard.
type Set map[Permission]struct{}

// NewSet builds a set from permissions.
func NewSet(perms ...Permission) Set {
	s := make(Set, len(perms))
	for _, p := range perms {
		s[p] = struct{}{}
	}

	return s
}

// Has reports whether the set allows action a on resource r.
func (s Set) Has(r Resource, a Action) bool {
	if s == nil {
		return false
	}

	if _, ok := s[All]; ok {
		return true
	}

	_, ok := s[Of(r, a)]

	return ok
}

// Missing returns the permissions in want that this set does not hold,
// so a caller can be told which ones rather than just refused.
//
// It exists for one rule: you cannot GRANT what you do not hold. Writing
// a role takes members:write, and nothing stopped that role from carrying
// every other permission in the catalogue - so a member trusted only to
// invite people could mint a role holding everything and assign it to
// themselves in two calls. The wildcard was refused in a role and
// enumerating the catalogue by hand was not, which is the same thing
// written out longhand.
//
// An owner holds All, so this returns nothing for them and they may
// delegate anything, which is what ownership means.
func (s Set) Missing(want []string) []string {
	var out []string
	for _, raw := range want {
		r, a, ok := Parse(raw)
		if !ok {
			// Not this function's job to reject - the caller validates
			// the catalogue first and would already have refused.
			continue
		}

		if !s.Has(r, a) {
			out = append(out, raw)
		}
	}

	return out
}

// Touches reports whether the set allows anything on r.
//
// Used by the route-group gate as an early refusal: a caller with no
// permission at all on a resource is turned away at the group rather
// than at each route, so a group nobody may reach costs one map
// lookup instead of a handler.
//
// It asks the resource which actions it has rather than trying all
// three. Spelling the list out here is how a resource that later grows
// an action stays unreachable through the group gate until somebody
// remembers to widen a predicate two files away.
func (s Set) Touches(r Resource) bool {
	if s == nil {
		return false
	}

	if _, ok := s[All]; ok {
		return true
	}

	d, ok := Lookup(r)
	if !ok {
		return false
	}

	for _, a := range d.Actions {
		if _, ok := s[Of(r, a)]; ok {
			return true
		}
	}

	return false
}

// List renders the set for the console, sorted, so the response is
// stable and a diff between two members is readable.
func (s Set) List() []string {
	out := make([]string, 0, len(s))
	for p := range s {
		out = append(out, string(p))
	}

	sortStrings(out)

	return out
}

// sortStrings is an insertion sort. The set is at most twice the
// registry, so this avoids pulling sort into a model package that is
// otherwise dependency-free apart from strings.
func sortStrings(xs []string) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j] < xs[j-1]; j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
}

// FromStrings builds a Set from stored permission strings - the shape
// a custom group keeps in the database.
//
// Entries Parse rejects are skipped, not fatal, and the asymmetry with
// the strict write-side validation is deliberate. A group is data, and
// data outlives code: a resource renamed two releases from now leaves
// a string in the column that no longer parses. Failing the whole set
// would lock every holder out of everything over one stale entry, and
// honoring it would grant something nobody defined. Skipping means the
// stale entry grants nothing and costs nothing else.
//
// Always a fresh map, never nil - Set{} for empty input. An empty
// group is a valid lockdown, and the caller must be able to tell it
// apart from "no group at all", which is ForMember's hasGroup flag.
func FromStrings(perms []string) Set {
	s := make(Set, len(perms))
	for _, p := range perms {
		if r, a, ok := Parse(p); ok {
			s[Of(r, a)] = struct{}{}
		}
	}

	return s
}

// Parse turns "emails:read" back into its parts. Invalid input yields
// ok=false rather than a zero value that would silently mean
// something.
//
// The action must exist ON THAT RESOURCE, not merely be one of the
// three. "contacts:write" names a real resource and a real action and
// is still nothing - contacts are written by the delivery worker and
// have no write route - so it is refused here rather than accepted
// into a role where it would read as a granted privilege forever.
func Parse(p string) (Resource, Action, bool) {
	res, act, found := strings.Cut(p, ":")
	if !found {
		return "", "", false
	}

	a := Action(act)
	r := Resource(res)
	d, ok := Lookup(r)
	if !ok || !d.Allows(a) {
		return "", "", false
	}

	return r, a, true
}
