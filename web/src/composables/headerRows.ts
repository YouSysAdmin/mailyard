// Custom message headers as the forms hold them: name/value rows.
//
// The rows are the form's shape and the object is the wire's, and this
// is the only translation between them, so every form drops an unnamed
// row the same way and refuses the same problems before the request
// goes out. HeaderEditor renders the rows.
//
// RESERVED NAMES ARE NOT MIRRORED HERE. The server refuses From,
// Subject, Message-ID and the rest with a message naming the header and
// the field to use instead, and a second copy of that list in the
// console is one that drifts.

/** One row of the editor. */
export interface HeaderRow {
  name: string
  value: string
}

/** The stored object as rows, in a stable order. */
export function headersToRows(headers: Record<string, string> | undefined | null): HeaderRow[] {
  if (!headers) return []

  return Object.keys(headers)
    .sort()
    .map((name) => ({ name, value: headers[name] ?? '' }))
}

/**
 * The rows as the object a request carries, or undefined when nothing
 * is named - so a form without headers sends the request it always did.
 *
 * A row with an empty name is dropped rather than refused: rows are
 * added by pressing a button, so an empty one at the bottom is somebody
 * in the middle of typing, not a mistake worth an error message.
 */
export function rowsToHeaders(rows: HeaderRow[]): Record<string, string> | undefined {
  const out: Record<string, string> = {}
  let named = 0
  for (const row of rows) {
    const name = row.name.trim()
    if (!name) continue

    out[name] = row.value
    named++
  }

  return named > 0 ? out : undefined
}

// RFC 5322 ftext: printable US-ASCII other than the colon. The same rule
// the server applies, so a typo is caught before the request rather
// than reported back as one.
const FIELD_NAME = /^[\x21-\x39\x3b-\x7e]+$/

// The server's caps, internal/core/mailheader.
const MAX_NAME_LEN = 128
const MAX_VALUE_LEN = 4096

/**
 * The first thing wrong with the rows, or '' when they can be sent.
 * Shown under the editor and checked by the form before it submits.
 */
export function headerRowsProblem(rows: HeaderRow[], max = 20): string {
  const seen = new Set<string>()
  let named = 0
  for (const row of rows) {
    const name = row.name.trim()
    if (!name) continue

    named++
    if (!FIELD_NAME.test(name)) {
      return `"${name}" is not a valid header name: letters, digits and punctuation, no spaces or colons`
    }

    const key = name.toLowerCase()
    if (seen.has(key)) return `"${name}" is listed twice`

    seen.add(key)
    if (name.length > MAX_NAME_LEN) return `"${name}" is too long for a header name`
    if (row.value.length > MAX_VALUE_LEN)
      return `"${name}" has a value over ${MAX_VALUE_LEN} characters`
    // Every control character but the tab, the same rule as the server.
    if (/[\x00-\x08\x0a-\x1f\x7f]/.test(row.value)) {
      return `"${name}" has a line break or control character in its value`
    }
  }

  if (named > max) return `At most ${max} headers`

  return ''
}
