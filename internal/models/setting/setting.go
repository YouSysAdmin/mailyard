// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package setting models platform-wide settings: the knobs an
// administrator can turn at runtime, as opposed to the ones in
// mailyard.yaml that need a restart.
//
// Only keys in Registry exist. An unknown key is rejected on write
// rather than silently stored, so the settings table can never drift
// into holding values nothing reads.
package setting

import (
	"encoding/json"
	"strings"
	"time"
)

// Value types.
const (
	TypeString = "string"
	TypeInt    = "int"
	TypeBool   = "bool"

	// TypeList is several values in one setting, stored as the JSON array
	// StringList reads.
	//
	// Declaring the type is what lets the console render a textarea and
	// convert one value per line. As a plain string it becomes a 110px
	// box an operator writes brackets and commas into, where a typo
	// parses to nothing - StringList answers nil on a decode error, so a
	// malformed host list reads as "none configured".
	//
	// It belongs in the registry rather than being special-cased by key
	// in TypeScript, or the next list setting gets it wrong again.
	TypeList = "list"
)

// Keys. Every one of these is actually consulted by something - a
// setting nothing reads is a lie to the operator, so new keys land
// here at the same time as the code that honors them.
const (
	// KeyRetentionDays bounds how long email metadata rows live.
	// Zero disables purging entirely.
	//
	// Thirty days, unlike every other window here, which keeps forever
	// until an operator says otherwise. This is the table that grows
	// per message, so "keep everything" is a disk that fills while
	// nobody has decided anything. It is also what the weekly
	// partitioning is sized for - five live partitions, one dropped a
	// week.
	KeyRetentionDays = "retention_days"

	// KeyEmailBodyRetentionDays bounds how long rendered bodies are
	// kept on an email row. Zero follows retention_days.
	KeyEmailBodyRetentionDays = "email_body_retention_days"

	// KeyEmailAttachmentRetentionDays bounds attachment bytes on
	// outbound and inbound mail. Zero follows retention_days.
	KeyEmailAttachmentRetentionDays = "email_attachment_retention_days"

	// KeyInboundRetentionDays bounds received mail. Zero follows
	// retention_days.
	KeyInboundRetentionDays = "inbound_retention_days"

	// KeyWebhookDeliveryRetentionDays bounds the delivery log.
	KeyWebhookDeliveryRetentionDays = "webhook_delivery_retention_days"

	// KeyTrackingEventRetentionDays bounds open and click events.
	KeyTrackingEventRetentionDays = "tracking_event_retention_days"

	// KeyAuditLogRetentionDays bounds the audit trail.
	KeyAuditLogRetentionDays = "audit_log_retention_days"

	// KeyMaintenanceMode parks the API: writes are refused for
	// everyone but a platform admin.
	KeyMaintenanceMode = "maintenance_mode"

	// KeyTLSCertificate* name the managed certificate each listener
	// serves. Empty keeps whatever its yaml block builds.
	//
	// Settings and not config: an expired certificate is the one
	// emergency where a fix must not need a shell on the box and a
	// restart of the mail server.
	KeyTLSCertificateServer     = "tls_certificate_server"
	KeyTLSCertificateSubmission = "tls_certificate_submission"
	KeyTLSCertificateInbound    = "tls_certificate_inbound"

	// Where the platform's own mail says it comes from. It leaves
	// through a row in the shared pool, so this is all an operator
	// sets here.
	//
	// Settings and not yaml, because the pool beside it is already
	// edited in the console and a wrong from address must not need a
	// restart to correct. Empty turns platform mail off: invitations
	// return a copyable link and password reset is unavailable.
	KeyPlatformMailFrom     = "platform_mail_from"
	KeyPlatformMailFromName = "platform_mail_from_name"

	// KeyNotificationRetentionDays bounds how long read notifications
	// live. Unread ones are never purged by age - an alert nobody has
	// looked at is still trying to say something.
	KeyNotificationRetentionDays = "notification_retention_days"

	// KeyBounceAlertPercent is the bounce rate, as a percentage of a
	// project's finished sends in the last hour, that raises an
	// alert. Zero turns the alert off.
	KeyBounceAlertPercent = "bounce_alert_percent"

	// KeyBounceAlertMinVolume is how many messages must have finished
	// in the window before the rate is trusted. Two bounces out of
	// three sends is 66% and means nothing.
	KeyBounceAlertMinVolume = "bounce_alert_min_volume"

	// KeySandboxRetentionDays is how long a captured message is kept.
	// Zero keeps them until the per-project cap pushes them out.
	//
	// Seven days rather than the zero every other window defaults to:
	// sandbox mail is throwaway, and nobody comes back to a test
	// message from last quarter.
	KeySandboxRetentionDays = "sandbox_retention_days"

	// KeySandboxMaxMessages caps how many messages one project keeps.
	// Zero is unlimited.
	//
	// This, not the day window, is what actually bounds the table. A
	// test suite can write ten thousand messages in a morning and a
	// seven day window does nothing about it until day seven.
	KeySandboxMaxMessages = "sandbox_max_messages"

	// KeyRelayNodesAutoApprove lets a node that enrolled with a valid
	// token start carrying mail immediately, instead of waiting for an
	// admin.
	//
	// Off by default, and that is the point: a node in the pool
	// receives the content of real messages, and the enrolment token is
	// one secret shared by the whole fleet. With approval in the way a
	// leaked token gets somebody a pending row an admin will not
	// recognise - without it, a copy of everybody's mail.
	//
	// Turn it on where nodes come and go on their own.
	KeyRelayNodesAutoApprove = "relay_nodes_auto_approve"

	// KeyUserProjectCreation lets an ordinary account create a project.
	//
	// Off by default, so a new installation is one where projects are
	// made by a platform admin and joined by invitation. That is the
	// shape most installations want and the one that cannot be got back
	// to afterwards: a tenant somebody created is a tenant with mail,
	// members and keys in it, and deleting it is not a settings change.
	//
	// A platform admin is never subject to this - somebody has to be
	// able to make the first project on an installation where nobody
	// else may.
	//
	// It gates CREATION only. Belonging to no project stays an ordinary
	// state either way: the projects page answers it, and an invitation
	// is the way in.
	KeyUserProjectCreation = "user_project_creation"

	// KeySecurityAlertsEnabled mails somebody when access changes or a
	// project has a problem - see internal/core/alertmail for the list.
	//
	// On by default, or everything the installation knows about a problem
	// stays on a screen nobody is looking at. It does nothing until
	// platform mail is configured, which is itself opt-in.
	KeySecurityAlertsEnabled = "security_alerts_enabled"

	// KeyACME* configure Let's Encrypt.
	//
	// Settings and not config, because what kept them in a file went
	// away: a yaml key earns its place by binding a port, and
	// tls-alpn-01 needs none - the CA validates against the TLS
	// listener that is already up. So ACME is turned on, given hosts
	// and ordered from without touching a file or restarting.
	KeyACMEEnabled = "acme_enabled"

	// KeyACMEHosts is a JSON array of names, the shape every string list
	// is stored in here. Declared TypeList, so the settings page offers a
	// textarea rather than asking for the JSON by hand.
	KeyACMEHosts = "acme_hosts"
	KeyACMEEmail = "acme_email"

	// KeyACMEDirectoryURL points at a different CA.
	//
	// For the staging directory. Let's Encrypt allows five duplicates a
	// week per name set, so debugging issuance can burn the budget in
	// an afternoon. Staging is far looser and issues an untrusted
	// certificate, which is right for finding out whether validation
	// reaches you.
	KeyACMEDirectoryURL = "acme_directory_url"
)

