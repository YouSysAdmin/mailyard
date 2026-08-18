<script setup lang="ts">
// The Yard mark: a bounded plot with a gate broken into its top edge,
// holding a letter. Enclosure plus intake plus contents - receive, hold,
// dispatch, which is what the product does.
//
// The enclosure is drawn in currentColor so the mark takes the colour of
// whatever it sits in. Only the letter carries the accent, and it falls
// back to currentColor when no accent is in scope, so the mark still
// works in one colour.
//
// Optical sizing. A downward arrow in the gate reads as "download into a
// document" at large sizes, which is why the letter is there instead.
// Below about 20px the stroked envelope has the opposite problem - its
// flap crease turns to noise inside the rectangle - so `small` swaps it
// for a solid letter and thickens the enclosure. Same silhouette, fewer
// parts.
withDefaults(defineProps<{ size?: number | string; small?: boolean }>(), {
  size: 20,
  small: false,
})
</script>

<template>
  <svg
    :width="size"
    :height="size"
    viewBox="0 0 32 32"
    fill="none"
    aria-hidden="true"
    focusable="false"
    class="brand-mark"
  >
    <!-- The plot, broken at the top for the gate. -->
    <path
      d="M11.5 4.6H7.2a3 3 0 0 0-3 3v17.2a3 3 0 0 0 3 3h17.6a3 3 0 0 0 3-3V7.6a3 3 0 0 0-3-3h-4.3"
      stroke="currentColor"
      :stroke-width="small ? 2.6 : 2.1"
      stroke-linecap="round"
    />
    <!-- The letter it holds. -->
    <rect v-if="small" x="9.2" y="15" width="13.6" height="9" rx="2" class="letter-solid" />
    <template v-else>
      <rect
        x="9"
        y="14.6"
        width="14"
        height="9.6"
        rx="1.6"
        class="letter-line"
        stroke-width="2.1"
      />
      <path
        d="M9.6 16.4 16 21.1l6.4-4.7"
        class="letter-line"
        stroke-width="2.1"
        stroke-linecap="round"
        stroke-linejoin="round"
      />
    </template>
  </svg>
</template>

<style scoped>
.brand-mark {
  flex: 0 0 auto;
}
/* Two classes rather than one shared with both fill and stroke. A CSS
   declaration outranks an inline presentation attribute, so a single
   class setting fill would have overridden fill="none" on the outlined
   envelope and rendered it as a solid block - which is exactly what it
   did until this was split. */
.letter-line {
  fill: none;
  stroke: var(--accent-fg, currentColor);
}
.letter-solid {
  fill: var(--accent-fg, currentColor);
  stroke: none;
}
</style>
