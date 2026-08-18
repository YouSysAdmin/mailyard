<script setup lang="ts">
// The activity bell and the panel behind it.
//
// Its own component because it is a popover with its own data, its own
// polling and its own idea of when it is open - none of which the bar
// around it has any reason to hold. The feed itself lives in
// useNotificationFeed, so what is here is only the presentation.
import { ref } from 'vue'
import { useNotificationFeed } from '../composables/useNotificationFeed'
import { timeAgo } from '../composables/formatDate'
import type { Notification } from '../api/notifications'

const emit = defineEmits<{
  /** A notification with a link was opened. */
  (e: 'follow', link: string): void
  /** Opened, so whatever else is open in the bar should close. */
  (e: 'opened'): void
}>()

const { unread, items, watching, start, loadPanel, markAllRead, markRead } = useNotificationFeed()

const root = ref<HTMLElement | null>(null)
const open = ref(false)

// A live notification refreshes the panel only while it is OPEN. A
// closed bell stays a COUNT - prepending to a list nobody is looking at
// grows in memory for the whole session.
start(() => {
  if (open.value) void loadPanel()
})

async function toggle() {
  open.value = !open.value
  if (!open.value) return

  emit('opened')
  await loadPanel()
}

async function pick(n: Notification) {
  await markRead(n)
  open.value = false
  if (n.link) emit('follow', n.link)
}

/**
 * Close when the click landed outside.
 *
 * Asked of the ELEMENT rather than of a class name, so renaming a class
 * while restyling cannot leave the panel unable to close - which is the
 * kind of break that looks like a Vue problem and is not.
 */
function closeIfOutside(e: MouseEvent) {
  if (!root.value?.contains(e.target as Node)) open.value = false
}

defineExpose({ close: () => (open.value = false), closeIfOutside })
</script>

<template>
  <!-- Silent where the caller may not read notifications: without the
       permission every poll is a 403 and the stream is refused, all
       swallowed, so the page looks fine while the browser console fills
       with failures for an endpoint this person is not meant to know
       exists. -->
  <div v-if="watching" ref="root" class="bell-wrap">
    <button
      class="bell"
      :class="{ ringing: unread > 0 }"
      :title="unread > 0 ? `${unread} unread` : 'Notifications'"
      @click="toggle"
    >
      <svg
        width="18"
        height="18"
        viewBox="0 0 18 18"
        fill="none"
        stroke="currentColor"
        stroke-width="1.5"
        stroke-linecap="round"
        stroke-linejoin="round"
      >
        <path d="M13.5 6a4.5 4.5 0 10-9 0c0 5.25-2.25 6.75-2.25 6.75h13.5S13.5 11.25 13.5 6z" />
        <path d="M10.3 15.75a1.5 1.5 0 01-2.6 0" />
      </svg>
      <!-- Past nine the exact number stops being information and starts
           being a number that does not fit. -->
      <span v-if="unread > 0" class="count">{{ unread > 9 ? '9+' : unread }}</span>
    </button>

    <div v-if="open" class="panel">
      <header class="panel-head">
        <span>Notifications</span>
        <button v-if="unread > 0" class="clear-all" @click="markAllRead">Mark all read</button>
      </header>

      <p v-if="items.length === 0" class="panel-empty">Nothing to report.</p>

      <article
        v-for="n in items"
        v-else
        :key="n.id"
        class="entry"
        :class="[`is-${n.severity}`, { fresh: !n.read_at }]"
        @click="pick(n)"
      >
        <div class="entry-title">{{ n.title }}</div>
        <div v-if="n.body" class="entry-body">{{ n.body }}</div>
        <time class="entry-when">{{ timeAgo(n.created_at) }}</time>
      </article>
    </div>
  </div>
</template>

<style scoped>
.bell-wrap {
  position: relative;
}

.bell {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 34px;
  height: 34px;
  border: none;
  border-radius: 8px;
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  transition:
    background var(--transition),
    color var(--transition);
}

.bell:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

/* Something is waiting. The badge says how many, so the bell itself only
   has to stop looking dormant. */
.bell.ringing {
  color: var(--text-primary);
}

.count {
  position: absolute;
  top: 2px;
  right: 2px;
  min-width: 16px;
  height: 16px;
  padding: 0 4px;
  border-radius: 8px;
  background: var(--danger-600);
  color: var(--text-on-status);
  font-size: 10px;
  font-weight: 600;
  line-height: 16px;
  text-align: center;
}

.panel {
  position: absolute;
  top: calc(100% + 8px);
  right: 0;
  z-index: 60;
  width: 340px;
  max-height: 420px;
  overflow-y: auto;
  border: 1px solid var(--border-primary);
  border-radius: var(--radius-md);
  /* --bg-elevated never existed, so this rendered its #fff fallback in
     dark mode too - a white panel on a dark page. */
  background: var(--bg-popover);
  box-shadow: var(--shadow-lg);
}

.panel-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 14px;
  border-bottom: 1px solid var(--border-primary);
  font-size: 13px;
  font-weight: 600;
}

.clear-all {
  border: none;
  background: transparent;
  color: var(--accent-fg);
  font-size: 12px;
  cursor: pointer;
}

.panel-empty {
  margin: 0;
  padding: 24px 14px;
  color: var(--text-muted);
  font-size: 13px;
  text-align: center;
}

.entry {
  padding: 10px 14px;
  border-bottom: 1px solid var(--border-primary);
  /* Transparent rather than absent, so a warning gaining its colour
     does not also shift the text by three pixels. */
  border-left: 3px solid transparent;
  cursor: pointer;
  transition: background var(--transition);
}

.entry:last-child {
  border-bottom: none;
}

.entry:hover {
  background: var(--bg-hover);
}

/* Unread, which is about ATTENTION and is carried by the surface. The
   stripe is about SEVERITY and is a separate axis - a read error still
   says it was an error. */
.entry.fresh {
  background: var(--bg-active);
}

/* Ordinary news needs no stripe, so only the two that are not say so.
   The severity arrives on every notification and was being dropped:
   these two rules existed under names the markup never wrote, so a
   warning and a failure looked exactly like an announcement. */
.entry.is-warning {
  border-left-color: var(--warning-fg);
}

.entry.is-error {
  border-left-color: var(--danger-fg);
}

.entry-title {
  color: var(--text-primary);
  font-size: 13px;
  font-weight: 500;
}

.entry-body {
  margin-top: 2px;
  color: var(--text-secondary);
  font-size: 12px;
}

.entry-when {
  display: block;
  margin-top: 4px;
  color: var(--text-muted);
  font-size: 11px;
}
</style>
