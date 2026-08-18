import api from './client'

// In-app notifications. Addressed to the project rather than to a
// person, so read state is shared: one member clearing an alert
// clears it for everyone.
export interface Notification {
  id: string
  project_id: string
  type: string
  severity: 'info' | 'warning' | 'error'
  title: string
  body?: string
  link?: string
  read_at?: string
  created_at: string
}

export interface NotificationPage {
  notifications: Notification[] | null
  unread: number
  limit: number
  offset: number
}

export const notificationsApi = {
  list: (params: { unread?: boolean; limit?: number; offset?: number } = {}) =>
    api.get<NotificationPage>('/notifications/', { params }),
  // Split from list because the badge is polled far more often than
  // the list is opened, and this is one indexed count.
  unread: () => api.get<{ unread: number }>('/notifications/unread'),
  markRead: (id: string) => api.post(`/notifications/${id}/read`),
  markAllRead: () => api.post<{ marked: number }>('/notifications/read-all'),
  remove: (id: string) => api.delete(`/notifications/${id}`),
}
