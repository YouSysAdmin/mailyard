<script setup lang="ts">
// Copy a value to the clipboard and say so.
//
// Fourteen views wrote this out: writeText, a `copied` ref, a two second
// setTimeout to put the label back. Some of them raised a toast as well
// and some did not, so the same act reported itself differently
// depending on which page you were on.
//
// The button reports in its own label, because it is the thing that was
// clicked. A toast is available through `announce` for the cases that
// earned one: an operator copying an SPF fragment out of a page holding
// four similar records wants to be told which one is now on the
// clipboard, and the button's own label cannot say that without becoming
// a sentence. One component either way, so the two cannot drift apart -
// the difference is the message, not the mechanism.
import { onUnmounted, ref } from 'vue'
import { useNotificationStore } from '../stores/notification'

const props = withDefaults(
  defineProps<{
    value: string
    label?: string
    copiedLabel?: string
    // Any of the stylesheet's button classes. The default is the small
    // secondary button, which is what most of these were.
    variant?: string
    // Raise a toast saying this was copied, for values a label cannot
    // name. Empty means the label does the reporting.
    announce?: string
  }>(),
  { label: 'Copy', copiedLabel: 'Copied', variant: 'btn btn-secondary btn-sm', announce: '' },
)

const notify = useNotificationStore()

const copied = ref(false)
let timer: ReturnType<typeof setTimeout> | undefined

async function copy() {
  try {
    await navigator.clipboard.writeText(props.value)
  } catch {
    // A refused clipboard (an insecure origin, a browser that asks)
    // says so, because the reader's next move is to select the value by
    // hand and they need to know that is what happened.
    notify.error('Could not copy - select the value and copy it by hand')

    return
  }
  copied.value = true
  if (props.announce) notify.success(props.announce)
  clearTimeout(timer)
  timer = setTimeout(() => (copied.value = false), 2000)
}

// The timer outlives the button when a row is removed or a dialog closes
// while it is counting down.
onUnmounted(() => clearTimeout(timer))
</script>

<template>
  <button type="button" :class="variant" @click="copy">
    {{ copied ? copiedLabel : label }}
  </button>
</template>
