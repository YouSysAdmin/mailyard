import api from './client'

// Identity providers, managed by a platform admin.
//
// The client secret is never returned - has_secret reports whether one
// is stored. On update, omitting client_secret leaves the stored value
// alone, so the form can save without ever holding the secret.
export interface OAuthProvider {
  id: string
  name: string
  slug: string
  type: string
  client_id: string
  has_secret: boolean
  issuer: string
  auth_url: string
  token_url: string
  userinfo_url: string
  scopes: string[]
  enabled: boolean
  hidden: boolean
  auto_register: boolean
  require_email_verified: boolean
  allowed_domains: string[]
  allowed_emails: string[]
  groups_claim: string
  allowed_groups: string[]
  // usable is false when the provider is too incomplete to sign in
  // with, which is why an enabled provider may still be absent from
  // the login page.
  usable: boolean
  // callback_url is the redirect URI to register at the IdP.
  callback_url: string
  created_at: string
  updated_at: string
}

export interface OAuthProviderInput {
  name: string
  slug?: string
  type?: string
  client_id?: string
  client_secret?: string
  issuer?: string
  auth_url?: string
  token_url?: string
  userinfo_url?: string
  scopes?: string[]
  enabled?: boolean
  hidden?: boolean
  auto_register?: boolean
  require_email_verified?: boolean
  allowed_domains?: string[]
  allowed_emails?: string[]
  groups_claim?: string
  allowed_groups?: string[]
}

export interface OAuthTestResult {
  ok: boolean
  error?: string
  issuer?: string
  auth_url?: string
  token_url?: string
  discovered: boolean
  redirect_url: string
  scopes: string[]
  warnings?: string[]
}

export const oauthProvidersApi = {
  list: () => api.get<{ providers: OAuthProvider[] }>('/admin/oauth-providers/'),
  get: (id: string) => api.get<{ provider: OAuthProvider }>(`/admin/oauth-providers/${id}`),
  create: (body: OAuthProviderInput) =>
    api.post<{ provider: OAuthProvider }>('/admin/oauth-providers/', body),
  update: (id: string, body: OAuthProviderInput) =>
    api.patch<{ provider: OAuthProvider }>(`/admin/oauth-providers/${id}`, body),
  remove: (id: string) => api.delete(`/admin/oauth-providers/${id}`),
  test: (id: string) => api.post<{ test: OAuthTestResult }>(`/admin/oauth-providers/${id}/test`),
}
