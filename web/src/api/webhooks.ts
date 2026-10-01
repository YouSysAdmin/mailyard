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
  get: (id: string) => api.get<{ webhook: Webhook }>(`/webhooks/${id}`),
  // Each field only when sent. The secret is untouched by an edit.
  update: (id: string, payload: Partial<WebhookPayload>) =>
    api.patch<{ webhook: Webhook }>(`/webhooks/${id}`, payload),
  remove: (id: string) => api.delete(`/webhooks/${id}`),
  // Puts back a hook the dispatcher disabled after every attempt failed.
  enable: (id: string) => api.post<{ webhook: Webhook }>(`/webhooks/${id}/enable`),
  disable: (id: string, reason?: string) =>
    api.post<{ webhook: Webhook }>(`/webhooks/${id}/disable`, reason ? { reason } : undefined),
  // One signed webhook.test delivery, made during the request.
  test: (id: string) => api.post<{ delivery: WebhookDelivery }>(`/webhooks/${id}/test`),
  // Keyset paged: a project with email.sent subscribed writes a
  // delivery row per message, plus one per retry.
  deliveries: (
    id: string,
    params: { status?: 'success' | 'failed'; event?: string; limit?: number; cursor?: string } = {},
  ) =>
    api.get<{ deliveries: WebhookDelivery[]; next_cursor: string }>(`/webhooks/${id}/deliveries`, {
      params,
    }),
  redeliver: (id: string, deliveryId: string) =>
    api.post<{ delivery: WebhookDelivery }>(`/webhooks/${id}/deliveries/${deliveryId}/redeliver`),
}
