import api from './client'
import type { Webhook, WebhookDelivery } from './types'

export interface WebhookPayload {
  url: string
  events: string[]
  filters?: string[]
}

export const webhooksApi = {
  list: () => api.get<{ webhooks: Webhook[] }>('/webhooks/'),
  // The signing secret is generated server-side and returned exactly once.
  create: (payload: WebhookPayload) =>
    api.post<{ webhook: Webhook; secret: string }>('/webhooks/', payload),
  remove: (id: string) => api.delete(`/webhooks/${id}`),
  // Puts back a hook the dispatcher disabled after every attempt failed.
  enable: (id: string) => api.post<{ webhook: Webhook }>(`/webhooks/${id}/enable`),
  // Keyset paged: a project with email.sent subscribed writes a
  // delivery row per message, plus one per retry.
  deliveries: (id: string, params: { limit?: number; cursor?: string } = {}) =>
    api.get<{ deliveries: WebhookDelivery[]; next_cursor: string }>(`/webhooks/${id}/deliveries`, {
      params,
    }),
}
