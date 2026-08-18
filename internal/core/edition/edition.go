// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package edition names which build of Mailyard this binary is.
//
// ONE place answers it, and it answers from the BUILD TAG rather than
// from config or an ldflag. A -X could relabel a community binary as
// the enterprise one without adding a line of the code that makes it
// so, which turns the honest answer this package exists to give into
// a claim anybody can make.
//
// Three things read it: the auth info endpoint, which is how the
// console learns what to offer; the relay listing, which is how a page
// explains an empty table; and config validation, which refuses to boot
// an installation configured for a feature it does not carry. Nothing
// gates on it - the gate is the absent code.
package edition
