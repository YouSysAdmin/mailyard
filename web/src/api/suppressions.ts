import api from './client'
import type { Suppression } from './types'

// Keyset paging, not page numbers. This table gains a row per
// permanently rejected message and is never pruned, so an offset
// deep into it costs the database everything it skips. Pass
// next_cursor back to get the next page, and an absent next_cursor
// means there is nothing more.
//
// There is no total for the same reason - COUNT(*) here is a full
// index scan to produce a number nobody acts on.
export interface SuppressionListParams {
  kind?: string
  /** Matches the START of an address. */
  search?: string
  limit?: number
  cursor?: string
}

export const suppressionsApi = {
  list: (params: SuppressionListParams = {}) =>
    api.get<{ suppressions: Suppression[]; next_cursor: string }>('/suppressions/', { params }),
  create: (payload: { email: string; kind?: string; reason?: string }) =>
    api.post<{ suppression: Suppression }>('/suppressions/', payload),
  // Delete takes the address as a query param so it works for any kind.
  //
  // SCOPED by listId. Without one it lifts the global block only: the
  // rows are unique on (project, email, list), and an address can be
  // globally blocked AND separately opted out of named lists. Pass the
  // row's unsubscribe_list_id to remove exactly the row on screen.
  remove: (email: string, listId?: string) =>
    api.delete('/suppressions/', { params: { email, list_id: listId || undefined } }),
}
