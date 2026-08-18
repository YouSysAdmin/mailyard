<script setup lang="ts">
// A JSON snapshot of everything this project holds.
//
// NOT role-gated, matching the server: any member may export their own
// project. Like usage it reads the ACTIVE project through the injected
// header, so it is only offered when this page is showing that project.
import { ref } from 'vue'
import { dataApi, exportCounts } from '../../../api/data'
import { apiErrorMessage } from '../../../api/client'
import { useNotificationStore } from '../../../stores/notification'
import { dateStamp, downloadText } from '../../../composables/download'
import FigureRows, { type Figure } from './FigureRows.vue'

const props = defineProps<{
  /** Names the file, so a folder of exports says which is which. */
  slug: string
  isActiveProject: boolean
}>()

const notify = useNotificationStore()

const exporting = ref(false)
const written = ref<Figure[] | null>(null)

async function run() {
  exporting.value = true
  try {
    const doc = (await dataApi.export()).data.export
    downloadText(
      `mailyard-export-${props.slug || 'project'}-${dateStamp()}.json`,
      'application/json',
      JSON.stringify(doc, null, 2),
    )
    written.value = exportCounts(doc).map((c) => ({ label: c.label, value: String(c.count) }))

    // A capped section is REPORTED rather than left to be discovered by
    // counting rows in the file - an export that silently stops at a
    // ceiling is worse than one that failed.
    if (doc.truncated?.length) {
      notify.error(
        `Export downloaded, but these sections hit their ceiling and are incomplete: ${doc.truncated.join(', ')}`,
      )
    } else {
      notify.success('Export downloaded')
    }
  } catch (err) {
    notify.error(apiErrorMessage(err, 'Failed to export project data'))
  } finally {
    exporting.value = false
  }
}
</script>

<template>
  <div class="card">
    <div class="card-header">
      <h2>Data Export</h2>
    </div>

    <div class="card-body">
      <p v-if="!isActiveProject" class="form-hint">Switch to this project to export its data.</p>

      <template v-else>
        <p class="form-hint mb-3">
          Downloads a JSON snapshot of this project - templates, contacts, subscribers,
          suppressions, webhooks, domains, and sender addresses. Passwords, API keys, and other
          secrets are never included, so the file is safe to hand to whoever asked for it.
        </p>

        <button class="btn btn-secondary" :disabled="exporting" @click="run">
          {{ exporting ? 'Preparing...' : 'Export Project Data' }}
        </button>

        <FigureRows v-if="written" class="mt-4" :rows="written" />
      </template>
    </div>
  </div>
</template>
