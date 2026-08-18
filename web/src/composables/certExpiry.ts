// How long a certificate has left, said four ways.
//
// Every card on the certificates page asks this - the listener table,
// the managed store, the ACME hosts, the relay authority and the
// installation's own pair - so it lives here rather than being the one
// thing each of them has to carry a copy of.
import type { CertificateDetails } from '../api/certificates'
import { formatDate } from './formatDate'

/** Amber from here. A month is time to arrange a renewal. */
const SOON_DAYS = 30

/** Red from here. A week is time to do it today. */
const URGENT_DAYS = 7

/**
 * Days left, negative once it has gone.
 *
 * Null when the stored certificate will not parse, which is a different
 * state from expired and has to stay tellable apart: one is a date, the
 * other is a file nothing can read.
 */
export function daysLeft(d?: CertificateDetails): number | null {
  if (!d) return null

  return Math.floor((new Date(d.not_after).getTime() - Date.now()) / 86400000)
}

/** The badge class for that number. */
export function expiryClass(d?: CertificateDetails): string {
  const days = daysLeft(d)
  if (days === null) return 'badge badge-neutral'
  if (days <= URGENT_DAYS) return 'badge badge-danger'
  if (days <= SOON_DAYS) return 'badge badge-warning'

  return 'badge badge-success'
}

/** What the badge says. Short, because it sits in a narrow column. */
export function expiryLabel(d?: CertificateDetails): string {
  const days = daysLeft(d)
  if (days === null) return 'unreadable'
  if (days < 0) return 'expired'

  return `${days}d`
}

/**
 * The exact date, on hover.
 *
 * Not under the badge: printed in the cell it wrapped over four lines in
 * a column two characters wide, and the number of days is what anybody
 * actually scans for.
 */
export function expiryTitle(d?: CertificateDetails): string {
  if (!d) return 'The stored certificate will not parse'

  const when = formatDate(d.not_after)

  return (daysLeft(d) ?? 0) < 0 ? `Expired ${when}` : `Expires ${when}`
}

/** Everything within the warning window, for the strip at the top. */
export function expiringSoon<T extends { details?: CertificateDetails }>(items: T[]): T[] {
  return items.filter((c) => {
    const days = daysLeft(c.details)

    return days !== null && days <= SOON_DAYS
  })
}
