import api from './client'
import type {
  PermissionResource,
  Project,
  ProjectInvitation,
  ProjectMember,
  ProjectRole,
} from './types'

export interface ProjectPayload {
  name?: string
  description?: string
  default_language?: string
  strict_senders?: boolean
  track_opens?: boolean
  track_clicks?: boolean
  bounce_address?: string
  alert_email?: string
  sandbox_retention_days?: number
}

// What the caller may do in one project. The list carries it per row,
// keyed by project id, because gating a row by the ACTIVE project's
// permissions judges one project by another's role.
export interface ProjectAccess {
  owner: boolean
  permissions: string[]
}

export const projectApi = {
  list: () =>
    api.get<{
      projects: Project[]
      access: Record<string, ProjectAccess>
      // Whether this caller may create ANOTHER project - a platform
      // setting, off by default, so on most installations only a
      // platform admin can. Answered by the server, never worked out
      // here: the console offering a button the API refuses is the
      // failure this exists to avoid.
      can_create: boolean
    }>('/projects/'),
  get: (id: string) =>
    api.get<{ project: Project; owner: boolean; permissions: string[] }>(`/projects/${id}`),
  create: (payload: ProjectPayload) => api.post<{ project: Project }>('/projects/', payload),
  update: (id: string, payload: ProjectPayload) =>
    api.patch<{ project: Project }>(`/projects/${id}`, payload),
  remove: (id: string) => api.delete(`/projects/${id}`),

  listMembers: (id: string) => api.get<{ members: ProjectMember[] }>(`/projects/${id}/members`),

  listRoles: (id: string) => api.get<{ roles: ProjectRole[] }>(`/projects/${id}/roles`),
  createRole: (id: string, payload: RolePayload) =>
    api.post<{ role: ProjectRole }>(`/projects/${id}/roles`, payload),
  updateRole: (id: string, roleId: string, payload: RolePayload) =>
    api.patch<{ role: ProjectRole }>(`/projects/${id}/roles/${roleId}`, payload),
  removeRole: (id: string, roleId: string) => api.delete(`/projects/${id}/roles/${roleId}`),
  // Empty role_id clears the default, which leaves roleless members
  // reaching nothing.
  setDefaultRole: (id: string, roleId: string) =>
    api.put<{ project: Project }>(`/projects/${id}/default-role`, { role_id: roleId }),
  addMember: (id: string, payload: { email: string; role_id?: string }) =>
    api.post<{ member: ProjectMember }>(`/projects/${id}/members`, payload),
  // role_id: '' clears the member's own role, dropping them to the
  // project default. Owner may only be sent by an owner.
  updateMember: (id: string, userId: string, payload: { role_id?: string; owner?: boolean }) =>
    api.patch<{ member: ProjectMember }>(`/projects/${id}/members/${userId}`, payload),
  removeMember: (id: string, userId: string) => api.delete(`/projects/${id}/members/${userId}`),

  listInvitations: (id: string) =>
    api.get<{ invitations: ProjectInvitation[] }>(`/projects/${id}/invitations`),
  // The backend returns the invitation token exactly once on create,
  // plus emailed=true when system mail delivered the link as well,
  // so the UI can surface a copyable invite link.
  createInvitation: (id: string, payload: { email: string; role_id?: string }) =>
    api.post<{ invitation: ProjectInvitation; token: string; emailed?: boolean }>(
      `/projects/${id}/invitations`,
      payload,
    ),
  deleteInvitation: (id: string, invId: string) =>
    api.delete(`/projects/${id}/invitations/${invId}`),

  acceptInvitation: (token: string) =>
    api.post<{ project: Project; member: ProjectMember }>(`/invitations/${token}/accept`),
  declineInvitation: (token: string) =>
    api.post<{ declined: boolean }>(`/invitations/${token}/decline`),
}

export interface RolePayload {
  name: string
  description?: string
  permissions: string[]
}

// The permission catalogue - static, served by the binary that
// enforces it.
export const permissionsApi = {
  catalog: () => api.get<{ resources: PermissionResource[]; actions: string[] }>('/permissions'),
}
