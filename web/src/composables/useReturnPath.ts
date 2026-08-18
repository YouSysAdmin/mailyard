/**
 * Resolves the ?next= return path that gated pages outside the SPA
 * (currently /docs) attach when they bounce a reader to the login
 * form.
 *
 * The value arrives in a URL the reader can edit, so it is an open
 * redirect unless it is checked. The obvious check - starts with "/"
 * but not "//" - is not enough: browsers normalise a backslash to a
 * forward slash while parsing, so "/\evil.example" becomes
 * "//evil.example" and lands on another origin. Rather than grow the
 * list of prefixes to reject, hand the string to the URL parser the
 * browser will actually use and compare the origin it produces.
 */
export function safeReturnPath(next: unknown): string | null {
  if (typeof next !== 'string' || next === '') return null
  let url: URL
  try {
    url = new URL(next, window.location.origin)
  } catch {
    return null
  }
  if (url.origin !== window.location.origin) return null
  // Rebuild from the parsed parts rather than returning the input, so
  // whatever the caller navigates to is the thing that was checked.
  return url.pathname + url.search + url.hash
}
