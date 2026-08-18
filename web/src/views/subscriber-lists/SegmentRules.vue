<script setup lang="ts">
// The rule builder behind a dynamic list, and the count it matches.
//
// ONE component because there were two copies: the create dialog and
// the detail page each carried the RuleRow type, both option tables,
// rowsToRules, addRule, removeRule, previewSegment and thirty-five
// lines of identical markup. Only one of them had rulesToRows, which is
// the tell - the dialog never had to read an existing rule back, so the
// two halves of the same translation lived in different files.
//
// It speaks FilterRule to its caller and RuleRow to itself. A rule
// stores `custom_fields.<key>` as one string where the form needs two
// controls, so the translation has to happen somewhere: here, once,
// instead of at every call site.
import { computed, ref, watch } from 'vue'
import { subscriberListsApi } from '../../api/subscriberLists'
import { apiErrorMessage } from '../../api/client'
import type { FilterOperator, FilterRule, Subscriber } from '../../api/types'
import { useNotificationStore } from '../../stores/notification'
import StatusBadge from '../../components/StatusBadge.vue'

const props = withDefaults(defineProps<{ readonly?: boolean }>(), { readonly: false })

/** The rules as the API states them. Empty while nothing is written. */
const rules = defineModel<FilterRule[]>({ required: true })

const notify = useNotificationStore()

/** One editor row. `custom` splits the field into a choice and a key. */
interface RuleRow {
  fieldChoice: string
  customKey: string
  operator: FilterOperator
  value: string
}

const CUSTOM_PREFIX = 'custom_fields.'

const FIELDS = ['email', 'name', 'status', 'timezone', 'language'] as const

const OPERATORS: { value: FilterOperator; label: string }[] = [
  { value: 'eq', label: 'equals' },
  { value: 'neq', label: 'not equals' },
  { value: 'contains', label: 'contains' },
  { value: 'starts_with', label: 'starts with' },
  { value: 'ends_with', label: 'ends with' },
  { value: 'gt', label: 'greater than' },
  { value: 'lt', label: 'less than' },
  { value: 'exists', label: 'exists' },
]

const rows = ref<RuleRow[]>(toRows(rules.value))

const preview = ref<{ matched: number; sample: Subscriber[] } | null>(null)
const previewing = ref(false)

function toRows(list: FilterRule[]): RuleRow[] {
  return list.map((r) => {
    const custom = r.field.startsWith(CUSTOM_PREFIX)

    return {
      fieldChoice: custom ? 'custom' : r.field,
      customKey: custom ? r.field.slice(CUSTOM_PREFIX.length) : '',
      operator: r.operator,
      value: r.value === undefined || r.value === null ? '' : String(r.value),
    }
  })
}

/**
 * The rows that are actually a rule.
 *
 * A half-filled row is dropped rather than refused: rows are added by
 * pressing a button, so an empty one at the bottom is somebody in the
 * middle of typing, not a mistake worth an error message.
 */
const built = computed<FilterRule[]>(() =>
  rows.value
    .filter((r) => (r.fieldChoice === 'custom' ? r.customKey.trim() !== '' : r.fieldChoice !== ''))
    .map((r) => ({
      field: r.fieldChoice === 'custom' ? CUSTOM_PREFIX + r.customKey.trim() : r.fieldChoice,
      operator: r.operator,
      // `exists` asks whether the field is there at all, so a value
      // would be sent and ignored.
      value: r.operator === 'exists' ? undefined : r.value,
    })),
)

// The model follows the rows, so a caller reads the current rules
// without asking for them. Rows are NOT rebuilt from the model in turn:
// that round trip drops the half-typed row under the cursor.
watch(built, (v) => (rules.value = v), { deep: true })

// A list arriving after mount - the detail page loads it - fills the
// editor once. Length is the test rather than deep equality, because
// what has to be caught is going from nothing to something.
watch(
  () => rules.value,
  (v) => {
    if (rows.value.length === 0 && v.length > 0) rows.value = toRows(v)
  },
)

function add() {
  rows.value.push({ fieldChoice: 'email', customKey: '', operator: 'eq', value: '' })
}

function remove(index: number) {
  rows.value.splice(index, 1)
  preview.value = null
}

async function runPreview() {
  if (built.value.length === 0) {
    notify.error('Add a rule before previewing')

    return
  }

  previewing.value = true
  try {
    const res = await subscriberListsApi.previewSegment({ filter_rules: built.value })
    preview.value = { matched: res.data.matched, sample: res.data.sample ?? [] }
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to preview the segment'))
  } finally {
    previewing.value = false
  }
}
</script>

<template>
  <div>
    <div v-for="(row, i) in rows" :key="i" class="rule">
      <select v-model="row.fieldChoice" class="form-select" :disabled="readonly">
        <option v-for="f in FIELDS" :key="f" :value="f">{{ f }}</option>
        <option value="custom">custom field...</option>
      </select>

      <input
        v-if="row.fieldChoice === 'custom'"
        v-model="row.customKey"
        class="form-input"
        placeholder="field key"
        :disabled="readonly"
      />

      <select v-model="row.operator" class="form-select" :disabled="readonly">
        <option v-for="op in OPERATORS" :key="op.value" :value="op.value">{{ op.label }}</option>
      </select>

      <!-- `exists` takes no value, so the control goes rather than
           sitting there disabled and looking like something to fill. -->
      <input
        v-if="row.operator !== 'exists'"
        v-model="row.value"
        class="form-input"
        placeholder="value"
        :disabled="readonly"
      />

      <button v-if="!readonly" class="btn btn-danger btn-sm" @click="remove(i)">Remove</button>
    </div>

    <p v-if="rows.length === 0" class="form-hint">
      No rules yet. A dynamic list needs at least one - every rule has to match.
    </p>

    <div class="rule-actions">
      <button v-if="!readonly" class="btn btn-secondary btn-sm" @click="add">Add rule</button>
      <button
        class="btn btn-secondary btn-sm"
        :disabled="previewing || built.length === 0"
        @click="runPreview"
      >
        {{ previewing ? 'Counting...' : 'Preview' }}
      </button>
    </div>

    <div v-if="preview" class="preview">
      <p class="preview-count">
        <strong>{{ preview.matched }}</strong>
        {{ preview.matched === 1 ? 'subscriber matches' : 'subscribers match' }} these rules.
      </p>

      <div v-if="preview.sample.length > 0" class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th>Email</th>
              <th>Name</th>
              <th>Status</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="s in preview.sample" :key="s.id">
              <td>{{ s.email }}</td>
              <td>{{ s.name || '-' }}</td>
              <td><StatusBadge :status="s.status" scope="subscriber" /></td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* Wraps rather than scrolls: four controls on one line need about
   560px, and this sits inside a dialog that can be narrower. */
.rule {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}

/* Every control shares the width left over, so a row reads as one
   sentence instead of as a wide select beside a narrow input. */
.rule .form-select,
.rule .form-input {
  flex: 1;
  min-width: 120px;
}

.rule-actions {
  display: flex;
  gap: 8px;
}

.preview {
  margin-top: 12px;
  border-top: 1px solid var(--border-primary);
  padding-top: 12px;
}

.preview-count {
  margin: 0 0 8px;
  font-size: 13px;
  color: var(--text-secondary);
}
</style>