// Definition describes one known setting.
type Definition struct {
	Key     string `json:"key"`
	Type    string `json:"type"`
	Default string `json:"default"`

	// Description is shown in the admin UI.
	Description string `json:"description"`

	// Unit is a display hint ("days", empty for none).
	Unit string `json:"unit,omitempty"`

	// ManagedAt is the console route with the real editor for this
	// setting, ManagedIn is that page's name. Empty means the settings
	// list is the only editor. Set, it shows the value read-only and
	// links there instead of offering a second control.
	ManagedAt string `json:"managed_at,omitempty"`
	ManagedIn string `json:"managed_in,omitempty"`

	// Edition names a build this setting only does anything in. Empty
	// means both, which is nearly every key.
	//
	// The key stays REGISTERED either way. It is a stored value, and
	// dropping it from the registry would refuse writes to a row that
	// already exists and lose an operator's answer across an edition
	// change. What this field buys is the registry's own rule - a
	// setting nothing reads is a lie to the operator - so the console
	// renders it as a control that says which build it belongs to
	// rather than one that silently governs nothing.
	Edition string `json:"edition,omitempty"`
}

// EditionEnterprise marks a setting that only does something in the
// enterprise build. It is the string internal/core/edition reports, not
// a second vocabulary - the console compares the two directly.
const EditionEnterprise = "enterprise"

