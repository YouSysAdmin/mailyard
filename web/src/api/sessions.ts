import { appApi } from './client'

export interface UserSession {
  id: string
  user_id: string
  user_agent?: string
  ip?: string
  created_at: string
  last_seen_at: string
  expires_at: string
  revoked: boolean
  // Set when the sign-in came through an identity provider, absent for
  // a password or passkey one. auth_provider_name is what to show.
  auth_provider_id?: string
  auth_provider_name?: string
  // Marks the session making the request, so the UI can label it and
  // not offer it as somebody else's to kill.
  current?: boolean
}

export const sessionsApi = {
  list: () => appApi.get<{ sessions: UserSession[] }>('/auth/sessions'),
  revoke: (id: string) => appApi.delete<{ revoked: number }>(`/auth/sessions/${id}`),
  revokeOthers: () => appApi.post<{ revoked: number }>('/auth/sessions/revoke-others'),
}
