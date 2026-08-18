import api from './client'
import type { Stylesheet } from './types'

export const stylesheetsApi = {
  list: () => api.get<{ stylesheets: Stylesheet[] }>('/stylesheets/'),
  get: (id: string) => api.get<{ stylesheet: Stylesheet }>(`/stylesheets/${id}`),
  create: (payload: { name: string; css: string }) =>
    api.post<{ stylesheet: Stylesheet }>('/stylesheets/', payload),
  update: (id: string, payload: { name: string; css: string }) =>
    api.put<{ stylesheet: Stylesheet }>(`/stylesheets/${id}`, payload),
  remove: (id: string) => api.delete(`/stylesheets/${id}`),
}
