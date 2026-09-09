<script setup lang="ts">
// One number with a glyph and a line saying what it is measured against.
//
// It sits in components/ and not under views/dashboard/ because two
// pages draw these, and a glyph drawn on the nav grid (18 viewBox,
// stroke 1.5) rather than this one reads at a visibly thinner weight.
//
// The CLASSES are the stylesheet's own `.stat-card` family, not a
// private set: they are global, the inbound page and the campaign page
// use them too, and a component that reinvented them would be a second
// definition of a card that already exists.
//
// `sub` is where the honesty lives. Several of these are RATES, and a
// rate with no denominator is not zero, it is unmeasured - so the caller
// passes '-' as the value and says why underneath, rather than printing
// a number nobody can act on.
import { STAT_ICONS } from './statIcons'

defineProps<{
  label: string
  /** A key in STAT_ICONS. */
  icon: string
  /** Already formatted - separators, a percent sign, or '-'. */
  value: string
  /** What the number is out of, or why there is no number. */
  sub?: string
}>()
</script>

<template>
  <div class="stat-card">
    <div class="stat-header">
      <div class="stat-label">{{ label }}</div>
      <div class="stat-icon">
        <!-- One wrapper for all ten, so the size and stroke cannot drift
             between cards the way they do when each is written by hand. -->
        <svg
          width="20"
          height="20"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
          v-html="STAT_ICONS[icon] ?? ''"
        ></svg>
      </div>
    </div>

    <div class="stat-value">{{ value }}</div>
    <div v-if="sub" class="stat-sub">{{ sub }}</div>
  </div>
</template>
