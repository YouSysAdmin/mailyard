import api from './client'
import type { APIKey } from './types'

export interface CreateKeyPayload {
  name: string
  permissions?: string[]
  allowed_ips?: string[]
  expires_at?: string
  // sandbox mints a key whose sends are captured into the project
  // sandbox instead of delivered. Fixed at creation - there is no
  // edit for it, so one careless change cannot turn every test into
  // a real send.
  sandbox?: boolean
}

export const apiKeysApi = {
  list: () => api.get<{ api_keys: APIKey[] }>('/api-keys/'),
  // Create returns the plaintext token exactly once.
  create: (payload: CreateKeyPayload) =>
    api.post<{ api_key: APIKey; token: string }>('/api-keys/', payload),
  revoke: (id: string) => api.post(`/api-keys/${id}/revoke`),
  remove: (id: string) => api.delete(`/api-keys/${id}`),
}
