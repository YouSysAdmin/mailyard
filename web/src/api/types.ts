// TypeScript mirrors of the Go models in internal/models. Field names
// follow the json tags exactly. Optional Go fields (omitempty or
// pointers) are optional here.

// 1 local, 2 OIDC. Decides whether this person may manage their own
// password, second factor and passkeys - an identity provider owns
// those on an OIDC account.
export type AccountType = 1 | 2

export interface User {
  id: string
  email: string
  account_type: AccountType
  // The whole of platform administration. It replaced role plus
  // super_user, which nothing anywhere told apart.
  admin: boolean
  disabled: boolean
  email_verified?: boolean
  totp_enabled?: boolean
  // Resolved server-side on every read, so the admin list can disable
  // a reset button that would have nothing to do.
  passkey_count?: number
  created_at: string
  last_login_at?: string
}

export interface Project {
  id: string
  name: string
  slug: string
  description?: string
  owner_id?: string
  default_language: string
  // Assigned usage plan. Empty means the default plan applies.
  plan_id?: string
  // When true, sends must use a registered sender address.
  strict_senders?: boolean
  // Defaults for mail sent outside a campaign. Campaigns track
  // regardless of these.
  track_opens?: boolean
  track_clicks?: boolean
  bounce_address?: string
  // Where alerts go BESIDE the owners. Additive, never a replacement -
  // see the server field.
  alert_email?: string
  sandbox_retention_days?: number
  // The role members carry when their own membership names none.
  // Empty means they reach nothing at all.
  default_role_id?: string
  created_at: string
  updated_at?: string
  // Attached client-side from the list endpoint response.
  owner?: boolean
}

export interface ProjectMember {
  id: string
  project_id: string
  user_id: string
  email?: string
  // Ownership is not a role: it grants everything in the catalogue
  // plus deleting the project and rewriting its SSO policy.
  owner: boolean
  // The role in force. Empty means the project named no default
  // either, so this member reaches nothing.
  role_id?: string
  role_name?: string
  // The role came from the project default rather than from this
  // member - the two survive a change to the default differently.
  inherited_role?: boolean
  created_at: string
}

// ProjectRole is a permission list a project wrote for itself. There
// are no built-in roles. permissions holds "resource:action" strings
// from the catalogue.
export interface ProjectRole {
  id: string
  project_id: string
  name: string
  description?: string
  permissions: string[]
  // How many members currently carry it, including those holding it
  // by way of the project default - a referenced role cannot be
  // deleted.
  members: number
  // The project names this role as the one its roleless members
  // carry. Also undeletable while true.
  default: boolean
  created_at: string
  updated_at?: string
}

// PermissionResource is one catalogue entry, served by
// GET /api/permissions so the grid and the enforcement share a source.
export interface PermissionResource {
  resource: string
  label: string
  description: string
  // The actions this resource actually has. Most have fewer than
  // three, and the grid renders a checkbox only where one is listed -
  // a box that grants nothing teaches people to stop reading them.
  actions: string[]
  infrastructure: boolean
}

export interface ProjectInvitation {
  id: string
  project_id: string
  email: string
  // The role offered. Empty offers the project default, which is also
  // where an invitation lands whose role was deleted before anybody
  // accepted it.
  role_id?: string
  role_name?: string
  status: 'pending' | 'accepted'
  invited_by: string
  expires_at: string
  created_at: string
}

// RelayNode is an egress machine that enrolled itself and delivers
// straight to recipient mail exchangers from its own address.
//
// It IS a shared pool server - the id here is its own identity, and
// server_id points at the delivery row. Two states matter and they are
// separate: status is whether an admin approved it, alive is whether
// it has reported in recently. A node can be approved and dead.
export interface RelayNode {
  id: string
  server_id: string
  project_id?: string
  name: string
  version?: string
  // public_ip is what the control plane observed, not what the node
  // claimed. It has to be authorized in the bounce domain's SPF.
  public_ip?: string
  last_seen_at?: string
  created_at: string
  host: string
  port: number
  status: 'enabled' | 'disabled' | 'invalid' | 'pending'
  alive: boolean
  // The receiving half, for a node that also runs an MX.
  //
  // inbound_enabled and inbound_queued are what the node reports.
  // last_inbound_at is what the platform observed - a queue that only
  // grows while this stays old is an MX taking mail it cannot hand
  // back, which is otherwise invisible: bounces just stop appearing.
  inbound_enabled: boolean
  inbound_queued: number
  last_inbound_at?: string
}

