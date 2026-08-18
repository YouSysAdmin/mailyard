// Writing an address with the name attached to it.
//
// The server holds itself to this already: a display name is composed in
// ONE place, smtpclient.FormatAddress, never a Sprintf at the call site,
// with a guard behind it. The console had
// drifted the way that rule exists to stop: three renderings of the same
// pair in two different shapes, `Name <email>` in the sender picker and
// on the campaign summary, `email (Name)` in the subscriber picker. Two
// pickers of an address, two forms of the same answer.
//
// `Name <email>` is the one kept: it is what mail itself looks like, and
// it is the form somebody can paste straight into a To field.

/**
 * The address, with the name in front of it when there is one.
 *
 * Empty in, empty out - a subscriber row with no address renders '' and
 * not a stray pair of angle brackets.
 */
export function formatMailbox(email: string, name?: string): string {
  const addr = (email ?? '').trim()
  const display = (name ?? '').trim()
  if (!addr) return display

  return display ? `${display} <${addr}>` : addr
}
