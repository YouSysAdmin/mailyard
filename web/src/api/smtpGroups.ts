import api from './client'
import type { SMTPServerGroup } from './types'

export interface SMTPGroupPayload {
  name?: string
  slug?: string
  description?: string
  // make_default promotes this group. There is no way to unset the
  // flag - a project must always have exactly one default, so the
  // only legal move is handing it to another group.
  make_default?: boolean
}

export const smtpGroupApi = {
  list: () => api.get<{ smtp_server_groups: SMTPServerGroup[] }>('/smtp-server-groups/'),
  get: (id: string) => api.get<{ smtp_server_group: SMTPServerGroup }>(`/smtp-server-groups/${id}`),
  create: (payload: SMTPGroupPayload) =>
    api.post<{ smtp_server_group: SMTPServerGroup }>('/smtp-server-groups/', payload),
  update: (id: string, payload: SMTPGroupPayload) =>
    api.patch<{ smtp_server_group: SMTPServerGroup }>(`/smtp-server-groups/${id}`, payload),
  remove: (id: string) => api.delete(`/smtp-server-groups/${id}`),
}