// SharedSMTPServer is a platform-owned server, the fallback for
// projects that have configured none of their own. Admin-only: no
// project-scoped endpoint returns one of these.
export interface SharedSMTPServer {
  provider?: string
  provider_config?: Record<string, string>
  id: string
  created_by?: string
  name: string
  host: string
  port: number
  username?: string
  encryption: 'none' | 'starttls' | 'ssl'
  skip_dkim: boolean
  allowed_emails: string[]
  // allowed_domains restricts which sender domains may relay through
  // this server. Empty allows any.
  allowed_domains: string[]
  // strict additionally requires the sending project to have verified
  // the sender's domain, which is what stops one project relaying as
  // another's through platform credentials.
  security_mode: 'permissive' | 'strict'
  // Reserved for the platform's own mail - invitations, password
  // resets, signup confirmations. No tenant is routed through it.
  platform_only: boolean
  // ses_topic_arn is the SNS topic SES publishes this server's
  // bounces to. On the server rather than in platform config because
  // SES belongs to one server - and it is what ties a notification to
  // the project whose mail it is about.
  ses_topic_arn?: string
  priority: number
  status: 'enabled' | 'disabled' | 'invalid' | 'pending'
  validation_error?: string
  validated_at?: string
  created_at: string
}

// SMTPServerGroup is a named pool a send can be routed to, and the
// unit failover happens within. Every project has exactly one with
// is_default set, used when a send names none.
export interface SMTPServerGroup {
  id: string
  project_id: string
  name: string
  slug: string
  description?: string
  is_default: boolean
  created_at: string
  // Filled by the list and get reads, never sent back.
  servers?: SMTPServer[]
}

// A mail provider this build can send through, as the server describes
// it. The form is built from these rather than from a list here, so
// adding a provider does not need a matching edit in TypeScript.
export interface MailProvider {
  id: string
  label: string
  // dial says the provider connects to a host and port, which is what
  // decides whether the form asks for them.
  dial: boolean
  options?: Array<{ key: string; label: string; required: boolean; hint?: string }>
  credential_hint?: string
  // re_signs says the provider rewrites headers and signs the result
  // itself, so our signature cannot survive. The form hides the
  // skip-DKIM choice for those: it has one correct answer, and the
  // server enforces it whatever a client sends.
  re_signs: boolean
}

export interface SMTPServer {
  id: string
  project_id: string
  created_by?: string
  name: string
  // provider is how the row is reached. Absent means smtp, which is what
  // every row was before providers existed.
  provider?: string
  provider_config?: Record<string, string>
  host: string
  port: number
  username?: string
  encryption: 'none' | 'starttls' | 'ssl'
  // skip_dkim turns off Mailyard's own DKIM signature for this server,
  // for providers that rewrite headers and re-sign (Amazon SES).
  skip_dkim: boolean
  allowed_emails: string[]
  // allowed_domains restricts which sender domains this server carries.
  // Empty carries any, and the match is exact - a listed name does not
  // cover its subdomains, because SPF is written per name.
  allowed_domains: string[]
  // ses_topic_arn is the SNS topic SES publishes this server's bounces
  // to. Only SES replaces the envelope sender, so it is the only case
  // where feedback cannot come back as a DSN.
  ses_topic_arn?: string
  // group_id is the pool this server belongs to, priority its order
  // within it (lowest first).
  group_id?: string
  priority: number
  status: 'enabled' | 'disabled' | 'invalid'
  validation_error?: string
  validated_at?: string
  created_at: string
}

export interface Template {
  id: string
  project_id: string
  name: string
  description?: string
  default_language: string
  active_version_id?: string
  sample_data?: string
  created_by?: string
  last_edited_by?: string
  created_at: string
  updated_at?: string
}

export interface TemplateVersion {
  id: string
  template_id: string
  version: number
  stylesheet_id?: string
  sample_data?: string
  created_at: string
}

export interface TemplateLocalization {
  id: string
  version_id: string
  language: string
  subject: string
  html?: string
  text?: string
  created_at: string
  updated_at?: string
}

export interface Stylesheet {
  id: string
  project_id: string
  name: string
  css: string
  created_at: string
  updated_at?: string
}

export interface Language {
  id: string
  project_id: string
  code: string
  name: string
  is_default: boolean
  created_at: string
}

export type EmailStatus =
  'pending' | 'queued' | 'processing' | 'sent' | 'failed' | 'suppressed' | 'scheduled'

export interface EmailAttachment {
  filename: string
  content?: string
  content_type?: string
}

export interface Email {
  id: string
  project_id: string
  created_by?: string
  api_key_id?: string
  smtp_server_id?: string
  // The server that actually carried it, which after a failover walk is
  // NOT necessarily smtp_server_id - that one is what the sender asked
  // for and keeps meaning that across retries. Empty until a delivery
  // succeeds.
  delivered_via?: string
  // The pool it was routed through, resolved from the caller's slug at
  // accept time so a queued row never holds a name that can be renamed
  // out from under it.
  smtp_group_id?: string
  sender: string
  recipients: string[]
  subject: string
  template_name?: string
  html_body?: string
  text_body?: string
  attachments?: EmailAttachment[]
  headers?: Record<string, string>
  list_unsubscribe_url?: string
  list_unsubscribe_mailto?: string
  list_unsubscribe_post?: boolean
  // The transactional opt-out scope this send belongs to, empty for an
  // unscoped send.
  unsubscribe_list_id?: string
  status: EmailStatus
  error_message?: string
  // tracked says the message went out with tracking applied, so a
  // zero open_count means nobody opened it rather than that nobody
  // was counting.
  tracked?: boolean
  opened_at?: string
  clicked_at?: string
  open_count?: number
  click_count?: number
  attempts: number
  max_attempts: number
  next_attempt_at?: string
  created_at: string
  scheduled_at?: string
  sent_at?: string
}

