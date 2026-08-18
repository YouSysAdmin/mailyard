<script setup lang="ts">
// The permission editor: a checkbox grid over the catalogue, plus the
// same set as policy text underneath.
//
// One component because two things carry a permission list - a
// project role and an API key - and they must offer the same
// catalogue in the same order. A second copy would drift the moment a
// resource is added, and the drift would be invisible: an absent row
// looks exactly like a permission nobody wanted.
//
// A checkbox is drawn only where the RESOURCE declares that action.
// Most have fewer than three, and a box that grants nothing teaches
// people to stop reading the rest of them.
//
// The catalogue is passed in rather than fetched here, so a page that
// already loaded it for another reason does not fetch it twice.
import { computed, ref, watch } from 'vue'
import type { PermissionResource } from '../api/types'
import FormField from '../components/FormField.vue'

const props = defineProps<{
  // The selected "resource:action" strings.
  modelValue: string[]
  catalog: PermissionResource[]
  // allowWildcard offers the "*" option. True for API keys, false for
  // project roles - see permission.ForKey for why the two differ. A
  // role says "everything" by making the member an owner instead.
  allowWildcard?: boolean
  // readonly renders the same grid as a record of what a credential
  // holds. Reused rather than rebuilt, so a resource added to the
  // catalogue appears in both places at once.
  readonly?: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string[]]
  // Raised whenever the policy text cannot be parsed, so the parent
  // can disable its save button rather than send something the server
  // will refuse.
  'update:invalid': [invalid: boolean]
}>()

const selected = computed(() => new Set(props.modelValue))
const wildcard = computed(() => selected.value.has('*'))

const contentResources = computed(() => props.catalog.filter((r) => !r.infrastructure))
const infraResources = computed(() => props.catalog.filter((r) => r.infrastructure))

// The Vault-style text form. The GRID is authoritative while
// checkboxes are clicked; the textarea applies on blur, refusing
// unknown entries loudly instead of dropping them - a typo that saved
// fine and then silently granted nothing is the failure mode this
// guards.
const jsonText = ref('[]')
const jsonError = ref('')

watch(jsonError, (v) => emit('update:invalid', v !== ''))

function emitSet(next: Set<string>) {
  const list = [...next].sort()
  jsonText.value = JSON.stringify(list, null, 2)
  jsonError.value = ''
  emit('update:modelValue', list)
}

// Re-render the text whenever the parent replaces the list (opening
// the editor on a different record, say).
watch(
  () => props.modelValue,
  (v) => {
    const rendered = JSON.stringify([...v].sort(), null, 2)
    if (rendered !== jsonText.value) {
      jsonText.value = rendered
      jsonError.value = ''
    }
  },
  { immediate: true },
)

function applyTextToGrid() {
  if (props.readonly) return
  let parsed: unknown
  try {
    parsed = JSON.parse(jsonText.value)
  } catch {
    jsonError.value = 'Not valid JSON'
    return
  }
  if (!Array.isArray(parsed) || parsed.some((x) => typeof x !== 'string')) {
    jsonError.value = 'Expected a JSON array of "resource:action" strings'
    return
  }
  const known = new Set<string>()
  for (const r of props.catalog) {
    for (const a of r.actions) known.add(`${r.resource}:${a}`)
  }
  if (props.allowWildcard) known.add('*')
  const unknown = (parsed as string[]).filter((p) => !known.has(p))
  if (unknown.length > 0) {
    jsonError.value = `Unknown permission: ${unknown.join(', ')}`
    return
  }
  emitSet(new Set(parsed as string[]))
}

function toggle(resource: string, action: string) {
  if (props.readonly) return
  const key = `${resource}:${action}`
  const next = new Set(selected.value)
  if (next.has(key)) next.delete(key)
  else next.add(key)
  emitSet(next)
}

function has(resource: string, action: string): boolean {
  return selected.value.has(`${resource}:${action}`)
}

// The action columns, in catalogue order rather than per resource, so
// every row's boxes line up under the same headings even where a
// resource has only one of them.
const columns = ['read', 'write', 'delete']

function toggleWildcard() {
  if (props.readonly) return
  emitSet(wildcard.value ? new Set() : new Set(['*']))
}
</script>

<template>
  <div>
    <label v-if="allowWildcard" class="checkbox-label wildcard-row">
      <input type="checkbox" :checked="wildcard" :disabled="readonly" @change="toggleWildcard" />
      <span>
        <strong>Full access</strong>
        <small class="form-hint">
          Everything in this project, including resources added in future versions.
        </small>
      </span>
    </label>

    <template v-if="!wildcard">
      <div class="perm-grid">
        <div class="perm-row perm-head">
          <div></div>
          <div v-for="a in columns" :key="a" class="perm-col">{{ a }}</div>
        </div>
        <div v-for="r in contentResources" :key="r.resource" class="perm-row">
          <div class="perm-label" :title="r.description">{{ r.label }}</div>
          <div v-for="a in columns" :key="a" class="perm-col">
            <input
              v-if="r.actions.includes(a)"
              type="checkbox"
              :aria-label="`${r.label} ${a}`"
              :disabled="readonly"
              :checked="has(r.resource, a)"
              @change="toggle(r.resource, a)"
            />
          </div>
        </div>
      </div>

      <p class="grid-section">Infrastructure and governance</p>
      <div class="perm-grid">
        <div class="perm-row perm-head">
          <div></div>
          <div v-for="a in columns" :key="a" class="perm-col">{{ a }}</div>
        </div>
        <div v-for="r in infraResources" :key="r.resource" class="perm-row">
          <div class="perm-label" :title="r.description">{{ r.label }}</div>
          <div v-for="a in columns" :key="a" class="perm-col">
            <input
              v-if="r.actions.includes(a)"
              type="checkbox"
              :aria-label="`${r.label} ${a}`"
              :disabled="readonly"
              :checked="has(r.resource, a)"
              @change="toggle(r.resource, a)"
            />
          </div>
        </div>
      </div>
    </template>

    <!-- The parse failure is the field's ERROR, not a hint painted red:
         FormField already replaces the guidance with it, which is what
         the two branches here were doing by hand. -->
    <FormField
      class="mt-4"
      label="As policy text"
      :error="jsonError"
      :hint="
        readonly
          ? ''
          : 'The same permissions as JSON - edit either form. Unknown entries are refused, not dropped.'
      "
    >
      <textarea
        v-model="jsonText"
        class="form-textarea policy-text"
        rows="6"
        spellcheck="false"
        :readonly="readonly"
        @blur="applyTextToGrid"
      ></textarea>
    </FormField>
  </div>
</template>

<style scoped>
.perm-grid {
  display: grid;
  gap: 4px;
}
.perm-row {
  display: grid;
  grid-template-columns: 1fr 70px 70px 70px;
  align-items: center;
  padding: 4px 8px;
  border-radius: 6px;
}
.perm-row:hover {
  background: var(--bg-hover, rgba(128, 128, 128, 0.08));
}
.perm-head {
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--text-secondary);
}
.perm-head:hover {
  background: none;
}
.perm-col {
  text-align: center;
}
.perm-label {
  font-size: 13px;
}
.grid-section {
  font-size: 13px;
  font-weight: 600;
  letter-spacing: 0.02em;
  color: var(--text-secondary);
  margin: 16px 0 8px;
  padding-bottom: 6px;
  border-bottom: 1px solid var(--border-secondary);
}
.wildcard-row {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 10px;
  border-radius: 6px;
  border: 1px solid var(--border-secondary);
}
.wildcard-row .form-hint {
  display: block;
  margin-top: 2px;
}
.policy-text {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
}
</style>
