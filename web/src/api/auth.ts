import { appApi } from './client'
import type { User } from './types'

// One identity provider as offered on the sign-in page. Deliberately
// thin: this endpoint is unauthenticated, so it carries no client
// ids, issuers, or allowlists.
export interface LoginProvider {
  name: string
  slug: string
  type: string
  start_url: string
}

export interface AuthInfo {
  // Which build the server is: 'community' or 'enterprise'. The one
  // place the console learns it, and it comes from the server because
  // this bundle is the same bundle in both - the source is public
  // either way, so nothing here can be compiled out.
  //
  // Nothing is gated on it. A page reads it only to explain an empty
  // table honestly, which is the difference between "your operator has
  // not switched this on" and "this build does not have it".
  edition?: string
  auth_disabled?: boolean
  local_enabled?: boolean
  // True when at least one provider is offered. Kept as a convenience
  // flag so the template does not have to reason about an empty array.
  oidc_enabled?: boolean
  providers?: LoginProvider[]
  password_reset_enabled?: boolean
  registration_enabled?: boolean
  // Whether the install offers passkeys at all. It says nothing about
  // whether any particular account has one - the endpoint is public,
  // so answering that would be an enumeration oracle.
  passkeys_enabled?: boolean
}

// One enrolled passkey, as the console shows it. No key material and
// no credential id: neither is any use here, and the credential id is
// a correlatable identifier with no reason to be in a page.
export interface Passkey {
  id: string
  name: string
  created_at: string
  last_used_at?: string
}

export const authApi = {
  login: (email: string, password: string, totpCode?: string) =>
    appApi.post<{ user: User }>('/auth/login', { email, password, totp_code: totpCode }),
  logout: () => appApi.post('/auth/logout'),
  // On installs with system mail the account is created unverified
  // and the response carries verification_required instead of a user.
  register: (email: string, password: string) =>
    appApi.post<{ user?: User; verification_required?: boolean; message?: string }>(
      '/auth/register',
      { email, password },
    ),
  // Redeeming the emailed link also signs the account in.
  verifyEmail: (token: string) => appApi.post<{ user: User }>('/auth/verify-email', { token }),
  verifyEmailResend: (email: string) =>
    appApi.post<{ message: string }>('/auth/verify-email/resend', { email }),
  totpSetup: () => appApi.post<{ secret: string; otpauth_url: string }>('/auth/2fa/setup'),
  changePassword: (currentPassword: string, password: string) =>
    appApi.post<{ message: string }>('/auth/password', {
      current_password: currentPassword,
      password,
    }),
  totpEnable: (code: string) =>
    appApi.post<{ totp_enabled: boolean }>('/auth/2fa/enable', { code }),
  totpDisable: (code: string) =>
    appApi.post<{ totp_enabled: boolean }>('/auth/2fa/disable', { code }),
  // Passkeys. The begin calls return the raw WebAuthn options and the
  // finish calls take the raw ceremony result, so the bodies here are
  // whatever webauthn.ts produced rather than a shape of our own.
  passkeyList: () => appApi.get<{ passkeys: Passkey[] }>('/auth/passkeys'),
  passkeyRegisterBegin: (password: string) =>
    appApi.post<{ publicKey: Record<string, unknown> }>('/auth/passkeys/register/begin', {
      password,
    }),
  passkeyRegisterFinish: (name: string, credential: unknown) =>
    appApi.post<{ passkey: Passkey }>('/auth/passkeys/register/finish', credential, {
      params: { name },
    }),
  passkeyRename: (id: string, name: string) =>
    appApi.patch<{ renamed: boolean }>(`/auth/passkeys/${id}`, { name }),
  passkeyDelete: (id: string, password: string) =>
    appApi.post<{ removed: boolean }>(`/auth/passkeys/${id}/delete`, { password }),
  passkeyLoginBegin: () =>
    appApi.post<{ publicKey: Record<string, unknown> }>('/auth/passkey/login/begin'),
  passkeyLoginFinish: (credential: unknown) =>
    appApi.post<{ user: User }>('/auth/passkey/login/finish', credential),
  me: () => appApi.get<{ user?: User; auth_disabled?: boolean }>('/auth/me'),
  info: () => appApi.get<AuthInfo>('/auth/info'),
  passwordResetRequest: (email: string) =>
    appApi.post<{ message: string }>('/auth/password-reset/request', { email }),
  passwordResetConfirm: (token: string, password: string) =>
    appApi.post<{ message: string }>('/auth/password-reset/confirm', { token, password }),
  // SSO start is a full-page navigation, not an XHR. The URL comes
  // from the provider list rather than being built here, so the path
  // is only spelled out on the server.
  ssoStartURL: (p: LoginProvider) => p.start_url,
}
