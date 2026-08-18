import api from './client'
import type { Subscriber } from './types'

export interface SubscriberPayload {
  email?: string
  name?: string
  status?: string
  custom_fields?: Record<string, unknown>
  timezone?: string
  language?: string
}

export interface ImportResult {
  created: number
  updated: number
  skipped: number
  errors: { index: number; email: string; error: string }[]
}

export const subscribersApi = {
  list: (params: { status?: string; q?: string; limit?: number; offset?: number } = {}) =>
    api.get<{ subscribers: Subscriber[]; total: number }>('/subscribers/', { params }),
  get: (id: string) => api.get<{ subscriber: Subscriber }>(`/subscribers/${id}`),
  create: (payload: SubscriberPayload) =>
    api.post<{ subscriber: Subscriber }>('/subscribers/', payload),
  update: (id: string, payload: SubscriberPayload) =>
    api.patch<{ subscriber: Subscriber }>(`/subscribers/${id}`, payload),
  remove: (id: string) => api.delete(`/subscribers/${id}`),
  importJSON: (payload: { subscribers: SubscriberPayload[] }) =>
    api.post<ImportResult>('/subscribers/import', payload),
  importCSV: (csv: string) =>
    api.post<ImportResult>('/subscribers/import/csv', csv, {
      headers: { 'Content-Type': 'text/csv' },
    }),
}
