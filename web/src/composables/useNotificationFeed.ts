// The activity bell: an unread count, the recent list behind it, and
// the live stream that keeps both current.
//
// A composable rather than state in the topbar, because there are
// three ways the count moves - a poll, a server-sent event, and the
// reader opening or clearing the panel - and keeping them in one place
// is what stops them disagreeing about the number on the badge.
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { notificationsApi, type Notification } from '../api/notifications'
import { connectEventStream } from '../api/eventstream'
import { useProjectStore } from '../stores/project'
import { useAutoRefresh } from './useAutoRefresh'

/** How many the panel shows. It is a glance, not a log. */
const PANEL_SIZE = 20

/** Badge poll interval - the fallback for a dropped stream. */
const POLL_MS = 60_000

export function useNotificationFeed() {
  const projects = useProjectStore()

  const unread = ref(0)
  const items = ref<Notification[]>([])
  let disconnect: (() => void) | null = null

  /**
   * Whether to watch at all.
   *
   * Two conditions, and both are about not making requests the caller
   * would be refused. Notifications are a tenant resource, so with no
   * project every tick answers 400 - and belonging to no project is an
   * ordinary state here. Without the permission it is a 403 a minute
   * plus a rejected EventSource: all swallowed, so the app looks fine
   * while the browser console fills with failures for endpoints that
   * person is not meant to know exist.
   */
  const watching = computed(() => !!projects.currentProjectId && projects.can('notifications:read'))

  /** Refresh the badge alone - cheaper than the list. */
  async function refreshBadge() {
    if (!watching.value) return

    try {
      unread.value = (await notificationsApi.unread()).data.unread
    } catch {
      // The next tick tries again. A failed badge refresh is not worth
      // interrupting anybody over.
    }
  }

  /** Load what the panel shows, and correct the badge with it. */
  async function loadPanel() {
    if (!watching.value) return

    try {
      const res = await notificationsApi.list({ limit: PANEL_SIZE })
      items.value = res.data.notifications ?? []
      unread.value = res.data.unread
    } catch {
      items.value = []
    }
  }

  async function markAllRead() {
    try {
      await notificationsApi.markAllRead()
      unread.value = 0
      const now = new Date().toISOString()
      items.value = items.value.map((n) => ({ ...n, read_at: n.read_at ?? now }))
    } catch {
      // Deliberately leaves the badge alone: a failed write must not
      // let the UI claim the alerts were cleared.
    }
  }

  /** Mark one read, tolerating a failed write. */
  async function markRead(n: Notification) {
    if (n.read_at) return

    try {
      await notificationsApi.markRead(n.id)
      n.read_at = new Date().toISOString()
      unread.value = Math.max(0, unread.value - 1)
    } catch {
      // Whatever the caller was doing with it still works.
    }
  }

  function listen(onIncoming: () => void) {
    disconnect?.()
    disconnect = null
    if (!watching.value) return

    disconnect = connectEventStream(projects.currentProjectId, (e) => {
      if (e.type !== 'notification.created') return

      unread.value += 1
      onIncoming()
    })
  }

  /**
   * Start watching, and keep watching the right project.
   *
   * Driven by a WATCH rather than by mount: neither the project nor
   * the permission set exists yet when a layout mounts - fetchProjects
   * resolves both. Reacting also covers the switcher, since the stream
   * is per project and the permission can differ between two projects
   * the same person belongs to.
   */
  function start(onIncoming: () => void) {
    // The badge follows the rule every refreshing surface follows: a
    // minute apart, and not at all in a background tab. It had a bare
    // setInterval once, which polled every open tab forever whether or
    // not anybody could see the number.
    useAutoRefresh(refreshBadge, { intervalMs: POLL_MS })

    watch(
      [() => projects.currentProjectId, watching],
      () => {
        unread.value = 0
        items.value = []
        void refreshBadge()
        listen(onIncoming)
      },
      { immediate: true },
    )
  }

  onBeforeUnmount(() => disconnect?.())

  return { unread, items, watching, start, loadPanel, markAllRead, markRead }
}
