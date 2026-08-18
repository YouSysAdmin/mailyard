// Entering or leaving a session crosses a DOCUMENT boundary.
//
// A `router.push` would keep the same document alive, and with it every
// Pinia store. Two of those hold the signed-in person's world: the
// project list, the active project, and the permissions it was resolved
// for. Sign out as an administrator and back in as somebody else and you
// get the administrator's projects, can() answering out of their
// permissions, and requests for a project the new account is not a
// member of - which arrives as a screen full of failed loads.
//
// The project store also clears itself when the account id changes, and
// both are deliberate. That one keeps the state model honest. This one
// makes it impossible to get wrong for whatever state we add next, since
// a fresh document cannot carry anything stale.

import { safeReturnPath } from './useReturnPath'

// consoleBase is where the SPA is mounted - the same '/app/' vite is
// configured with and the router is created with. Read from the build
// rather than written a third time.
const consoleBase = import.meta.env.BASE_URL || '/app/'

// leaving is true from the moment somebody presses Logout.
//
// Clearing the cookie server-side makes every request already in flight
// answer 401, and the response interceptor turns a 401 into "Your session
// has expired" on the login page. On a deliberate sign-out that is a lie
// dressed as an error: the dashboard's six requests and the unread badge
// were enough to land on `?error=authentication%20required` every time.
// Fixed here rather than in the interceptor, because
// what the interceptor lacks is the INTENTION.
let leaving = false

export function isLeaving(): boolean {
  return leaving
}

// enterConsole starts a session. `next` is honored when it is a path on
// this origin, which is how a gated page outside the SPA (/docs) sends a
// reader back where they were going.
//
// The check is safeReturnPath and not a leading-slash test, because a
// leading-slash test is exactly what that file documents as insufficient:
// a browser normalises "/\evil.example" into "//evil.example" while
// parsing and leaves the origin. Every caller sanitises today, so this is
// the guard being in the function that NAVIGATES rather than in each of
// its callers - one of which will eventually pass a raw query parameter.
export function enterConsole(next?: string | null) {
  window.location.href = safeReturnPath(next) ?? consoleBase
}

// enterInvitation starts a session and lands on one invitation.
//
// Here rather than at the call site because this file owns the mount
// point - the caller has a token, not a URL. The token is encoded and
// the rest of the path is ours, so there is nothing a caller can steer.
export function enterInvitation(token: string) {
  window.location.href = consoleBase + 'invitations?token=' + encodeURIComponent(token)
}

// leaveConsole ends one. The cookie is already gone server-side by the
// time this runs, so there is nothing to protect - this is about the tab.
export function leaveConsole() {
  window.location.href = consoleBase + 'login'
}

// beginLeaving is called before the logout request, since that is when
// the 401s start. The tab is going either way, so a request that fails in
// this window has nothing to report.
export function beginLeaving() {
  leaving = true
}

// sessionExpired is the other way a session ends: the cookie was gone
// before the request, so the reader is told why. Separate from
// leaveConsole because the message is the whole point - and it is here so
// the console's path is written in one place.
export function sessionExpired(message: string) {
  window.location.href = consoleBase + 'login?error=' + encodeURIComponent(message)
}
