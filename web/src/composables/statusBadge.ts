// WHICH COLOUR A STATUS IS, decided once, so a status added to the API
// is added in one place and the same badge reads the same on every
// page.
//
// The vocabularies are kept APART rather than merged into one table,
// because they disagree: a campaign message that is `pending` is neutral
// (it is simply not its turn yet) while an email that is `pending` is a
// warning (it should have moved). Merging them would have to pick one.

export type StatusScope =
  'email' | 'campaign' | 'campaignMessage' | 'subscriber' | 'inbound' | 'webhook'

const scopes: Record<StatusScope, Record<string, string>> = {
  email: {
    sent: 'badge-success',
    failed: 'badge-danger',
    pending: 'badge-warning',
    queued: 'badge-info',
    processing: 'badge-warning',
    suppressed: 'badge-secondary',
    scheduled: 'badge-info',
  },
  campaign: {
    draft: 'badge-neutral',
    scheduled: 'badge-info',
    sending: 'badge-info',
    sent: 'badge-success',
    paused: 'badge-warning',
    cancelled: 'badge-danger',
  },
  campaignMessage: {
    pending: 'badge-neutral',
    queued: 'badge-info',
    sent: 'badge-success',
    failed: 'badge-danger',
    skipped: 'badge-warning',
  },
  subscriber: {
    subscribed: 'badge-success',
    unsubscribed: 'badge-neutral',
    bounced: 'badge-warning',
    complained: 'badge-danger',
  },
  inbound: {
    received: 'badge-success',
    rejected: 'badge-warning',
    failed: 'badge-danger',
  },
  webhook: {
    active: 'badge-success',
    disabled: 'badge-danger',
  },
}

// An unknown status is a bare badge rather than a guess: a status this
// build has never heard of is one the server added, and colouring it as
// success or danger would be inventing a meaning for it.
export function statusBadgeClass(status: string, scope: StatusScope): string {
  const variant = scopes[scope][status]

  return variant ? `badge ${variant}` : 'badge'
}
