// Turning a received message into the quotation above a reply.
//
// Its own file because it is the only piece of the reader that is worth
// reading on its own: everything else there is markup, and this is a
// decision about what a reply looks like.
import { formatDate } from '../../composables/formatDate'
import type { InboundEmail } from '../../api/inbound'

/** Lines of the original kept in the quotation. */
const MAX_LINES = 200

/**
 * The original rendered as a quotation, or '' when there is nothing to
 * quote.
 *
 * TEXT ONLY. An HTML-only message could be stripped down to something
 * text-shaped, but a bad conversion quotes the sender as saying
 * something they did not - and a reply with no quote is merely less
 * helpful, which is the better failure.
 *
 * Bounded, because a long thread quoted in full would push the reply out
 * of the compose box and grow without limit with each round trip.
 */
export function quoteForReply(src: InboundEmail): string {
  const body = (src.text_body ?? '').trim()
  if (!body) return ''

  const when = formatDate(src.received_at, '')
  const attribution = when ? `On ${when}, ${src.sender} wrote:` : `${src.sender} wrote:`

  const lines = body.split(/\r?\n/)
  const kept = lines.slice(0, MAX_LINES).map((l) => (l ? `> ${l}` : '>'))
  if (lines.length > MAX_LINES) kept.push('> [...]')

  return `${attribution}\n${kept.join('\n')}`
}
