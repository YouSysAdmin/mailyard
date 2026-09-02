<script setup lang="ts">
// The Yard mark. The drawing lives in assets/brand-mark.svg and is
// pulled in through an external <use>, so the page carries a reference
// and not the geometry - one file to change, and one file the browser
// caches across every page that shows it.
//
// `?no-inline` keeps it a file: below its size limit vite would fold
// the asset into a data: URI, and a <use> may not point at one.
//
// Colour crosses the <use> boundary because it is inherited: the
// enclosure takes currentColor from wherever the host svg sits, the
// letter reads --accent-fg the same way. Sizing is the prop and only
// the prop - a width in CSS beats the svg's own attribute and makes the
// number a page passes a lie.
//
// `small` picks the optical variant for use below about 20px: a solid
// letter and a heavier enclosure, since a stroked envelope's flap
// crease turns to noise at that size.
import markUrl from '../assets/brand-mark.svg?no-inline'

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
    aria-hidden="true"
    focusable="false"
    class="brand-mark"
  >
    <use :href="`${markUrl}#${small ? 'mark-small' : 'mark'}`" />
  </svg>
</template>

<style scoped>
.brand-mark {
  flex: 0 0 auto;
}
</style>
