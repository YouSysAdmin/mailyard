<script setup lang="ts">
// A remark that stays on the page, as opposed to a toast that goes away.
//
// A COMPONENT rather than four classes, because the shape was always the
// same four elements and ten views were writing it out: a box, a wrapper
// that existed only to sit beside an icon these never had, a heading
// paragraph and one or more body paragraphs. The wrapper is gone and the
// heading is a prop, so a caller writes what it has to say and nothing
// about how it is built.
//
// The severities are the badge vocabulary, not a second one: a warning
// notice and a warning badge on the same page have to be the same
// colour, and they now read from the same tokens.
withDefaults(
  defineProps<{
    kind?: 'info' | 'success' | 'warning' | 'danger'
    /** A line above the body, in the notice's own colour. */
    title?: string
  }>(),
  { kind: 'info', title: '' },
)
</script>

<template>
  <div class="notice" :class="`notice-${kind}`">
    <p v-if="title" class="notice-head">{{ title }}</p>
    <slot />
  </div>
</template>
