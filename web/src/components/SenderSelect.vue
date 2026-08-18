<script setup lang="ts">
import { ref, watch } from 'vue'
import type { Sender } from '../api/senders'
import { formatMailbox } from '../composables/mailbox'

// From-address picker backed by the project's approved senders.
// With no registered senders it stays a plain free-text input so
// nothing is blocked. With senders it renders a select of
// "Name <email>" options plus a Custom option that reveals the
// free-text input. v-model always carries the bare email address.
const props = defineProps<{
  modelValue: string
  senders: Sender[]
  id?: string
  placeholder?: string
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: string): void
  (e: 'sender', sender: Sender | null): void
}>()

const CUSTOM = '__custom__'
const selected = ref('')

// Set while this component itself emits a value change, so the
// modelValue watcher only re-syncs on external writes (form resets,
// loading an existing record).
let internalEdit = false

function senderLabel(s: Sender): string {
  return formatMailbox(s.email, s.name)
}

function sync() {
  if (props.senders.length === 0) return
  if (!props.modelValue) {
    selected.value = ''
    return
  }
  const match = props.senders.find((s) => s.email === props.modelValue)
  selected.value = match ? match.email : CUSTOM
}

watch(() => props.senders, sync, { immediate: true })

watch(
  () => props.modelValue,
  () => {
    if (internalEdit) {
      internalEdit = false
      return
    }
    sync()
  },
)

function onSelect() {
  if (selected.value === CUSTOM) {
    // Keep the current value as a starting point for the text input.
    emit('sender', null)
    return
  }
  const s = props.senders.find((x) => x.email === selected.value) ?? null
  internalEdit = true
  emit('update:modelValue', s ? s.email : '')
  emit('sender', s)
}

function onInput(e: Event) {
  internalEdit = true
  emit('update:modelValue', (e.target as HTMLInputElement).value)
}
</script>

<template>
  <input
    v-if="senders.length === 0"
    :id="id"
    :value="modelValue"
    type="text"
    class="form-input"
    :placeholder="placeholder || 'sender@example.com'"
    @input="onInput"
  />
  <div v-else class="sender-select">
    <select :id="id" v-model="selected" class="form-select" @change="onSelect">
      <option value="" disabled>Select a sender address</option>
      <option v-for="s in senders" :key="s.id" :value="s.email">{{ senderLabel(s) }}</option>
      <option :value="CUSTOM">Custom address...</option>
    </select>
    <input
      v-if="selected === CUSTOM"
      :value="modelValue"
      type="text"
      class="form-input"
      :placeholder="placeholder || 'sender@example.com'"
      @input="onInput"
    />
  </div>
</template>

<style scoped>
.sender-select {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
</style>
