<script setup lang="ts">
// The row actions of a table, folded into one button.
//
// Three buttons per row was fine while every column was short. Give the
// name column real names and the row wraps or the table scrolls, and it
// is the actions - the least-read cells on the page - that took the
// room. One trigger, and the choices appear only when asked for.
//
// TELEPORTED to body and positioned from the trigger's rectangle: the
// table sits in an overflow-x: auto wrapper, and a popover positioned
// inside it is clipped at the wrapper's edge - which for the last rows is
// exactly where the menu wants to open. Fixed positioning survives the
// wrapper; scrolling or resizing closes the menu rather than trying to
// follow.
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'

withDefaults(defineProps<{ label?: string }>(), { label: 'Actions' })

const open = ref(false)
const trigger = ref<HTMLElement | null>(null)
const sheet = ref<HTMLElement | null>(null)
const style = ref<{ top: string; left: string }>({ top: '0px', left: '0px' })

function place() {
  const t = trigger.value?.getBoundingClientRect()
  const s = sheet.value?.getBoundingClientRect()
  if (!t || !s) return
  // Right edges aligned, below the trigger. Flip above when the sheet
  // would run off the bottom of the viewport.
  const top = t.bottom + 4 + s.height > window.innerHeight ? t.top - 4 - s.height : t.bottom + 4
  style.value = { top: `${top}px`, left: `${Math.max(8, t.right - s.width)}px` }
}

function close() {
  open.value = false
}

function onDocumentClick(e: MouseEvent) {
  const n = e.target as Node
  if (!trigger.value?.contains(n) && !sheet.value?.contains(n)) close()
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') close()
}

watch(open, async (is) => {
  if (is) {
    await nextTick()
    place()
    document.addEventListener('click', onDocumentClick, true)
    document.addEventListener('keydown', onKey)
    window.addEventListener('scroll', close, true)
    window.addEventListener('resize', close)
  } else {
    document.removeEventListener('click', onDocumentClick, true)
    document.removeEventListener('keydown', onKey)
    window.removeEventListener('scroll', close, true)
    window.removeEventListener('resize', close)
  }
})

onBeforeUnmount(close)
</script>

<template>
  <div ref="trigger" class="action-menu-trigger">
    <button
      type="button"
      class="btn btn-secondary btn-sm"
      :aria-expanded="open"
      aria-haspopup="menu"
      @click="open = !open"
    >
      {{ label }}
      <svg
        width="12"
        height="12"
        viewBox="0 0 16 16"
        fill="none"
        stroke="currentColor"
        stroke-width="1.5"
        stroke-linecap="round"
        stroke-linejoin="round"
      >
        <path d="M4 6l4 4 4-4" />
      </svg>
    </button>

    <!-- Any click inside closes the menu: every entry is an action, and
         an action that leaves its menu open reads as one that did not
         take. -->
    <Teleport to="body">
      <div v-if="open" ref="sheet" class="action-menu" role="menu" :style="style" @click="close">
        <slot />
      </div>
    </Teleport>
  </div>
</template>
