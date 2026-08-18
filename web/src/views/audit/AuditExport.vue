<script setup lang="ts">
// Taking the trail away as a file.
//
// The reason to export an audit log is to hold the record somewhere
// other than the system being audited - which is why TRUNCATION IS SAID
// OUT LOUD. A short file nobody was warned about is the one failure that
// matters here, so hitting the ceiling is reported as an error even
// though the download succeeded.
import { ref } from 'vue'
import { auditApi, type AuditEvent, type AuditWindow } from '../../api/audit'
import { apiErrorMessage } from '../../api/client'
import { useNotificationStore } from '../../stores/notification'
import { dateStamp, downloadText, toCSV } from '../../composables/download'
import BaseModal from '../../components/BaseModal.vue'
import FormField from '../../components/FormField.vue'

const props = defineProps<{
  /** Which of the two trails is on screen. */
  trail: 'project' | 'security'
  /** Security only: every account rather than your own. */
  allAccounts: boolean
}>()

const emit = defineEmits<{ (e: 'close'): void }>()

const notify = useNotificationStore()

const from = ref('')
const to = ref('')
const format = ref<'csv' | 'json'>('csv')
const exporting = ref(false)

// Column ORDER is the file's contract - a spreadsheet somebody built on
// last month's export reads by position - so the header list and the row
// builder sit together and are changed together.
const CSV_HEADERS = [
  'when',
  'category',
  'type',
  'actor_email',
  'actor_id',
  'client_ip',
  'user_agent',
  'method',
  'path',
  'status',
  'detail',
  'project_id',
  'event_id',
]

function csvRow(e: AuditEvent): unknown[] {
  return [
    e.created_at,
    e.category,
    e.type,
    e.actor_email,
    e.actor_id,
    e.client_ip,
    e.user_agent,
    e.method,
    e.path,
    e.status,
    e.detail,
    e.project_id,
    e.id,
  ]
}

async function run() {
  exporting.value = true
  try {
    const w: AuditWindow = {}
    if (from.value) w.from = from.value
    if (to.value) w.to = to.value

    const res =
      props.trail === 'project'
        ? await auditApi.projectLogExport(w)
        : await auditApi.securityLogExport(w, props.allAccounts)
    const doc = res.data
    const rows = doc.events ?? []
    if (rows.length === 0) {
      notify.error('Nothing recorded in that range, so there was nothing to export')

      return
    }

    // The timestamps in the file are the SERVER's, so the name carries
    // the window that was asked for rather than today's date.
    const range =
      from.value || to.value ? `${from.value || 'start'}_${to.value || 'now'}` : dateStamp()
    const base = `mailyard-${props.trail === 'project' ? 'audit' : 'security'}-log-${range}`

    if (format.value === 'csv') {
      downloadText(`${base}.csv`, 'text/csv', toCSV(CSV_HEADERS, rows.map(csvRow)))
    } else {
      downloadText(`${base}.json`, 'application/json', JSON.stringify(doc, null, 2))
    }

    emit('close')

    if (doc.truncated) {
      notify.error(
        `Exported ${doc.count} events, which is the ceiling - there are older ones in this ` +
          `range. Export it in shorter windows to get them all.`,
      )
    } else {
      notify.success(`Exported ${doc.count} event${doc.count === 1 ? '' : 's'}`)
    }
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to export the audit log'))
  } finally {
    exporting.value = false
  }
}
</script>

<template>
  <BaseModal @close="emit('close')">
    <template #header>
      <h3>Export {{ trail === 'project' ? 'project activity' : 'account security' }}</h3>
    </template>
    <p class="text-sm text-muted mb-3">Leave both dates empty for the whole trail. Newest first.</p>
    <div class="flex gap-3">
      <FormField class="flex-1" label="From">
        <input v-model="from" type="date" class="form-input" />
      </FormField>
      <FormField class="flex-1" label="To" hint="Includes the whole day.">
        <input v-model="to" type="date" class="form-input" />
      </FormField>
    </div>
    <FormField label="Format">
      <select v-model="format" class="form-select">
        <option value="csv">CSV (spreadsheet)</option>
        <option value="json">JSON (every field, as stored)</option>
      </select>
    </FormField>
    <template #footer>
      <button class="btn btn-secondary" @click="emit('close')">Cancel</button>
      <button class="btn btn-primary" :disabled="exporting" @click="run">
        {{ exporting ? 'Exporting...' : 'Export' }}
      </button>
    </template>
  </BaseModal>
</template>
