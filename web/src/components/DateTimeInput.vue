<script setup lang="ts">
// A date and a 24-hour time, in the shape datetime-local gives
// (`YYYY-MM-DDTHH:mm`, or "" when no date is chosen).
//
// The native datetime-local and time controls follow the browser locale
// and show AM/PM in en-US, while the console shows one clock (see
// formatDate.ts). So only the date uses the native picker, which carries
// no clock, and the time is two lists, 00-23 and 00-59.
import { computed } from 'vue'

const props = defineProps<{
  modelValue: string
  id?: string
  required?: boolean
  disabled?: boolean
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: string): void
}>()

// The time a freshly picked date starts at, until the lists say otherwise.
const DEFAULT_HOUR = '09'
const DEFAULT_MINUTE = '00'

const pad = (n: number) => String(n).padStart(2, '0')
const hours = Array.from({ length: 24 }, (_, i) => pad(i))
const minutes = Array.from({ length: 60 }, (_, i) => pad(i))

const date = computed(() => props.modelValue.slice(0, 10))
const hour = computed(() => props.modelValue.slice(11, 13) || DEFAULT_HOUR)
const minute = computed(() => props.modelValue.slice(14, 16) || DEFAULT_MINUTE)

function update(d: string, h: string, m: string) {
  emit('update:modelValue', d ? `${d}T${h}:${m}` : '')
}

function onDate(e: Event) {
  update((e.target as HTMLInputElement).value, hour.value, minute.value)
}

function onHour(e: Event) {
  update(date.value, (e.target as HTMLSelectElement).value, minute.value)
}

function onMinute(e: Event) {
  update(date.value, hour.value, (e.target as HTMLSelectElement).value)
}
</script>

<template>
  <div class="datetime">
    <input
      :id="id"
      :value="date"
      type="date"
      class="form-input"
      :required="required"
      :disabled="disabled"
      @input="onDate"
    />
    <select
      :value="hour"
      class="form-select"
      aria-label="Hour"
      :disabled="disabled || !date"
      @change="onHour"
    >
      <option v-for="h in hours" :key="h" :value="h">{{ h }}</option>
    </select>
    <span class="datetime-sep">:</span>
    <select
      :value="minute"
      class="form-select"
      aria-label="Minute"
      :disabled="disabled || !date"
      @change="onMinute"
    >
      <option v-for="m in minutes" :key="m" :value="m">{{ m }}</option>
    </select>
  </div>
</template>

<style scoped>
.datetime {
  display: flex;
  align-items: center;
  gap: 6px;
}

.datetime .form-input {
  flex: 1 1 auto;
  min-width: 0;
}

.datetime .form-select {
  flex: 0 0 auto;
  width: auto;
}

.datetime-sep {
  color: var(--text-muted);
}
</style>
