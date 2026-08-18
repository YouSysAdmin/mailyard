import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { authApi } from '../api/auth'
import { PROJECT_KEY } from '../api/client'
import type { User } from '../api/types'

// The session lives in an HttpOnly cookie the browser attaches to every
// same-origin request, so there is no token to hold here. `mailyard_user`
// caches the profile only so a reload can render before /auth/me
// resolves - it is not a credential. If the cookie is missing or expired
// the first API call 401s and the response interceptor in api/client.ts
// sends us to the login page.
const USER_KEY = 'mailyard_user'

// A corrupt cache is not a reason to lose the console.
//
// This ran unguarded during store setup, so a half-written value (an
// interrupted write, another tab, an extension) threw inside Pinia
// initialisation and blanked the whole app on every load until storage
// was cleared by hand. It is a cache of a profile - the cookie is the
// session - so the right answer to unreadable is to drop it and let the
// next request refill it.
//
// A cache written by an OLDER BUILD is the same problem wearing a
// different hat, and it does not throw. `admin` replaced `role` plus
// `super_user`, so a profile cached before that upgrade parses cleanly
// and answers `undefined` to the one question that decides whether the
// platform-admin half of the console exists. Found on a browser holding
// a profile from a build two weeks older: the account was an
// administrator, the server said so on every request, and the console
// showed no Admin section at all - permanently, because the profile is
// re-fetched only when there is NONE.
//
// So the shape is checked rather than assumed. An object that does not
// carry what this build reads is not a profile this build can use.
function cachedUser(): User | null {
  let parsed: unknown
  try {
    parsed = JSON.parse(localStorage.getItem(USER_KEY) || 'null')
  } catch {
    localStorage.removeItem(USER_KEY)

    return null
  }

  if (!parsed || typeof parsed !== 'object') return null

  const u = parsed as Partial<User>
  if (typeof u.id !== 'string' || typeof u.email !== 'string' || typeof u.admin !== 'boolean') {
    localStorage.removeItem(USER_KEY)

    return null
  }

  return u as User
}

export const useAuthStore = defineStore('auth', () => {
  const user = ref<User | null>(cachedUser())
  const authDisabled = ref(false)

  // Which build the server is. Empty until ensureEdition has answered,
  // and the pages that read it treat empty as "do not claim anything
  // yet" rather than as community - guessing wrong here means telling
  // an operator their feature is missing while it loads.
  const edition = ref('')
  // Only ever asked as "should an enterprise-only item say so". Written
  // this way round on purpose: unknown is not community, so a slow or
  // failed /auth/info leaves the nav plain rather than labelling
  // features as missing on an install that has them.
  const isCommunity = computed(() => edition.value === 'community')

  const isAuthenticated = computed(() => authDisabled.value || !!user.value)
  const isAdmin = computed(() => authDisabled.value || user.value?.admin === true)

  // Fetched once per document. The answer cannot change under a running
  // console - it is compiled into the binary being talked to - so a
  // second call would be a request per navigation for a constant.
  //
  // Not cached in localStorage either: the one thing that does change it
  // is the operator swapping the binary, and a stale value there would
  // outlive the upgrade and be read as the product lying.
  let editionPending: Promise<void> | null = null

  function ensureEdition(): Promise<void> {
    if (edition.value) return Promise.resolve()

    if (!editionPending) {
      editionPending = authApi
        .info()
        .then((res) => {
          edition.value = res.data.edition || ''
        })
        .catch(() => {
          // Unreachable info is not a verdict about the edition. Leave
          // it empty and let the next navigation ask again.
          editionPending = null
        })
    }

    return editionPending
  }

  async function login(email: string, password: string, totpCode?: string) {
    const res = await authApi.login(email, password, totpCode)
    setUser(res.data.user)
  }

  function setUser(u: User) {
    user.value = u
    localStorage.setItem(USER_KEY, JSON.stringify(u))
  }

  // clearSession drops client state only. The 401 interceptor relies on
  // this: calling logout() there would fire another request that 401s
  // in turn.
  function clearSession() {
    user.value = null
    localStorage.removeItem(USER_KEY)
    localStorage.removeItem(PROJECT_KEY)
  }

  async function logout() {
    try {
      await authApi.logout()
    } catch {
      /* the session is gone either way */
    }
    clearSession()
  }

  /** Ask the server who this is, and store the answer. */
  async function loadUser() {
    const res = await authApi.me()
    if (res.data.auth_disabled) {
      authDisabled.value = true

      return
    }

    if (res.data.user) setUser(res.data.user)
  }

  // The gate before a protected page, where there is nothing cached to
  // fall back on: a failure here means the console cannot say who this
  // is, so it must not carry on as though it could.
  async function fetchUser() {
    try {
      await loadUser()
    } catch {
      clearSession()
    }
  }

  // Once per document, like ensureEdition beside it. A profile refresh
  // per navigation would be a request for something that changes about
  // as often as the person signs in.
  let refreshPending: Promise<void> | null = null

  /**
   * Bring the cached profile up to date, at most once per document.
   *
   * A failure is SWALLOWED, deliberately, where the gate above clears
   * the session on one. There is a cached profile here and the page is
   * already rendered from it, so a flaky network would otherwise sign
   * somebody out mid-session for no reason. A session that is genuinely
   * over answers 401, and the response interceptor already turns that
   * into the login page from whichever request meets it first.
   */
  function refreshOnce(): Promise<void> {
    if (!refreshPending) {
      refreshPending = loadUser().catch(() => {
        // The next document asks again.
        refreshPending = null
      })
    }

    return refreshPending
  }

  return {
    user,
    authDisabled,
    edition,
    isCommunity,
    ensureEdition,
    isAuthenticated,
    isAdmin,
    login,
    logout,
    clearSession,
    fetchUser,
    refreshOnce,
    setUser,
  }
})
