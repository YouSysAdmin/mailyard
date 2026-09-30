import api from './client'
import type { FilterRule, Subscriber, SubscriberList, SubscriberListMember } from './types'

export interface SubscriberListPayload {
  name?: string
  description?: string
  type?: 'static' | 'dynamic'
  filter_rules?: FilterRule[]
}

export const subscriberListsApi = {
  list: () => api.get<{ subscriber_lists: SubscriberList[] }>('/subscriber-lists/'),
  get: (id: string) =>
    api.get<{ subscriber_list: SubscriberList; member_count?: number; opted_out_count: number }>(
      `/subscriber-lists/${id}`,
    ),
  create: (payload: SubscriberListPayload) =>
    api.post<{ subscriber_list: SubscriberList }>('/subscriber-lists/', payload),
  update: (id: string, payload: SubscriberListPayload) =>
    api.patch<{ subscriber_list: SubscriberList }>(`/subscriber-lists/${id}`, payload),
  remove: (id: string) => api.delete(`/subscriber-lists/${id}`),

  listMembers: (id: string) =>
    api.get<{ members: SubscriberListMember[] }>(`/subscriber-lists/${id}/members`),
  // Who opted out, member or not - a dynamic list has opt-outs and no
  // members, and this is the only way to see them.
  listOptOuts: (id: string, params: { limit?: number; offset?: number } = {}) =>
    api.get<{ opt_outs: SubscriberListMember[] }>(`/subscriber-lists/${id}/opt-outs`, { params }),
  addMember: (id: string, payload: { subscriber_id?: string; email?: string }) =>
    api.post(`/subscriber-lists/${id}/members`, payload),
  removeMember: (id: string, subscriberId: string) =>
    api.delete(`/subscriber-lists/${id}/members/${subscriberId}`),

  unsubscribe: (id: string, email: string, reason?: string) =>
    api.post(`/subscriber-lists/${id}/unsubscribe`, { email, reason }),
  resubscribe: (id: string, email: string) =>
    api.post(`/subscriber-lists/${id}/resubscribe`, { email }),
  previewSegment: (payload: { filter_rules: FilterRule[] }) =>
    api.post<{ matched: number; sample: Subscriber[] }>(
      '/subscriber-lists/preview-segment',
      payload,
    ),
}
