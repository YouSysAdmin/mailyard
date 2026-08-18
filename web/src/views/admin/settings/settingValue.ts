// A platform setting is stored as TEXT and edited as whatever its
// declared type calls for. These are the two edges of that.
//
// Their own file because both the row that edits a setting and the row
// that only displays one need them, and neither owns the other.
import type { PlatformSetting } from '../../../api/settings'

/** A switch. */
export function isBool(s: PlatformSetting): boolean {
  return s.type === 'bool'
}

/**
 * A number.
 *
 * Asked separately rather than as "not a bool", so a type added to the
 * registry later renders as text instead of silently as a number - and a
 * number input DISCARDS text, which is how platform_mail_from once could
 * not be set from this page at all.
 */
export function isInt(s: PlatformSetting): boolean {
  return s.type === 'int'
}

/** Several values in one setting - acme_hosts today. */
export function isList(s: PlatformSetting): boolean {
  return s.type === 'list'
}

/**
 * A stored JSON array as one value per line, which is what a textarea
 * shows.
 *
 * Anything that will not parse is handed back VERBATIM rather than
 * blanked: if a value got in by another route, showing it is what lets
 * somebody fix it.
 */
export function linesOf(raw: string): string {
  const text = (raw ?? '').trim()
  if (!text) return ''

  try {
    const parsed = JSON.parse(text)
    if (Array.isArray(parsed)) return parsed.join('\n')
  } catch {
    // Not an array: fall through and show the raw value.
  }

  return text
}

/**
 * And back, on the way to the server.
 *
 * The wire shape stays the JSON array every reader parses. The
 * conversion lives at this edge because asking an operator for brackets,
 * quotes and commas in a single-line box is asking for a syntax error
 * whose only symptom is an empty list.
 */
export function encodeLines(text: string): string {
  const items = (text ?? '')
    .split('\n')
    .map((v) => v.trim())
    .filter((v) => v !== '')

  return items.length ? JSON.stringify(items) : ''
}

/**
 * A setting rendered as TEXT, for the ones with no control - managed
 * elsewhere, or belonging to the other edition.
 *
 * Empty says "not set" rather than showing a blank, which reads as a
 * value that failed to load.
 */
export function displayValue(s: PlatformSetting): string {
  if (isBool(s)) return s.value === 'true' ? 'On' : 'Off'

  if (isList(s)) {
    const items = linesOf(s.value)
      .split('\n')
      .filter((v) => v !== '')

    return items.length ? items.join(', ') : 'none'
  }

  return s.value || 'not set'
}
