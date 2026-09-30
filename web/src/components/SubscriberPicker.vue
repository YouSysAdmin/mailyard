<script setup lang="ts">
// An address box that searches the project's subscribers as you type.
//
// A select was the first cut, and a select holds the whole audience:
// fine at forty rows, unusable at forty thousand, and the request that
// filled it fetched them all. The list endpoint already searches by
// address, so this asks it for a handful of matches per keystroke and
// leaves the rest on the server.
//
// v-model is the TEXT in the box. A row picked from the menu fills the
// box with its address and is reported through `pick`, and editing the
// text afterwards reports null - what is typed no longer names a row.
// So a caller wanting an exact subscriber listens to `pick`, and one
// happy with an address reads the model.
import { onBeforeUnmount, ref, watch } from 'vue'
import { subscribersApi } from '../api/subscribers'
import type { Subscriber } from '../api/types'

const props = defineProps<{
  modelValue: string
  id?: string
  placeholder?: string
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: string): void
  (e: 'pick', subscriber: Subscriber | null): void
}>()

// The menu shows this many. It is a picker, not a list page.
const LIMIT = 8

const text = ref(props.modelValue)
const matches = ref<Subscriber[]>([])
const open = ref(false)
const active = ref(-1)
const searching = ref(false)

// Whether the text is what a picked row put there. Editing clears it.
let picked = false
let timer: ReturnType<typeof setTimeout> | undefined
// Answers arrive out of order when somebody types fast, and a slow
// one for "a" must not replace the menu built for "anna".
let serial = 0

watch(
  () => props.modelValue,
  (v) => {
    if (v !== text.value) text.value = v
  },
)

async function search() {
  const mine = ++serial
  searching.value = true
  try {
    const res = await subscribersApi.list({ q: text.value.trim() || undefined, limit: LIMIT })
    if (mine !== serial) return

    matches.value = res.data.subscribers ?? []
    active.value = matches.value.length > 0 ? 0 : -1
    open.value = true
  } catch {
    // The box still holds an address the caller can use as typed.
    if (mine === serial) matches.value = []
  } finally {
    if (mine === serial) searching.value = false
  }
}

function onInput(e: Event) {
  text.value = (e.target as HTMLInputElement).value
  emit('update:modelValue', text.value)
  if (picked) {
    picked = false
    emit('pick', null)
  }

  clearTimeout(timer)
  timer = setTimeout(search, 250)
}

// Opening on focus with an empty box shows the newest few, so a reader
// who does not know an address yet still has somewhere to start.
function onFocus() {
  if (matches.value.length > 0) {
    open.value = true

    return
  }

  void search()
}

function choose(s: Subscriber) {
  picked = true
  text.value = s.email
  open.value = false
  emit('update:modelValue', s.email)
  emit('pick', s)
}

function onKeydown(e: KeyboardEvent) {
  if (!open.value || matches.value.length === 0) return

  if (e.key === 'ArrowDown') {
    e.preventDefault()
    active.value = (active.value + 1) % matches.value.length
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    active.value = (active.value - 1 + matches.value.length) % matches.value.length
  } else if (e.key === 'Enter') {
    if (active.value >= 0) {
      e.preventDefault()
      choose(matches.value[active.value])
    }
  } else if (e.key === 'Escape') {
    e.stopPropagation()
    open.value = false
  }
}

// Closed on blur. A click on an option fires mousedown before the
// blur, and that handler prevents the default so the box keeps focus
// and the click lands.
function onBlur() {
  open.value = false
}

onBeforeUnmount(() => clearTimeout(timer))
</script>

<template>
  <div class="sub-picker">
    <input
      :id="id"
      :value="text"
      type="text"
      class="form-input"
      autocomplete="off"
      role="combobox"
      :aria-expanded="open"
      aria-autocomplete="list"
      :placeholder="placeholder || 'user@example.com'"
      @input="onInput"
      @focus="onFocus"
      @blur="onBlur"
      @keydown="onKeydown"
    />
    <ul v-if="open" class="sub-picker-menu" role="listbox">
      <li v-if="matches.length === 0" class="sub-picker-empty">
        {{ searching ? 'Searching...' : 'No subscriber matches' }}
      </li>
      <li
        v-for="(s, i) in matches"
        :key="s.id"
        class="sub-picker-option"
        :class="{ 'is-active': i === active }"
        role="option"
        :aria-selected="i === active"
        @mousedown.prevent="choose(s)"
        @mousemove="active = i"
      >
        <span class="sub-picker-email">{{ s.email }}</span>
        <span v-if="s.name" class="sub-picker-name">{{ s.name }}</span>
      </li>
    </ul>
  </div>
</template>

<style scoped>
.sub-picker {
  position: relative;
}

/* Over the field below it rather than pushing the form down, which is
   what a menu is. Popover surface and shadow, like the account menu.
   A dialog scrolls its box, so a caller placing this near the bottom
   of one leaves room underneath - see the member dialog. */
.sub-picker-menu {
  position: absolute;
  top: calc(100% + 4px);
  left: 0;
  right: 0;
  z-index: 20;
  margin: 0;
  padding: 4px;
  list-style: none;
  max-height: 260px;
  overflow-y: auto;
  background: var(--bg-popover);
  border: 1px solid var(--border-primary);
  border-radius: var(--radius);
  box-shadow: var(--shadow-md);
}

.sub-picker-option {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  gap: 12px;
  padding: 7px 10px;
  border-radius: var(--radius-sm);
  font-size: 13px;
  cursor: pointer;
}

.sub-picker-option.is-active {
  background: var(--bg-hover);
}

.sub-picker-email {
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sub-picker-name {
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex-shrink: 1;
}

.sub-picker-empty {
  padding: 7px 10px;
  font-size: 13px;
  color: var(--text-muted);
}
</style>
