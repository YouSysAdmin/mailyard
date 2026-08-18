import api from './client'

// Contacts are written by the delivery worker, so this module is
// read-only by design - there is no create, update, or delete.
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
}
