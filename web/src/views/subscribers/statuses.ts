// What a subscriber's status can be.
//
// One list because it was written out twice - as the filter on the list
// page and as the select on the detail page - and the two are the same
// closed set the server validates against. A fifth status added on the
// server has one place to land here.
import type { SubscriberStatus } from '../../api/types'

/** Every status, in the order a reader meets them. */
export const SUBSCRIBER_STATUSES: { value: SubscriberStatus; label: string }[] = [
  { value: 'subscribed', label: 'Subscribed' },
  { value: 'unsubscribed', label: 'Unsubscribed' },
  { value: 'bounced', label: 'Bounced' },
  { value: 'complained', label: 'Complained' },
]
