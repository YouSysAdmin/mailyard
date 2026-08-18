import api from './client'
import type { Bounce } from './types'

// Keyset paging and server-side filtering. Doing either in the browser
// means a hard LIMIT 500, which on real sending volume is under an hour
// of history with no way to reach the rest.
export interface BounceListParams {
  /** hard, soft or complaint. */
  type?: string
  /** Matches the START of a recipient address. */
  search?: string
  limit?: number
  cursor?: string
}

export const bouncesApi = {
  list: (params: BounceListParams = {}) =>
    api.get<{ bounces: Bounce[]; next_cursor: string }>('/bounces/', { params }),
  // By address, not by row: a mailbox that did not exist yet has a
  // report per attempt, and the operator's question is about the person.
  //
  // This clears the HISTORY only. What blocks delivery is the
  // suppression the report created - see suppressionsApi.remove, which
  // the page calls alongside this one.
  remove: (email: string) => api.delete<{ deleted: number }>('/bounces/', { params: { email } }),
}
