<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { templatesApi, type TemplatePayload } from '../../api/templates'
import { languagesApi } from '../../api/languages'
import { apiErrorMessage } from '../../api/client'
import type { Template, Language } from '../../api/types'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import { useConfirm } from '../../composables/useConfirm'
import { useClientPager } from '../../composables/usePagination'
import Pagination from '../../components/Pagination.vue'
import { formatDate } from '../../composables/formatDate'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import PageHeader from '../../components/PageHeader.vue'
import BaseModal from '../../components/BaseModal.vue'
import FormField from '../../components/FormField.vue'
import TemplateImport from './TemplateImport.vue'
import { useFieldErrors } from '../../composables/fieldErrors'

const router = useRouter()
const notify = useNotificationStore()
const projStore = useProjectStore()
const { confirm } = useConfirm()

const templates = ref<Template[]>([])
const languages = ref<Language[]>([])
const loading = ref(true)
const search = ref('')

const filtered = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return templates.value
  return templates.value.filter(
    (t) => t.name.toLowerCase().includes(q) || (t.description || '').toLowerCase().includes(q),
  )
})
const { pageable, pageItems, goToPage } = useClientPager(filtered, 20)

// Create/edit modal
const showModal = ref(false)
const editing = ref<Template | null>(null)
const saving = ref(false)
const form = ref<Required<TemplatePayload>>({
  name: '',
  description: '',
  default_language: 'en',
  sample_data: '',
})

// Import modal
const showImportModal = ref(false)

async function onImported() {
  showImportModal.value = false
  await loadTemplates()
}

const { errors: fieldErrors, capture, clear } = useFieldErrors()

async function loadTemplates() {
  loading.value = true
  try {
    const res = await templatesApi.list()
    templates.value = res.data.templates ?? []
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load templates'))
  } finally {
    loading.value = false
  }
}

async function loadLanguages() {
  try {
    const res = await languagesApi.list()
    languages.value = res.data.languages ?? []
  } catch {
    // Non-critical - the language field falls back to a text input.
  }
}

function defaultLanguageCode(): string {
  return languages.value.find((l) => l.is_default)?.code || 'en'
}

function resetForm() {
  form.value = {
    name: '',
    description: '',
    default_language: defaultLanguageCode(),
    sample_data: '',
  }
  editing.value = null
}

function openCreate() {
  resetForm()
  showModal.value = true
}

function openEdit(t: Template) {
  editing.value = t
  form.value = {
    name: t.name,
    description: t.description || '',
    default_language: t.default_language || 'en',
    sample_data: t.sample_data || '',
  }
  showModal.value = true
}

function closeModal() {
  showModal.value = false
  showImportModal.value = false
  resetForm()
}

function validSampleData(): boolean {
  if (!form.value.sample_data.trim()) return true
  try {
    JSON.parse(form.value.sample_data)
    return true
  } catch {
    return false
  }
}

async function saveTemplate() {
  if (!form.value.name.trim()) return
  if (!validSampleData()) {
    notify.error('Sample data must be valid JSON')
    return
  }
  saving.value = true
  try {
    if (editing.value) {
      await templatesApi.update(editing.value.id, form.value)
      notify.success('Template updated')
    } else {
      await templatesApi.create(form.value)
      notify.success('Template created')
    }
    closeModal()
    await loadTemplates()
  } catch (e) {
    notify.error(
      apiErrorMessage(e, editing.value ? 'Failed to update template' : 'Failed to create template'),
    )
  } finally {
    saving.value = false
  }
}

