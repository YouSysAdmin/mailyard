import { computed, ref, type Ref } from 'vue'

export interface Pageable {
  current_page: number
  size: number
  total_pages: number
  total_elements: number
  empty: boolean
}

// The new API returns full arrays (emails use a keyset cursor instead),
// so pagination is a client-side slice over the loaded list.
export function useClientPager<T>(items: Ref<T[]>, size = 20) {
  const page = ref(0)

  const totalPages = computed(() => Math.max(1, Math.ceil(items.value.length / size)))

  const pageable = computed<Pageable>(() => ({
    current_page: Math.min(page.value, totalPages.value - 1),
    size,
    total_pages: totalPages.value,
    total_elements: items.value.length,
    empty: items.value.length === 0,
  }))

  const pageItems = computed(() => {
    const p = pageable.value.current_page
    return items.value.slice(p * size, (p + 1) * size)
  })

  function goToPage(p: number) {
    page.value = Math.min(Math.max(0, p), totalPages.value - 1)
  }

  return { page, pageable, pageItems, goToPage }
}

// Keyset paging, for the lists that grow with sending volume:
// suppressions, bounces, webhook deliveries.
//
// useClientPager above fetches everything and slices it in the
// browser. That is right for templates or api keys, where a project
// has dozens of rows, and wrong for a table that gains a row per
// message - it was hiding a hard LIMIT 500 on the server behind a
// pager that looked complete.
//
// The server hands back an opaque cursor rather than a page number,
// so this only goes forward. That is what reading a log looks like,
// and it is the price of every page costing the same regardless of
// depth.
export function useKeysetPager<T>(
  fetchPage: (cursor: string) => Promise<{ items: T[]; next: string }>,
) {
  const items = ref<T[]>([]) as Ref<T[]>
  const loading = ref(true)
  const loadingMore = ref(false)
  const cursor = ref('')
  const hasMore = computed(() => cursor.value !== '')

  // reload starts over. Any filter or search change has to go through
  // it: a cursor names a position in one ordering, so carrying it
  // across a filter change would resume in the middle of a result set
  // that no longer exists.
  async function reload() {
    loading.value = true
    try {
      const res = await fetchPage('')
      items.value = res.items
      cursor.value = res.next
    } finally {
      loading.value = false
    }
  }

  async function loadMore() {
    if (!cursor.value || loadingMore.value) return
    loadingMore.value = true
    try {
      const res = await fetchPage(cursor.value)
      items.value = items.value.concat(res.items)
      cursor.value = res.next
    } finally {
      loadingMore.value = false
    }
  }

  return { items, loading, loadingMore, hasMore, reload, loadMore }
}
