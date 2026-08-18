import api from './client'
import type { Language } from './types'

export const languagesApi = {
  list: () => api.get<{ languages: Language[] }>('/languages/'),
  create: (payload: { code: string; name: string; is_default?: boolean }) =>
    api.post<{ language: Language }>('/languages/', payload),
  update: (id: string, payload: { code: string; name: string; is_default?: boolean }) =>
    api.put<{ language: Language }>(`/languages/${id}`, payload),
  remove: (id: string) => api.delete(`/languages/${id}`),
}
