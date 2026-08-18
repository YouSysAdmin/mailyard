import api from './client'

// Sender mirrors internal/models/sender/sender.go. Addresses can only
// be registered for domains verified by the project and the console
// offers them in every From selector.
export interface Sender {
  id: string
  project_id: string
  created_by?: string
  email: string
  name?: string
  created_at: string
}

export const sendersApi = {
  list: () => api.get<{ senders: Sender[] }>('/senders/'),
  // 400 when the address domain is not verified by this project,
  // 409 when the address is already registered.
  create: (payload: { email: string; name?: string }) =>
    api.post<{ sender: Sender }>('/senders/', payload),
  remove: (id: string) => api.delete(`/senders/${id}`),
}
