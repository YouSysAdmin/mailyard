<script setup lang="ts">
// From / To search over a mail client's left pane: part of the sender,
// part of any recipient, both at once when both are given.
//
// Labelled From and To because those are the words the list, the reader
// and every form use. What is matched is the envelope, which is what the
// list rows show too - To therefore also finds a Bcc recipient.
//
// Collapsed behind a toggle, because it is reached for now and then
// and two inputs cost the list two rows. Collapsing keeps the terms,
// and the toggle counts them, so a short list is never unexplained.
// The default slot sits beside the toggle - the inbound status select.
//
// Submitted, not typed - Enter, or clearing a box - so a half-typed
// address is not a query per keystroke. The page owns the applied terms
// and passes them back in, which is what lets it reset them.
import { computed, nextTick, ref, watch } from 'vue'

export interface MessageSearchTerms {
  sender: string
  recipient: string
}

const props = defineProps<{ sender: string; recipient: string }>()
const emit = defineEmits<{ (e: 'apply', terms: MessageSearchTerms): void }>()

const sender = ref(props.sender)
const recipient = ref(props.recipient)
const active = computed(() => (props.sender ? 1 : 0) + (props.recipient ? 1 : 0))
const open = ref(active.value > 0)
const first = ref<HTMLInputElement | null>(null)

watch(
  () => [props.sender, props.recipient],
  ([s, r]) => {
    sender.value = s
    recipient.value = r
  },
)

async function toggle() {
  open.value = !open.value
  if (!open.value) return

  await nextTick()
  first.value?.focus()
}

function apply() {
  const next = { sender: sender.value.trim(), recipient: recipient.value.trim() }
  if (next.sender === props.sender && next.recipient === props.recipient) return

  emit('apply', next)
}
</script>

<template>
  <div>
    <div class="message-search-bar">
      <slot />
      <button
        type="button"
        class="btn btn-secondary btn-sm message-search-toggle"
        :aria-expanded="open"
        title="Filter by From or To"
        @click="toggle"
      >
        Filter
        <span v-if="active" class="message-search-count">{{ active }}</span>
      </button>
    </div>
    <div v-if="open" class="message-search-fields">
      <input
        ref="first"
        v-model="sender"
        class="form-input"
        type="search"
        placeholder="From"
        aria-label="Filter by From"
        title="Part of the From address. Press Enter to apply."
        @keyup.enter="apply"
        @search="apply"
      />
      <input
        v-model="recipient"
        class="form-input"
        type="search"
        placeholder="To"
        aria-label="Filter by To"
        title="Part of any To address, Bcc included. Press Enter to apply."
        @keyup.enter="apply"
        @search="apply"
      />
    </div>
  </div>
</template>