async function deleteTemplate(t: Template) {
  const confirmed = await confirm({
    title: 'Delete Template',
    message: `Are you sure you want to delete "${t.name}"? This action cannot be undone.`,
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return
  try {
    await templatesApi.remove(t.id)
    notify.success('Template deleted')
    await loadTemplates()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to delete template'))
  }
}

async function exportTemplate(t: Template) {
  try {
    const res = await templatesApi.export(t.id)
    const doc = res.data.export
    const blob = new Blob([JSON.stringify(doc, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `${doc.template.name}.json`
    a.click()
    URL.revokeObjectURL(url)
    notify.success('Template exported')
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to export template'))
  }
}

function openImport() {
  showImportModal.value = true
}

onMounted(() => {
  loadTemplates()
  loadLanguages()
})
</script>

<template>
  <div>
    <PageHeader title="Templates" />

    <div class="card">
      <div class="card-header">
        <input
          v-model="search"
          class="form-input w-search"
          placeholder="Search by name or description..."
          @input="goToPage(0)"
        />
        <div class="flex gap-2">
          <button
            v-if="projStore.can('templates:write')"
            class="btn btn-secondary"
            @click="openImport"
          >
            Import
          </button>
          <button
            v-if="projStore.can('templates:write')"
            class="btn btn-primary"
            @click="openCreate"
          >
            Create Template
          </button>
        </div>
      </div>

      <LoadingBlock v-if="loading" />

      <div v-else>
        <EmptyState v-if="filtered.length === 0">
          <h3>{{ search ? 'No Results' : 'No Templates' }}</h3>
          <p>
            {{
              search
                ? 'No templates match your search.'
                : 'Create your first template to reuse email layouts.'
            }}
          </p>
        </EmptyState>

        <template v-else>
          <div class="table-wrapper">
            <table>
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Language</th>
                  <th>Active Version</th>
                  <th>Created</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="tmpl in pageItems"
                  :key="tmpl.id"
                  class="row-clickable"
                  @click="router.push({ name: 'template-detail', params: { id: tmpl.id } })"
                >
                  <td>
                    <div>{{ tmpl.name }}</div>
                    <div v-if="tmpl.description" class="text-muted text-sm">
                      {{ tmpl.description }}
                    </div>
                  </td>
                  <td>
                    <span class="badge badge-neutral">{{ tmpl.default_language }}</span>
                  </td>
                  <td>
                    <span v-if="tmpl.active_version_id" class="badge badge-success">Active</span>
                    <span v-else class="text-muted">-</span>
                  </td>
                  <td>{{ formatDate(tmpl.created_at) }}</td>
                  <td>
                    <div class="flex gap-2">
                      <button
                        class="btn btn-primary btn-sm"
                        @click.stop="
                          router.push({ name: 'template-detail', params: { id: tmpl.id } })
                        "
                      >
                        Versions
                      </button>
                      <button
                        class="btn btn-secondary btn-sm"
                        @click.stop="
                          router.push({ name: 'template-preview', params: { id: tmpl.id } })
                        "
                      >
                        Preview
                      </button>
                      <button
                        v-if="projStore.can('templates:write')"
                        class="btn btn-secondary btn-sm"
                        @click.stop="openEdit(tmpl)"
                      >
                        Edit
                      </button>
                      <button class="btn btn-secondary btn-sm" @click.stop="exportTemplate(tmpl)">
                        Export
                      </button>
                      <button
                        v-if="projStore.can('templates:delete')"
                        class="btn btn-danger btn-sm"
                        @click.stop="deleteTemplate(tmpl)"
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
    </div>

    <!-- Create/Edit Template Modal -->
    <BaseModal
      v-if="showModal"
      :title="editing ? 'Edit Template' : 'Create Template'"
      size="modal-w560"
      @close="closeModal"
    >
      <FormField label="Name" :error="fieldErrors.name">
        <input v-model="form.name" class="form-input" placeholder="e.g. welcome-email" />
      </FormField>
      <FormField label="Description" :error="fieldErrors.description">
        <input
          v-model="form.description"
          class="form-input"
          placeholder="Optional short description"
        />
      </FormField>
      <FormField label="Default Language" hint="Used as the fallback for template sends">
        <select v-if="languages.length" v-model="form.default_language" class="form-select">
          <option v-for="lang in languages" :key="lang.id" :value="lang.code">
            {{ lang.name }} ({{ lang.code }})
          </option>
          <option
            v-if="!languages.some((l) => l.code === form.default_language)"
            :value="form.default_language"
          >
            {{ form.default_language }}
          </option>
        </select>
        <input v-else v-model="form.default_language" class="form-input" placeholder="en" />
      </FormField>
      <FormField
        label="Sample Data (JSON)"
        :error="fieldErrors.sample_data"
        hint="Default data used by editors and previews"
      >
        <textarea
          v-model="form.sample_data"
          class="form-textarea code-font"
          rows="5"
          placeholder='{"name": "John", "company": "Acme"}'
        ></textarea>
      </FormField>
      <template #footer>
        <button class="btn btn-secondary" @click="closeModal">Cancel</button>
        <button
          class="btn btn-primary"
          :disabled="saving || !form.name.trim()"
          @click="saveTemplate"
        >
          {{ saving ? 'Saving...' : editing ? 'Update' : 'Create' }}
        </button>
      </template>
    </BaseModal>

    <TemplateImport
      v-if="showImportModal"
      @imported="onImported"
      @close="showImportModal = false"
    />
  </div>
</template>