// The certificates page has the host list, the Order button and the
// listener selectors, so it owns every setting they touch.
const (
	certificatesRoute = "admin/certificates"
	certificatesPage  = "Certificates"
)

// Registry is the full set of settable keys.
var Registry = []Definition{
	{
		Key: KeyNotificationRetentionDays, Type: TypeInt, Default: "30", Unit: "days",
		Description: "Days to keep notifications that have been read. Unread ones are always kept.",
	},
	{
		Key: KeyBounceAlertPercent, Type: TypeInt, Default: "10", Unit: "%",
		Description: "Bounce rate over the last hour that raises a project alert. 0 turns the alert off.",
	},
	{
		Key: KeyBounceAlertMinVolume, Type: TypeInt, Default: "20", Unit: "emails",
		Description: "How many sends must finish in the hour before the bounce rate is judged.",
	},
	{
		Key: KeyRetentionDays, Type: TypeInt, Default: "30", Unit: "days",
		Description: "Days to keep email log rows. Whole DAILY partitions past this window are dropped rather than deleted row by row, so the window you set is the window you get. 0 keeps them forever, which means a disk that fills while nobody has decided anything - and, since partitions are never dropped either, a partition count that grows by 365 a year until concurrent queue claims start failing.",
	},
	{
		Key: KeyEmailBodyRetentionDays, Type: TypeInt, Default: "0", Unit: "days",
		Description: "Days to keep rendered HTML and text on an email row. 0 follows the email log window.",
	},
	{
		Key: KeyEmailAttachmentRetentionDays, Type: TypeInt, Default: "0", Unit: "days",
		Description: "Days to keep attachment bytes. 0 follows the email log window.",
	},
	{
		Key: KeyInboundRetentionDays, Type: TypeInt, Default: "0", Unit: "days",
		Description: "Days to keep received mail. 0 follows the email log window.",
	},
	{
		Key: KeyWebhookDeliveryRetentionDays, Type: TypeInt, Default: "30", Unit: "days",
		Description: "Days to keep webhook delivery history. 0 keeps it forever.",
	},
	{
		Key: KeyTrackingEventRetentionDays, Type: TypeInt, Default: "0", Unit: "days",
		Description: "Days to keep open and click events. 0 keeps them forever.",
	},
	{
		Key: KeyAuditLogRetentionDays, Type: TypeInt, Default: "90", Unit: "days",
		Description: "Days to keep audit log entries. 0 keeps them forever.",
	},
	{
		Key: KeySandboxRetentionDays, Type: TypeInt, Default: "7", Unit: "days",
		Description: "Days to keep a captured sandbox message. 0 keeps it until the per-project cap pushes it out. A sender may ask for a shorter window per message, never a longer one.",
	},
	{
		Key: KeySandboxMaxMessages, Type: TypeInt, Default: "500", Unit: "messages",
		Description: "How many sandbox messages one project keeps. The oldest are dropped past this. 0 is unlimited, which on a project wired into CI means the table grows until the disk says otherwise.",
	},
	{
		Key: KeyMaintenanceMode, Type: TypeBool, Default: "false",
		Description: "Refuse write requests from everyone but platform admins.",
	},
	{
		Key: KeyTLSCertificateServer, Type: TypeString, Default: "",
		ManagedAt: certificatesRoute, ManagedIn: certificatesPage,
		Description: "Name of the managed certificate the HTTP listener serves. Empty keeps whatever server.tls builds.",
	},
	{
		Key: KeyTLSCertificateSubmission, Type: TypeString, Default: "",
		ManagedAt: certificatesRoute, ManagedIn: certificatesPage,
		Description: "Name of the managed certificate the SMTP submission listener serves. Empty keeps whatever submission.tls builds.",
	},
	{
		Key: KeyTLSCertificateInbound, Type: TypeString, Default: "",
		ManagedAt: certificatesRoute, ManagedIn: certificatesPage,
		Description: "Name of the managed certificate the inbound MX listener serves. Empty keeps whatever inbound.tls builds.",
	},
	{
		Key: KeyACMEEnabled, Type: TypeBool, Default: "false",
		ManagedAt: certificatesRoute, ManagedIn: certificatesPage,
		Description: "Order certificates from a CA. A listener reaches these only when nothing is assigned to it, and a name that is not listed falls through to the self-signed pair rather than failing the handshake. On its own this orders nothing - name a host too.",
	},
	{
		Key: KeyACMEHosts, Type: TypeList, Default: "",
		ManagedAt: certificatesRoute, ManagedIn: certificatesPage,
		Description: "Hostnames to issue for, one per line. There is no default and the name in server.public_url is not used, so an empty list issues nothing. No wildcard is ever requested - the challenges this speaks cannot issue one.",
	},
	{
		Key: KeyACMEEmail, Type: TypeString, Default: "",
		ManagedAt: certificatesRoute, ManagedIn: certificatesPage,
		Description: "ACME account contact, where the CA sends expiry warnings. Used when the account is first registered, so changing it later does not re-register.",
	},
	{
		Key: KeyACMEDirectoryURL, Type: TypeString, Default: "",
		ManagedAt: certificatesRoute, ManagedIn: certificatesPage,
		Description: "A different ACME directory. Empty is Let's Encrypt production. Point it at the staging directory (https://acme-staging-v02.api.letsencrypt.org/directory) while working out why issuance fails - production allows five duplicate certificates per week and a debugging session spends that quickly.",
	},
	{
		Key: KeyPlatformMailFrom, Type: TypeString, Default: "",
		Description: "Address the platform's own mail comes from - invitations, password resets, signup confirmations. It leaves through the shared SMTP pool. Empty turns platform mail off, and invitations then return a copyable link instead.",
	},
	{
		Key: KeyPlatformMailFromName, Type: TypeString, Default: "",
		Description: "Display name beside the platform mail address. Optional.",
	},
	{
		Key: KeySecurityAlertsEnabled, Type: TypeBool, Default: "true",
		Description: "Mail somebody when access changes or a project has a problem - API keys, members, ownership, sign-in policy, data erasure, platform credentials, and the bounce rate alert. Project alerts go to the project owners and its alert address, account alerts to the account itself, installation alerts to every administrator. Needs platform mail configured.",
	},
	{
		Key: KeyUserProjectCreation, Type: TypeBool, Default: "false",
		Description: "Let any signed-in account create a project. Off, only platform administrators can, and everybody else joins a project by invitation - which is the arrangement to keep on an installation shared by teams, since a project somebody created is a tenant with its own mail, members and API keys. Platform administrators are never subject to this.",
	},
	{
		Key: KeyRelayNodesAutoApprove, Type: TypeBool, Default: "false",
		Edition:     EditionEnterprise,
		Description: "Let a relay node start delivering as soon as it enrols, without an admin approving it. A node in the pool receives the content of real messages, and the enrolment token is shared by every node - leave this off unless nodes are created and destroyed automatically.",
	},
}

