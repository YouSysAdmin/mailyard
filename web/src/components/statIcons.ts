// The glyphs on the dashboard's stat cards.
//
// NOT layouts/icons.ts, and deliberately so. That map is the NAVIGATION
// set: 18x18 at stroke 1.5, drawn for a row of menu items. These are
// 24-viewBox at stroke 2, which is what a 20px card icon wants, and the
// two do not mix - a nav glyph placed here reads as a thinner drawing of
// the same thing rather than as the same weight.
//
// Merging the two sets is a DESIGN decision, not a refactor: it means
// redrawing seven glyphs onto the other grid and accepting a different
// weight on this page. Written down here so the next person deciding
// that knows it was noticed and left alone.
//
// Feather geometry, like the rest of the product - see layouts/icons.ts
// for the licence note that covers both.

// Two drawings carry two meanings each, and the alias is deliberate: a
// key here names what the number MEANS, not what it looks like, so a
// card reading `icon="sent"` on the inbound page would be wrong even
// though the tick is right. One constant rather than a second copy of
// the path, because two copies is how the two stop matching.
const TICK = `
  <polyline points="20 6 9 17 4 12" />
`
const SLASHED_CIRCLE = `
  <circle cx="12" cy="12" r="10" />
  <line x1="4.93" y1="4.93" x2="19.07" y2="19.07" />
`

/** Inner shapes only. The svg wrapper is StatCard's, identical for all. */
export const STAT_ICONS: Record<string, string> = {
  // Total Emails
  total: `
    <path
    d="M4 4h16c1.1 0 2 .9 2 2v12c0 1.1-.9 2-2 2H4c-1.1 0-2-.9-2-2V6c0-1.1.9-2 2-2z"
    />
    <polyline points="22,6 12,13 2,6" />
  `,
  // Sent
  sent: TICK,
  // Received - inbound mail that was accepted.
  received: TICK,
  // In Queue
  queued: `
    <circle cx="12" cy="12" r="10" />
    <polyline points="12 6 12 12 16 14" />
  `,
  // Failed
  failed: `
    <circle cx="12" cy="12" r="10" />
    <line x1="15" y1="9" x2="9" y2="15" />
    <line x1="9" y1="9" x2="15" y2="15" />
  `,
  // Suppressed
  suppressed: SLASHED_CIRCLE,
  // Rejected - inbound mail refused at RCPT, which is the same act
  // seen from the other direction.
  rejected: SLASHED_CIRCLE,
  // Open Rate
  opened: `
    <path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7-10-7-10-7z" />
    <circle cx="12" cy="12" r="3" />
  `,
  // Click Rate
  clicked: `
    <path d="M9 11.5V5a2 2 0 0 1 4 0v6" />
    <path
    d="M13 11V9.5a2 2 0 0 1 4 0V13a7 7 0 0 1-7 7h-1a7 7 0 0 1-7-7v-2a2 2 0 0 1 4 0"
    />
  `,
  // Failure Rate
  failure: `
    <path
    d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"
    />
    <line x1="12" y1="9" x2="12" y2="13" />
    <line x1="12" y1="17" x2="12.01" y2="17" />
  `,
  // Bounces
  bounced: `
    <polyline points="9 14 4 9 9 4" />
    <path d="M20 20v-7a4 4 0 0 0-4-4H4" />
  `,
  // Domains
  domains: `
    <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
    <polyline points="9 12 11 14 15 10" />
  `,
}
