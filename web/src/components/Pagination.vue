<script setup lang="ts">
// The page picker under a client-paged table.
//
// It renders nothing at all for a single page: a control offering to
// move somewhere there is nowhere to move to is noise under every
// short list in the console.
//
// Page numbers are ZERO based throughout, matching Pageable and the
// stores that produce it. Only the labels are shifted by one, at the
// point of display - converting earlier would mean two numbering
// systems in one file, which is how off-by-ones get in.
import { computed } from 'vue'
import type { Pageable } from '../composables/usePagination'

const props = defineProps<{ pageable: Pageable }>()
const emit = defineEmits<{ (e: 'page', page: number): void }>()

/** How many numbered buttons to show before falling back to a window. */
const MAX_BUTTONS = 7

const current = computed(() => props.pageable.current_page)
const lastPage = computed(() => props.pageable.total_pages - 1)

/**
 * Which page numbers get a button, ascending.
 *
 * Plain numbers, with no separator entries mixed in: where a gap
 * belongs is derivable from the numbers themselves (see the template),
 * and a list that is "numbers, except sometimes not" makes every
 * reader of it handle two cases.
 *
 * The first and last page are always reachable - they are the two
 * destinations somebody actually aims for - and the rest of the budget
 * is a window centred on where they are now.
 */
const pages = computed<number[]>(() => {
  const last = lastPage.value
  if (last < 0) return []

  // They all fit, so show them all rather than manufacturing a gap.
  if (last + 1 <= MAX_BUTTONS) {
    return Array.from({ length: last + 1 }, (_, i) => i)
  }

  // Budget minus the first and last, which are always present.
  const window = MAX_BUTTONS - 2
  let start = current.value - Math.floor((window - 1) / 2)
  // Slide the window inside the range instead of letting it hang off
  // an end, so a page near either edge still gets a full row rather
  // than a short one.
  start = Math.max(1, Math.min(start, last - window))

  const middle = Array.from({ length: window }, (_, i) => start + i)

  return [...new Set([0, ...middle, last])].sort((a, b) => a - b)
})

function go(page: number) {
  if (page !== current.value && page >= 0 && page <= lastPage.value) {
    emit('page', page)
  }
}
</script>

<template>
  <!-- A nav landmark, so the control is announced as navigation and
       can be jumped to, rather than reading as a row of loose buttons
       after the table. -->
  <nav v-if="pageable.total_pages > 1" class="pagination" aria-label="Pagination">
    <span class="pagination-info">
      Page {{ current + 1 }} of {{ pageable.total_pages }} ({{ pageable.total_elements }} total)
    </span>

    <div class="pagination-buttons">
      <button
        type="button"
        class="btn btn-secondary btn-sm"
        :disabled="current === 0"
        @click="go(current - 1)"
      >
        Previous
      </button>

      <template v-for="(page, i) in pages" :key="page">
        <!-- A gap in the sequence is what an ellipsis means, so it is
             read off the numbers rather than stored among them. -->
        <span v-if="i > 0 && page > pages[i - 1] + 1" class="pagination-ellipsis">...</span>
        <button
          type="button"
          class="btn btn-sm"
          :class="page === current ? 'btn-primary' : 'btn-secondary'"
          :aria-current="page === current ? 'page' : undefined"
          @click="go(page)"
        >
          {{ page + 1 }}
        </button>
      </template>

      <button
        type="button"
        class="btn btn-secondary btn-sm"
        :disabled="current >= lastPage"
        @click="go(current + 1)"
      >
        Next
      </button>
    </div>
  </nav>
</template>

<style scoped>
.pagination-ellipsis {
  display: inline-flex;
  align-items: center;
  padding: 0 4px;
  color: var(--text-muted);
  font-size: 13px;
}
</style>
