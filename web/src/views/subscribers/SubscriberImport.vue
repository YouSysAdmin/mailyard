<script setup lang="ts">
// Bulk-add subscribers from pasted JSON or CSV, or from a file.
//
// Its own component because it is a dialog with two modes, a file
// reader and its own error handling - none of which the list page it
// opens from has any reason to hold.
import { computed, ref } from 'vue'
import { subscribersApi, type SubscriberPayload } from '../../api/subscribers'
import { apiErrorMessage } from '../../api/client'
import { useNotificationStore } from '../../stores/notification'
import BaseModal from '../../components/BaseModal.vue'
import FormField from '../../components/FormField.vue'

const emit = defineEmits<{
  /** Something was imported, so the list behind this is stale. */
  (e: 'imported'): void
  (e: 'close'): void
}>()

const notify = useNotificationStore()

const mode = ref<'json' | 'csv'>('json')
const text = ref({ json: '', csv: '' })
const busy = ref(false)
const picker = ref<HTMLInputElement | null>(null)

const ready = computed(() => text.value[mode.value].trim() !== '')

async function run() {
  busy.value = true
  try {
    const res =
      mode.value === 'json'
        ? await subscribersApi.importJSON({ subscribers: asArray(text.value.json) })
        : await subscribersApi.importCSV(text.value.csv)

    const { created, updated, skipped } = res.data
    notify.success(`${created} created, ${updated} updated, ${skipped} skipped`)
    emit('imported')
    emit('close')
  } catch (e) {
    // A parse failure is this dialog's, not the server's, and saying
    // "failed to import" for it sends the reader looking in the wrong
    // place.
    //
    // A server refusal is told plainly too. What comes back names the
    // LEAF field of the offending row - `email`, not `subscribers[3]` -
    // and this dialog has no such input to put it under, so the summary
    // line is both all there is and the more useful of the two.
    if (e instanceof SyntaxError) notify.error('That is not valid JSON')
    else notify.error(apiErrorMessage(e, 'Failed to import'))
  } finally {
    busy.value = false
  }
}

/**
 * One object or many - both are things a person reasonably pastes.
 *
 * The cast is honest about what this knows: the text came from a person
 * and the SERVER is what validates it, field by field, and answers with
 * a per-row skip count. Checking the shape here would only move the
 * same refusal earlier and describe it worse.
 */
function asArray(raw: string): SubscriberPayload[] {
  const parsed: unknown = JSON.parse(raw)

  return (Array.isArray(parsed) ? parsed : [parsed]) as SubscriberPayload[]
}

async function onFile(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return

  text.value.csv = await file.text()
  // Cleared so choosing the SAME file again still fires a change.
  input.value = ''
}
</script>

<template>
  <BaseModal title="Import subscribers" @close="emit('close')">
    <!-- The stylesheet's own tab strip. These were three button classes
         deep - `btn btn-secondary btn-sm` with a bound `btn-primary`
         layered on top - so the chosen one carried two variants at once
         and which won came down to the order rules appear in. -->
    <div class="tabs">
      <button class="tab" :class="{ active: mode === 'json' }" @click="mode = 'json'">JSON</button>
      <button class="tab" :class="{ active: mode === 'csv' }" @click="mode = 'csv'">CSV</button>
    </div>

    <FormField
      v-if="mode === 'json'"
      label="JSON"
      hint="An array of objects, each carrying at least an email."
    >
      <textarea
        v-model="text.json"
        class="form-textarea code-font"
        rows="10"
        placeholder='[{"email": "user@example.com", "name": "User"}]'
      ></textarea>
    </FormField>

    <template v-else>
      <FormField
        label="CSV"
        hint="The header must name email. Optional: name, status, timezone, language. Anything else becomes a custom field."
      >
        <!-- The placeholder is a template literal rather than a
             multi-line attribute: the source indentation of the second
             line lands INSIDE the string, and the box showed forty
             spaces before the example row. -->
        <textarea
          v-model="text.csv"
          class="form-textarea code-font"
          rows="10"
          :placeholder="'email,name,status\nuser@example.com,User,subscribed'"
        ></textarea>
      </FormField>

      <input ref="picker" class="hidden" type="file" accept=".csv,text/csv" @change="onFile" />
      <button class="btn btn-secondary btn-sm" @click="picker?.click()">Load a file</button>
    </template>

    <template #footer>
      <button class="btn btn-secondary" @click="emit('close')">Cancel</button>
      <button class="btn btn-primary" :disabled="busy || !ready" @click="run">
        {{ busy ? 'Importing...' : 'Import' }}
      </button>
    </template>
  </BaseModal>
</template>
