import api from './client'
import type { User, Project } from './types'

export interface CreateUserPayload {
  email: string
  password: string
  admin?: boolean
}

export interface UpdateUserPayload {
  password?: string
  admin?: boolean
  disabled?: boolean
  email_verified?: boolean
}

// Platform admin surface.
export const usersApi = {
  list: () => api.get<{ users: User[] }>('/admin/users/'),
  get: (id: string) => api.get<{ user: User }>(`/admin/users/${id}`),
  create: (payload: CreateUserPayload) => api.post<{ user: User }>('/admin/users/', payload),
  update: (id: string, payload: UpdateUserPayload) =>
    api.patch<{ user: User }>(`/admin/users/${id}`, payload),
  remove: (id: string) => api.delete(`/admin/users/${id}`),
  projects: (id: string) => api.get<{ projects: Project[] }>(`/admin/users/${id}/projects`),
  resetTOTP: (id: string) => api.delete<{ user: User }>(`/admin/users/${id}/2fa`),
  resetPasskeys: (id: string) => api.delete<{ removed: number }>(`/admin/users/${id}/passkeys`),
  revokeSessions: (id: string) =>
    api.post<{ revoked: number }>(`/admin/users/${id}/revoke-sessions`),
}
