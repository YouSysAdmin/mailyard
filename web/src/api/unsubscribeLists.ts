import api from './client'

// An unsubscribe list is a transactional opt-out scope. It has no
// membership - `suppressed_count` is how many addresses have opted
// out of it, read from the suppression list.
export interface UnsubscribeList {
  id: string
  project_id: string
  name: string
  public_name?: string
  description?: string
  active: boolean
  suppressed_count: number
  created_at: string
  updated_at?: string
}

export interface UnsubscribeListPayload {
  name?: string
  public_name?: string
  description?: string
  active?: boolean
}

export const unsubscribeListsApi = {
  list: () => api.get<{ unsubscribe_lists: UnsubscribeList[] }>('/unsubscribe-lists/'),
  get: (id: string) => api.get<{ unsubscribe_list: UnsubscribeList }>(`/unsubscribe-lists/${id}`),
  create: (payload: UnsubscribeListPayload) =>
    api.post<{ unsubscribe_list: UnsubscribeList }>('/unsubscribe-lists/', payload),
  update: (id: string, payload: UnsubscribeListPayload) =>
    api.patch<{ unsubscribe_list: UnsubscribeList }>(`/unsubscribe-lists/${id}`, payload),
  remove: (id: string) => api.delete(`/unsubscribe-lists/${id}`),
}