export interface APIKey {
  id: string
  project_id: string
  created_by?: string
  name: string
  key_prefix: string
  // Catalogue strings ("emails:write"), or the single wildcard "*".
  // The same vocabulary a member is judged by - there is no second
  // list of machine-only grants any more.
  permissions: string[]
  allowed_ips: string[]
  // sandbox: sends made with this key are captured into the project
  // sandbox and never delivered.
  sandbox: boolean
  revoked: boolean
  expires_at?: string
  last_used_at?: string
  created_at: string
}

// SMTPCredential is a submission login. No scopes and no expiry - it
// is either usable or revoked.
export interface SMTPCredential {
  id: string
  project_id: string
  created_by?: string
  name: string
  username: string
  allowed_ips: string[]
  smtp_group_id?: string
  // sandbox: submissions with this credential are captured into the
  // project sandbox and never delivered.
  sandbox: boolean
  revoked: boolean
  last_used_at?: string
  created_at: string
}

// SubmissionInfo mirrors the listener settings the server reports
// alongside the credential list.
export interface SubmissionInfo {
  enabled: boolean
  host: string
  port: string
  starttls: boolean
}

export type SuppressionKind = 'hard' | 'bounce' | 'complaint' | 'manual'

export interface Suppression {
  id: string
  project_id: string
  email: string
  kind: SuppressionKind
  reason?: string
  /**
   * Scopes the block to one unsubscribe list. Absent means a global
   * block covering every send from the project. An address can have
   * both - the rows are unique on (project, email, list).
   */
  unsubscribe_list_id?: string
  created_at: string
}

export type BounceType = 'hard' | 'soft' | 'complaint'

export interface Bounce {
  id: string
  project_id: string
  email_id?: string
  recipient: string
  type: BounceType
  reason?: string
  created_at: string
}

export const WEBHOOK_EVENTS = [
  'email.queued',
  'email.sent',
  'email.failed',
  'email.suppressed',
  'campaign.started',
  'campaign.completed',
  'inbound.received',
] as const

export type WebhookEvent = (typeof WEBHOOK_EVENTS)[number]

export interface Webhook {
  id: string
  project_id: string
  created_by?: string
  url: string
  events: string[]
  filters: string[]
  created_at: string
  disabled_at?: string
  disabled_reason?: string
}

export interface WebhookDelivery {
  id: string
  webhook_id: string
  project_id: string
  event: string
  status: 'success' | 'failed'
  http_status?: number
  error_message?: string
  attempt: number
  created_at: string
}

export type SubscriberStatus = 'subscribed' | 'unsubscribed' | 'bounced' | 'complained'

export interface Subscriber {
  id: string
  project_id: string
  email: string
  name?: string
  status: SubscriberStatus
  custom_fields?: Record<string, unknown>
  timezone?: string
  language?: string
  subscribed_at?: string
  unsubscribed_at?: string
  created_at: string
  updated_at?: string
}

export type FilterOperator =
  'eq' | 'neq' | 'contains' | 'starts_with' | 'ends_with' | 'gt' | 'lt' | 'exists'

export interface FilterRule {
  field: string
  operator: FilterOperator
  value?: unknown
}

export interface SubscriberList {
  id: string
  project_id: string
  name: string
  description?: string
  type: 'static' | 'dynamic'
  filter_rules: FilterRule[]
  created_at: string
  updated_at?: string
}

export type CampaignStatus = 'draft' | 'scheduled' | 'sending' | 'paused' | 'sent' | 'cancelled'

export interface CampaignVariant {
  name: string
  subject?: string
  template_id?: string
  split_percentage: number
}

export interface Campaign {
  id: string
  project_id: string
  created_by?: string
  name: string
  subject?: string
  from_email: string
  from_name?: string
  template_id: string
  language?: string
  template_data?: Record<string, unknown>
  status: CampaignStatus
  list_id: string
  // Which SMTP pool the whole campaign sends through, resolved from the
  // slug the payload takes. It was missing here while the server has
  // always answered with it, so the edit form had nothing to read the
  // current group back from - and a save that omits it CLEARS it.
  smtp_group_id?: string
  send_rate: number
  send_at_local_time: boolean
  ab_test_enabled: boolean
  ab_variants?: CampaignVariant[]
  scheduled_at?: string
  started_at?: string
  completed_at?: string
  created_at: string
  updated_at?: string
}

export type CampaignMessageStatus = 'pending' | 'queued' | 'sent' | 'failed' | 'skipped'

export interface CampaignMessage {
  id: string
  campaign_id: string
  subscriber_id: string
  // email is the subscriber's address, joined in by the list query.
  // Absent when the subscriber was deleted after the fan-out.
  email?: string
  email_id?: string
  status: CampaignMessageStatus
  error_message?: string
  variant?: string
  deliver_at?: string
  sent_at?: string
  opened_at?: string
  clicked_at?: string
  created_at: string
}
