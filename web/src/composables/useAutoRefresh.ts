import { onMounted, onUnmounted, ref, watch, type Ref } from 'vue'

// Keeping a page fresh without reloading it.
//
// The pages this is for show rows written by something else - mail going
// out, mail arriving, what a test suite captured, what somebody changed -
// so without this the only way to see the current state is reloading the
// whole console. One composable rather than a setInterval in each view:
// the rules below are all easy to get subtly wrong, and six copies means
// six chances to.

export interface AutoRefreshOptions {
  // How often to refresh. Ten seconds for a list.
  intervalMs?: number
  // Where the Auto choice is remembered. Omit and it is not offered -
  // which is right for a poll that exists to watch one thing finish.
  storageKey?: string
  // Whether Auto starts on for a caller that has never chosen.
  autoDefault?: boolean
  // Skip ticks while this is true, without turning Auto off. Two cases:
  // a list that has been paged back through, where refreshing would yank
  // the reader to the newest page every ten seconds, and a poll whose
  // subject has settled. Skipping rather than stopping for good is what
  // lets a retry put a finished message back under the poll.
  pauseWhen?: () => boolean
}

export interface AutoRefresh {
  // True while a refresh is in flight. Not the page's own loading flag:
  // that one swaps the table for a spinner, and doing it every ten
  // seconds makes the page unreadable. This drives the button only.
  refreshing: Ref<boolean>
  // Refresh now. What the button calls.
  refresh: () => Promise<void>
  // The Auto toggle, persisted when a storageKey was given.
  auto: Ref<boolean>
  // True when Auto is on but ticks are being skipped, so the page can
  // say so rather than looking broken.
  paused: Ref<boolean>
  // The cadence, in seconds, for RefreshControl's tooltip.
  //
  // RETURNED rather than passed to the control by the view, because a
  // number a view has to remember to forward is a number that goes
  // wrong: `everySeconds` was declared on RefreshControl for exactly
  // this and NOTHING passed it, so the tooltip said ten seconds
  // regardless - and the email detail page polls every three.
  everySeconds: number
}

export function useAutoRefresh(
  reload: () => unknown | Promise<unknown>,
  options: AutoRefreshOptions = {},
): AutoRefresh {
  const intervalMs = options.intervalMs ?? 10_000
  const everySeconds = Math.round(intervalMs / 1000)
  const refreshing = ref(false)
  const paused = ref(false)
  const auto = ref(readAuto(options))

  let timer: ReturnType<typeof setInterval> | undefined

  async function refresh() {
    // Never two at once. A slow list plus a ten second tick would
    // otherwise stack requests until the page gives up, and the last
    // response to arrive - not the newest - would win.
    if (refreshing.value) return
    refreshing.value = true
    try {
      await reload()
    } finally {
      refreshing.value = false
    }
  }

  function tick() {
    // Only when the user is actually looking. A background tab polling
    // every ten seconds is a request per tab per ten seconds forever,
    // and nobody is reading the answer.
    if (document.hidden) return
    if (!auto.value) return
    if (options.pauseWhen?.()) {
      paused.value = true
      return
    }
    paused.value = false
    void refresh()
  }

  function stop() {
    if (timer !== undefined) {
      clearInterval(timer)
      timer = undefined
    }
  }

  // Coming back to the tab refreshes immediately rather than waiting out
  // the rest of the interval: the data on screen is as old as the time
  // spent away, which is exactly when it is most likely to be wrong.
  function onVisible() {
    if (!document.hidden) tick()
  }

  onMounted(() => {
    timer = setInterval(tick, intervalMs)
    document.addEventListener('visibilitychange', onVisible)
  })

  onUnmounted(() => {
    stop()
    document.removeEventListener('visibilitychange', onVisible)
  })

  if (options.storageKey) {
    const key = options.storageKey
    watch(auto, (on) => {
      try {
        localStorage.setItem(key, on ? '1' : '0')
      } catch {
        // A browser refusing storage is not a reason to stop refreshing.
      }
      // Turning it on refreshes at once. Waiting out the rest of the
      // interval reads as a switch that did nothing.
      if (on) tick()
    })
  }

  return { refreshing, refresh, auto, paused, everySeconds }
}

function readAuto(options: AutoRefreshOptions): boolean {
  const fallback = options.autoDefault ?? true
  if (!options.storageKey) return true
  try {
    const raw = localStorage.getItem(options.storageKey)
    if (raw === null) return fallback
    return raw === '1'
  } catch {
    return fallback
  }
}
