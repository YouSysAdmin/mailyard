import { ref, computed, watch } from 'vue'

// The expiry half of a credential form, shared by the two key pages.
//
// A `datetime-local` input has no empty state a person can aim for:
// it opens showing a mask, and clearing it back to nothing once
// something has been typed is fiddly in every browser. So "leave it
// empty for a key that never expires" asked for a gesture the control
// does not really offer, and the safe outcome - a key that lives
// forever - was the one you got by doing nothing.
//
// Now it is a decision. The field opens one day ahead, and a key that
// should outlive that says so out loud.
const DEFAULT_DAYS = 1

// datetime-local wants `YYYY-MM-DDTHH:mm` in LOCAL time, which is
// exactly what toISOString does not give.
function localInputValue(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return (
    `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}` +
    `T${pad(d.getHours())}:${pad(d.getMinutes())}`
  )
}

export function useExpiry() {
  const never = ref(false)
  const at = ref('')

  function fillDefault() {
    const d = new Date()
    d.setDate(d.getDate() + DEFAULT_DAYS)
    at.value = localInputValue(d)
  }

  function reset() {
    never.value = false
    fillDefault()
  }

  // Load an existing credential's expiry, for the read-only view. The
  // local-time conversion stays in here, so nothing outside has to
  // know that toISOString is the wrong shape for the input.
  function set(iso?: string | null) {
    never.value = !iso
    at.value = iso ? localInputValue(new Date(iso)) : ''
  }

  // Coming back from "never" must not land on an empty required
  // field: the form would be disabled with nothing saying why, which
  // reads as broken rather than as unfinished.
  watch(never, (on) => {
    if (!on && !at.value) fillDefault()
  })

  // A date is required unless the caller said never, so a cleared
  // field cannot quietly mint a permanent credential.
  const invalid = computed(() => !never.value && !at.value)

  // What the API takes: absent means no expiry.
  function payload(): string | undefined {
    if (never.value || !at.value) return undefined
    return new Date(at.value).toISOString()
  }

  return { never, at, reset, set, invalid, payload }
}