// Lookup returns the definition for a key.
func Lookup(key string) (Definition, bool) {
	for _, d := range Registry {
		if d.Key == key {
			return d, true
		}
	}

	return Definition{}, false
}

// Setting is one stored override. Rows exist only for keys an
// administrator has actually changed - everything else resolves to
// the registry default.
type Setting struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	Type      string    `json:"type"`
	UpdatedAt time.Time `json:"updated_at"`
	UpdatedBy string    `json:"updated_by,omitempty"`
}

// StringList reads a setting stored as a JSON array.
//
// The shape every string list is stored in here - a JSON array in a TEXT
// column, portable across engines. Anything unparseable reads as empty
// rather than erroring: these are read on the handshake path, and a
// malformed row must not take a listener down. An empty list means
// nothing is allowed, which for an ACME host list is the safe direction.
func StringList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}

	cleaned := out[:0]
	for _, v := range out {
		if v = strings.TrimSpace(v); v != "" {
			cleaned = append(cleaned, v)
		}
	}

	return cleaned
}

// EncodeStringList is the other direction, for a caller writing one.
func EncodeStringList(items []string) string {
	var cleaned []string
	for _, v := range items {
		if v = strings.TrimSpace(v); v != "" {
			cleaned = append(cleaned, v)
		}
	}

	if len(cleaned) == 0 {
		return ""
	}

	b, err := json.Marshal(cleaned)
	if err != nil {
		return ""
	}

	return string(b)
}
