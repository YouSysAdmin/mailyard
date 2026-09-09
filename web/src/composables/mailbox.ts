// Writing an address with the name attached to it.
//
// The server holds itself to this already: a display name is composed in
// ONE place, smtpclient.FormatAddress, never a Sprintf at the call site.
// The console composes it in one place too, so two pickers of an address
// cannot show two forms of the same answer.
//
// `Name <email>` is the form: it is what mail itself looks like, and it
// is what somebody can paste straight into a To field.

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
