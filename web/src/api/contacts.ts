import api from './client'

// Contacts are written by the delivery worker, so there is no create or
// update. Delete is the clean-up: one record, or everything idle since a
// date. Blocking an address is a suppression, not this.
export interface Contact {
  id: string
  project_id: string
  email: string
  name?: string
  sent_count: number
  fail_count: number
  suppressed: boolean
  last_sent_at?: string
  last_failed_at?: string
  created_at: string
  updated_at: string
}

export interface ContactPage {
  contacts: Contact[]
  total: number
  limit: number
  offset: number
}

export const contactsApi = {
  list: (params: { search?: string; limit?: number; offset?: number } = {}) =>
    api.get<ContactPage>('/contacts/', { params }),
  get: (id: string) => api.get<{ contact: Contact }>(`/contacts/${id}`),
  remove: (id: string) => api.delete(`/contacts/${id}`),
  // inactive_before is an RFC 3339 instant. The server refuses a future
  // one, since that would be every contact.
  removeInactive: (inactiveBefore: string) =>
    api.delete<{ deleted: number; inactive_before: string }>('/contacts/', {
      params: { inactive_before: inactiveBefore },
    }),
}
