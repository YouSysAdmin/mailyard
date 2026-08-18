// The shape a campaign form holds, and the one way it becomes a request.
//
// Creating and editing a campaign are the same eleven fields, and they
// were two copies: two form objects, two field lists, two payload
// builders. The copies drifted exactly the way copies do - the create
// dialog offered a Server group and the edit form did not, and because
// the endpoint REBUILDS the record from the body, saving an edit cleared
// the pool the campaign was routed to. Silently.
//
// So `toPayload` is the only place a draft becomes a request. A field
// added to the form and forgotten in one of two submits is no longer a
// thing that can happen.
import type { CampaignPayload } from '../../api/campaigns'
import type { Campaign, CampaignVariant } from '../../api/types'

/** What the form holds. Strings throughout - it is bound to inputs. */
export interface CampaignDraft {
  name: string
  subject: string
  from_email: string
  from_name: string
  template_id: string
  language: string
  list_id: string
  send_rate: number
  /** The pool's SLUG. The stored record holds its id - see fromCampaign. */
  smtp_group: string
  send_at_local_time: boolean
  /** Raw JSON, parsed at submit so a half-typed object is not an error. */
  template_data: string
  ab_test_enabled: boolean
}

/** An empty draft, for the create dialog. */
export function blankDraft(): CampaignDraft {
  return {
    name: '',
    subject: '',
    from_email: '',
    from_name: '',
    template_id: '',
    language: '',
    list_id: '',
    send_rate: 0,
    smtp_group: '',
    send_at_local_time: false,
    template_data: '',
    ab_test_enabled: false,
  }
}

/**
 * A stored campaign as a draft.
 *
 * `smtp_group` is left empty here on purpose: the record holds the
 * group's ID and the endpoint takes its slug, so only the group list
 * can translate - the form fills it once that has arrived.
 */
export function fromCampaign(c: Campaign): CampaignDraft {
  return {
    name: c.name,
    subject: c.subject ?? '',
    from_email: c.from_email,
    from_name: c.from_name ?? '',
    template_id: c.template_id,
    language: c.language ?? '',
    list_id: c.list_id,
    send_rate: c.send_rate,
    smtp_group: '',
    send_at_local_time: c.send_at_local_time,
    template_data: c.template_data ? JSON.stringify(c.template_data, null, 2) : '',
    ab_test_enabled: c.ab_test_enabled,
  }
}

/**
 * The request body, for create and for update alike.
 *
 * Throws a SyntaxError on unparseable template data, which both callers
 * catch and report as such - the server would only say "invalid body".
 */
export function toPayload(d: CampaignDraft, variants: CampaignVariant[]): CampaignPayload {
  return {
    name: d.name.trim(),
    subject: d.subject.trim(),
    from_email: d.from_email.trim(),
    from_name: d.from_name.trim(),
    template_id: d.template_id,
    language: d.language.trim() || undefined,
    template_data: d.template_data.trim() ? JSON.parse(d.template_data) : undefined,
    list_id: d.list_id,
    send_rate: d.send_rate,
    smtp_group: d.smtp_group,
    send_at_local_time: d.send_at_local_time,
    ab_test_enabled: d.ab_test_enabled,
    // Sent only when the split is on. Sending an empty list with it off
    // would ask the server to store variants nothing will ever read.
    ab_variants: d.ab_test_enabled
      ? variants.map((v) => ({
          name: v.name,
          subject: v.subject || undefined,
          template_id: v.template_id || undefined,
          split_percentage: Number(v.split_percentage),
        }))
      : undefined,
  }
}

/**
 * Whether the draft can be sent.
 *
 * A split that does not total 100 leaves part of the list addressed by
 * nothing, which the server refuses - so the button says so rather than
 * the toast.
 */
export function draftIsReady(d: CampaignDraft, variants: CampaignVariant[]): boolean {
  if (!d.name.trim() || !d.from_email.trim() || !d.template_id || !d.list_id) return false
  if (!d.ab_test_enabled) return true

  const total = variants.reduce((sum, v) => sum + (Number(v.split_percentage) || 0), 0)

  return variants.length >= 2 && total === 100
}
