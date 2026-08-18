<script setup lang="ts">
// CSS a template version can point at, inlined into the HTML it renders.
//
// Kept out of the template body because a mail client wants the styling
// ON the elements, and the same rules usually serve every template a
// project has - so writing them once and naming them from a version is
// both less to maintain and what the renderer wants.
import { ref } from 'vue'
import { stylesheetsApi } from '../../api/stylesheets'
import { apiErrorMessage } from '../../api/client'
import type { Stylesheet } from '../../api/types'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import { useConfirm } from '../../composables/useConfirm'
import { useClientPager } from '../../composables/usePagination'
import { useFieldErrors } from '../../composables/fieldErrors'
import { formatDate } from '../../composables/formatDate'
import Pagination from '../../components/Pagination.vue'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import PageHeader from '../../components/PageHeader.vue'
import BaseModal from '../../components/BaseModal.vue'
import FormField from '../../components/FormField.vue'

const notify = useNotificationStore()
const projects = useProjectStore()
const { confirm } = useConfirm()
const { errors, capture, clear } = useFieldErrors()

const sheets = ref<Stylesheet[]>([])
const loading = ref(true)

const { pageable, pageItems, goToPage } = useClientPager(sheets, 20)

const draft = ref<{ editing: Stylesheet | null; name: string; css: string } | null>(null)
const saving = ref(false)

async function load() {
  loading.value = true
  try {
    sheets.value = (await stylesheetsApi.list()).data.stylesheets ?? []
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load the stylesheets'))
  } finally {
    loading.value = false
  }
}

function open(sheet?: Stylesheet) {
  clear()
  draft.value = { editing: sheet ?? null, name: sheet?.name ?? '', css: sheet?.css ?? '' }
}

async function save() {
  const d = draft.value
  if (!d || !d.name.trim()) return

  clear()
  saving.value = true
  const body = { name: d.name.trim(), css: d.css }
  try {
    if (d.editing) await stylesheetsApi.update(d.editing.id, body)
    else await stylesheetsApi.create(body)

    draft.value = null
    notify.success(d.editing ? 'Stylesheet saved' : 'Stylesheet created')
    await load()
  } catch (e) {
    if (!capture(e)) {
      notify.error(apiErrorMessage(e, d.editing ? 'Failed to save' : 'Failed to create it'))
    }
  } finally {
    saving.value = false
  }
}

async function remove(sheet: Stylesheet) {
  const confirmed = await confirm({
    title: 'Delete stylesheet',
    message: `Delete "${sheet.name}"? Versions pointing at it keep sending, unstyled.`,
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return

  try {
    await stylesheetsApi.remove(sheet.id)
    notify.success('Stylesheet deleted')
    await load()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to delete the stylesheet'))
  }
}

void load()
</script>

<template>
  <div>
    <PageHeader title="Stylesheets">
      <button v-if="projects.can('templates:write')" class="btn btn-primary" @click="open()">
        New stylesheet
      </button>
    </PageHeader>

    <LoadingBlock v-if="loading" />

    <div v-else class="card">
      <EmptyState
        v-if="sheets.length === 0"
        title="No stylesheets yet"
        text="Write the CSS once here, then point a template version at it."
      />

      <template v-else>
        <div class="table-wrapper">
          <table>
            <thead>
              <tr>
                <th>Name</th>
                <th>Created</th>
                <th>Last changed</th>
                <th v-if="projects.can('templates:write')" class="text-right"></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="sheet in pageItems" :key="sheet.id">
                <td>
                  <strong>{{ sheet.name }}</strong>
                </td>
                <td>{{ formatDate(sheet.created_at) }}</td>
                <td>
                  <span v-if="sheet.updated_at">{{ formatDate(sheet.updated_at) }}</span>
                  <span v-else class="text-muted">-</span>
                </td>
                <td v-if="projects.can('templates:write')" class="text-right">
                  <div class="table-actions">
                    <button class="btn btn-secondary btn-sm" @click="open(sheet)">Edit</button>
                    <button
                      v-if="projects.can('templates:delete')"
                      class="btn btn-danger btn-sm"
                      @click="remove(sheet)"
                    >
                      Delete
                    </button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <Pagination :pageable="pageable" @page="goToPage" />
      </template>
    </div>

    <BaseModal
      v-if="draft"
      :title="draft.editing ? 'Edit stylesheet' : 'New stylesheet'"
      size="modal-w640"
      @close="draft = null"
    >
      <FormField label="Name" :error="errors.name">
        <input v-model="draft.name" class="form-input" placeholder="Brand styles" />
      </FormField>

      <!-- Not a `form` dialog: Enter inside the CSS box is a newline,
           and a submit-on-Enter would save a half-written rule. -->
      <FormField
        label="CSS"
        :error="errors.css"
        hint="Inlined onto the elements at render time, because that is what a mail client honours."
      >
        <textarea
          v-model="draft.css"
          class="form-textarea code-font"
          rows="14"
          spellcheck="false"
          placeholder="body { font-family: sans-serif; }"
        ></textarea>
      </FormField>

      <template #footer>
        <button class="btn btn-secondary" @click="draft = null">Cancel</button>
        <button class="btn btn-primary" :disabled="saving || !draft.name.trim()" @click="save">
          {{ saving ? 'Saving...' : draft.editing ? 'Save' : 'Create' }}
        </button>
      </template>
    </BaseModal>
  </div>
</template>
