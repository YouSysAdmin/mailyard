<script setup lang="ts">
// A stack of labelled figures, divided.
//
// Two cards on the settings page show one: what the project has used
// against what its plan allows, and what an export just wrote out. They
// are the same shape and were the same twenty lines of CSS twice.
//
// The value is a STRING, formatted by the caller. Usage says "12 / 500"
// where a ceiling applies and "12" where none does, and an export says a
// plain count - deciding that here would mean this component knowing
// what a limit of zero means, which is the caller's business.
export interface Figure {
  label: string
  value: string
}

defineProps<{ rows: Figure[] }>()
</script>

<template>
  <div class="figure-rows">
    <div v-for="row in rows" :key="row.label" class="figure-row">
      <span class="figure-label">{{ row.label }}</span>
      <span class="figure-value">{{ row.value }}</span>
    </div>
  </div>
</template>

<style scoped>
.figure-rows {
  display: flex;
  flex-direction: column;
}

.figure-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 0;
  border-bottom: 1px solid var(--border-primary);
  font-size: 13px;
}

/* The card's own edge draws the line under the last row. */
.figure-row:last-child {
  border-bottom: none;
}

.figure-label {
  color: var(--text-secondary);
}

.figure-value {
  font-weight: 500;
  color: var(--text-primary);
  /* Tabular figures, so a column of numbers lines up on the digit
     rather than drifting with the width of a 1. */
  font-variant-numeric: tabular-nums;
}
</style>
