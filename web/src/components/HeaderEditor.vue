<script setup lang="ts">
// Custom message headers as name/value rows, wherever they are edited:
// a message being composed, a campaign, a project's defaults. The rows
// and their rules live in composables/headerRows.ts.
import { computed } from 'vue'
import { type HeaderRow, headerRowsProblem } from '../composables/headerRows'

const props = withDefaults(
  defineProps<{
    /** Greys every control, for a reader who may not edit. */
    disabled?: boolean
    /** How many named rows the server accepts. */
    max?: number
  }>(),
  { disabled: false, max: 20 },
)

const rows = defineModel<HeaderRow[]>({ required: true })

const problem = computed(() => headerRowsProblem(rows.value, props.max))
const full = computed(() => rows.value.length >= props.max)

function add() {
  rows.value = [...rows.value, { name: '', value: '' }]
}

function remove(index: number) {
  rows.value = rows.value.filter((_, i) => i !== index)
}
</script>

<template>
  <div>
    <div v-for="(row, i) in rows" :key="i" class="header-row">
      <input
        v-model="row.name"
        class="form-input header-name code-font"
        placeholder="X-Header-Name"
        spellcheck="false"
        :disabled="disabled"
      />
      <input
        v-model="row.value"
        class="form-input header-value"
        placeholder="value"
        :disabled="disabled"
      />
      <button type="button" class="btn btn-danger btn-sm" :disabled="disabled" @click="remove(i)">
        Remove
      </button>
    </div>

    <p v-if="problem" class="form-error">{{ problem }}</p>

    <button
      type="button"
      class="btn btn-secondary btn-sm"
      :disabled="disabled || full"
      @click="add"
    >
      Add header
    </button>
  </div>
</template>

<style scoped>
/* Name, value, remove on one line, wrapping on a phone. The value takes
   twice the room: a name is X-Something, a value is a sentence. */
.header-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}

.header-row .header-name {
  flex: 1;
  min-width: 140px;
}

.header-row .header-value {
  flex: 2;
  min-width: 160px;
}
</style>
