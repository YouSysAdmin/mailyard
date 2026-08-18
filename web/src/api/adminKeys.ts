import api from './client'

// A platform credential. Not a project key with wider permissions -
// a different table, a different token marker (mya_ against myk_) and
// no permission list at all, because the catalogue governs what a
// member may do inside a project and none of its resources describe
// creating a user or editing a plan.
export interface AdminAPIKey {
  id: string
  created_by?: string
  name: string
  key_prefix: string
  allowed_ips: string[]
  revoked: boolean
  expires_at?: string
  last_used_at?: string
  created_at: string
}

export interface CreateAdminKeyPayload {
  name: string
  allowed_ips?: string[]
  expires_at?: string
}

export const adminKeysApi = {
  list: () => api.get<{ admin_api_keys: AdminAPIKey[] }>('/admin/api-keys/'),
  // Create returns the plaintext token exactly once.
  create: (payload: CreateAdminKeyPayload) =>
    api.post<{ admin_api_key: AdminAPIKey; token: string }>('/admin/api-keys/', payload),
  revoke: (id: string) => api.post(`/admin/api-keys/${id}/revoke`),
  remove: (id: string) => api.delete(`/admin/api-keys/${id}`),
}
