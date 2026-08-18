<script setup lang="ts">
// One row in a mail client's left pane.
//
// The console has two of those - the sandbox and the inbound log - and
// they are the same act: reading down a list of mail that arrived. The
// row was written twice and the copies had already drifted in the ways
// copies do: one clamped the subject to two lines and the other
// truncated it to one, one offered a delete on hover and the other made
// you open the message first.
//
// Slotted where they genuinely differ. Inbound carries a status badge -
// rejected and failed are why somebody opened that page - and the
// sandbox has no statuses at all, because nothing there was delivered.
defineProps<{
  subject?: string
  /** Already worded by the page: a capture is hours old, not years. */
  time: string
  sender?: string
  recipients: string[]
  selected?: boolean
  /** Offers the hover delete. False where the caller may not delete. */
  deletable?: boolean
  /** Disables it while its request is in flight. */
  deleting?: boolean
  /** What the delete control says it will do, for the tooltip. */
  deleteLabel?: string
}>()

const emit = defineEmits<{
  (e: 'open'): void
  (e: 'delete'): void
}>()

/** At most two addresses, then a count. A fan-out row stays one line. */
function summarise(to: string[]): string {
  if (!to || to.length === 0) return '-'
  if (to.length <= 2) return to.join(', ')

  return `${to[0]} and ${to.length - 1} more`
}
</script>

<template>
  <!-- A wrapper, NOT a button. The delete control is a button of its own
       and nesting one inside another is markup no browser keeps - it
       hoists the inner one out of the outer, which loses both the row
       and the click. -->
  <div class="list-row" :class="{ selected }">
    <button class="list-open" @click="emit('open')">
      <span class="list-top">
        <span class="list-subject">{{ subject || '(no subject)' }}</span>
        <span class="list-time">{{ time }}</span>
      </span>
      <span class="list-to" :title="recipients.join(', ')">to: {{ summarise(recipients) }}</span>
      <span class="list-bottom">
        <span class="list-from">{{ sender || '(no sender)' }}</span>
        <slot name="badge" />
      </span>
    </button>

    <!-- An icon, not the word. At list width the label pushed the row's
         own text out of the way, and forty of them read as a column of
         warnings rather than as a per-row action. Drawn to the console's
         own geometry: 18 viewBox, stroke 1.5, currentColor, round caps. -->
    <button
      v-if="deletable"
      class="list-delete"
      :disabled="deleting"
      :title="deleteLabel ?? 'Delete'"
      :aria-label="deleteLabel ?? 'Delete'"
      @click.stop="emit('delete')"
    >
      <svg
        width="16"
        height="16"
        viewBox="0 0 18 18"
        fill="none"
        stroke="currentColor"
        stroke-width="1.5"
        stroke-linecap="round"
        stroke-linejoin="round"
      >
        <path d="M2.75 4.5h12.5M7 4.5V3.25h4V4.5M4.5 4.5l.75 9.25h7.5l.75-9.25" />
        <path d="M7.5 7.25v3.75M10.5 7.25v3.75" />
      </svg>
    </button>
  </div>
</template>

<style scoped>
.list-row {
  position: relative;
  border-bottom: 1px solid var(--border-primary);
  border-left: 2px solid transparent;
}

.list-open {
  display: flex;
  flex-direction: column;
  gap: 2px;
  width: 100%;
  padding: 10px 12px;
  border: none;
  background: none;
  text-align: left;
  cursor: pointer;
  font: inherit;
  color: inherit;
}

.list-row:hover {
  background: var(--bg-secondary);
}

.list-row.selected {
  border-left-color: var(--primary-500);
  background: var(--bg-secondary);
}

.list-top {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 8px;
}

.list-subject {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary);
  /* Two lines at most: a long subject otherwise makes one row as tall
     as three and the list stops being scannable. The whole subject is
     at the top of the reader beside it. */
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.list-time {
  flex-shrink: 0;
  font-size: 11px;
  color: var(--text-tertiary);
}

.list-to,
.list-from {
  font-size: 12px;
  color: var(--text-secondary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* The sender and whatever the page badges the row with, on one line.
   min-width:0 so the address truncates instead of pushing the badge
   under the hover delete. */
.list-bottom {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  min-width: 0;
}

/* Revealed on hover, so forty rows are not forty buttons. It stays
   reachable by keyboard: focus-within counts as hover here. */
.list-delete {
  position: absolute;
  right: 8px;
  bottom: 8px;
  display: none;
  padding: 5px;
  border: 1px solid var(--border-primary);
  border-radius: var(--radius-sm);
  background: var(--bg-primary);
  color: var(--text-tertiary);
  cursor: pointer;
}

.list-delete:hover:not(:disabled) {
  border-color: var(--danger-600);
  color: var(--danger-600);
}

.list-delete:disabled {
  cursor: default;
  opacity: 0.5;
}

.list-row:hover .list-delete,
.list-row:focus-within .list-delete {
  display: inline-flex;
}
</style>
