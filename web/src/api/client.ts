import axios from 'axios'
import { isLeaving, sessionExpired } from '../composables/session'

// Two base paths, because the server has two surfaces and they are
// split by what an operation IS rather than by who calls it.
//
//   api     -> /api/v1, the product surface: templates, campaigns,
//              domains, everything an integration could also want.
//   appApi  -> /app/api, the console's own: sign-in ceremonies,
//              session and passkey management, the event stream.
//              Nothing here is usable remotely.
//
// The session travels as an HttpOnly cookie the browser attaches to
// every same-origin request, so both work with no token handling here.
const api = axios.create({
  baseURL: '/api/v1',
  headers: {
    'Content-Type': 'application/json',
  },
  withCredentials: true,
})

// The console's own api. Same interceptors - see below, they are
// installed on both.
export const appApi = axios.create({
  baseURL: '/app/api',
  headers: {
    'Content-Type': 'application/json',
  },
  withCredentials: true,
})

// The active project is injected on every request. Stored as a plain
// localStorage key so the interceptor works without importing the pinia
// store (which would create an import cycle client -> store -> client).
export const PROJECT_KEY = 'mailyard_project_id'

// Installed on both clients. The project header is harmless where it
// is not read, and the session-expiry redirect has to fire wherever
// the 401 arrives - a console page that only talks to /app/api would
// otherwise sit there showing stale data.
for (const client of [api, appApi]) {
  client.interceptors.request.use((config) => {
    const stored = localStorage.getItem(PROJECT_KEY)
    if (stored) {
      config.headers['X-Mailyard-Project-Id'] = stored
    }
    return config
  })

  client.interceptors.response.use(
    (response) => response,
    (error) => {
      const status = error.response?.status
      const onLogin = window.location.pathname.endsWith('/login')
      // The router guard probes /auth/me on a fresh visit and handles
      // the 401 itself - redirecting here would flash a session-expired
      // error at users who simply are not signed in yet.
      const isMeProbe = error.config?.url?.endsWith('/auth/me')
      // A deliberate sign-out is not an expired session, and the 401s it
      // produces are the requests it interrupted - see isLeaving().
      if (status === 401 && !onLogin && !isMeProbe && !isLeaving()) {
        // Drop client state only - calling logout() here would fire
        // another request that 401s in turn.
        localStorage.removeItem('mailyard_user')
        localStorage.removeItem(PROJECT_KEY)
        sessionExpired(apiErrorMessage(error, 'Your session has expired'))
      }
      return Promise.reject(error)
    },
  )
}

// browserURL builds a link the BROWSER follows directly - a download,
// a raw message view - rather than one axios fetches.
//
// Those requests carry the session cookie but not the project header,
// because a window.location navigation cannot set headers. The server
// accepts ?project_id= for exactly this case, and without it the
// request resolves the caller's DEFAULT project - which is the right
// one only while that happens to be the project they are looking at.
// A member of two projects downloading an attachment from the other
// one got a 404 and no explanation.
export function browserURL(path: string): string {
  const proj = localStorage.getItem(PROJECT_KEY)
  const url = `/api/v1${path}`
  if (!proj) return url
  return `${url}${url.includes('?') ? '&' : '?'}project_id=${encodeURIComponent(proj)}`
}

// The backend error envelope is {"error": "message", "fields": [...]}.
export function apiErrorMessage(err: unknown, fallback = 'Request failed'): string {
  const e = err as { response?: { data?: { error?: string } }; message?: string }
  return e?.response?.data?.error || e?.message || fallback
}

export default api
