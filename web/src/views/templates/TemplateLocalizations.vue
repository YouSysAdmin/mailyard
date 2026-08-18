<script setup lang="ts">
// The content of one version, one language per row.
//
// Bound to the version the page has selected, so switching versions
// reloads it rather than the page rebuilding the card - which is why the
// load hangs off a watch and not off mount.
//
// There are three ways to write a localization and this card offers all
// of them: the quick dialog for a subject or a paste, the code editor
// for markup, and the visual builder for a layout. They write the same
// record, so the choice is the reader's.
import { ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { templatesApi, type LocalizationPayload } from '../../api/templates'
import { apiErrorMessage } from '../../api/client'
import type { Language, TemplateLocalization, TemplateVersion } from '../../api/types'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import { useConfirm } from '../../composables/useConfirm'
import { useFieldErrors } from '../../composables/fieldErrors'
import { formatDate } from '../../composables/formatDate'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import BaseModal from '../../components/BaseModal.vue'
import FormField from '../../components/FormField.vue'
import TemplatePreviewDialog from './TemplatePreviewDialog.vue'

const props = defineProps<{
  templateId: string
  version: TemplateVersion
  /** The template's default, marked in the list. */
  defaultLanguage: string
  /** The project's configured languages, offered when adding one. */
  languages: Language[]
  /** What the preview fills the template with. */
  sampleData?: string
}>()

const emit = defineEmits<{ (e: 'languages', codes: string[]): void }>()

const router = useRouter()
const notify = useNotificationStore()
const projects = useProjectStore()
const { confirm } = useConfirm()
const { errors, capture, clear } = useFieldErrors()

const rows = ref<TemplateLocalization[]>([])
const loading = ref(true)

const editor = ref<{ existing: TemplateLocalization | null; form: LocalizationPayload } | null>(
  null,
)
const saving = ref(false)

const previewing = ref('')

function publish() {
  emit(
    'languages',
    rows.value.map((l) => l.language),
  )
}

async function load() {
  loading.value = true
  try {
    const res = await templatesApi.listLocalizations(props.templateId, props.version.id)
    rows.value = res.data.localizations ?? []
  } catch (e) {
    rows.value = []
    notify.error(apiErrorMessage(e, 'Failed to load the localizations'))
  } finally {
    loading.value = false
    publish()
  }
}

function add() {
  clear()
  editor.value = { existing: null, form: { language: '', subject: '', html: '', text: '' } }
}

function edit(l: TemplateLocalization) {
  clear()
  editor.value = {
    existing: l,
    form: { language: l.language, subject: l.subject, html: l.html || '', text: l.text || '' },
  }
}

async function save() {
  const open = editor.value
  if (!open) return

  clear()
  saving.value = true
  try {
    // PUT upserts on (version, language), so the same call serves both
    // the add and the edit - which is why one dialog does both.
    const res = await templatesApi.putLocalization(props.templateId, props.version.id, open.form)
    const stored = res.data.localization
    const known = rows.value.some((l) => l.language === stored.language)
    rows.value = known
      ? rows.value.map((l) => (l.language === stored.language ? stored : l))
      : [...rows.value, stored]
    editor.value = null
    publish()
    notify.success(known ? 'Localization updated' : 'Localization added')
  } catch (e) {
    if (!capture(e)) {
      notify.error(apiErrorMessage(e, open.existing ? 'Failed to save it' : 'Failed to add it'))
    }
  } finally {
    saving.value = false
  }
}

async function remove(l: TemplateLocalization) {
  const confirmed = await confirm({
    title: 'Delete localization',
    message: `Delete the "${l.language}" content of this version? This cannot be undone.`,
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return

  try {
    await templatesApi.deleteLocalization(props.templateId, l.id)
    rows.value = rows.value.filter((x) => x.id !== l.id)
    publish()
    notify.success('Localization deleted')
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to delete the localization'))
  }
}

/** Open one of the two full-page writing surfaces on this language. */
function openIn(name: 'template-editor' | 'template-builder', l: TemplateLocalization) {
  router.push({
    name,
    params: { id: props.templateId, versionId: props.version.id },
    query: { lang: l.language },
  })
}

watch(() => props.version.id, load, { immediate: true })
</script>

<template>
  <div class="card">
    <div class="card-header">
      <h2>Content - v{{ version.version }}</h2>

      <button v-if="projects.can('templates:write')" class="btn btn-primary btn-sm" @click="add">
        Add language
      </button>
    </div>

    <LoadingBlock v-if="loading" />

    <EmptyState
      v-else-if="rows.length === 0"
      text="Nothing written yet. Add a language to give this version a subject and a body."
    />

    <div v-else class="table-wrapper">
      <table>
        <thead>
          <tr>
            <th>Language</th>
            <th>Subject</th>
            <th>Updated</th>
            <th class="text-right"></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="l in rows" :key="l.id">
            <td>
              <strong>{{ l.language }}</strong>
              <span v-if="l.language === defaultLanguage" class="badge badge-info ml-2">
                default
              </span>
            </td>
            <td class="truncate w-search">{{ l.subject }}</td>
            <td>{{ formatDate(l.updated_at || l.created_at) }}</td>
            <td class="text-right">
              <div class="flex gap-2">
                <button class="btn btn-secondary btn-sm" @click="previewing = l.language">
                  Preview
                </button>
                <template v-if="projects.can('templates:write')">
                  <button class="btn btn-secondary btn-sm" @click="openIn('template-editor', l)">
                    Editor
                  </button>
                  <button class="btn btn-secondary btn-sm" @click="openIn('template-builder', l)">
                    Builder
                  </button>
                  <button class="btn btn-secondary btn-sm" @click="edit(l)">Quick edit</button>
                </template>
                <button
                  v-if="projects.can('templates:delete')"
                  class="btn btn-danger btn-sm"
                  @click="remove(l)"
                >
                  Delete
                </button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <BaseModal v-if="editor" size="modal-w720" @close="editor = null">
      <template #header>
        <h3>
          {{ editor.existing ? `Edit ${editor.existing.language}` : 'Add a language' }}
        </h3>
      </template>

      <!-- The language keys the record, so an existing one is shown
           rather than offered: changing it would write a second row. -->
      <FormField
        v-if="!editor.existing"
        label="Language"
        :error="errors.language"
        hint="From the languages configured for this project."
      >
        <select v-if="languages.length" v-model="editor.form.language" class="form-select">
          <option value="" disabled>Pick a language</option>
          <option v-for="lang in languages" :key="lang.id" :value="lang.code">
            {{ lang.name }} ({{ lang.code }})
          </option>
        </select>
        <input v-else v-model="editor.form.language" class="form-input" placeholder="en" />
      </FormField>

      <FormField label="Subject" :error="errors.subject">
        <input v-model="editor.form.subject" class="form-input" placeholder="Welcome {{name}}!" />
      </FormField>

      <FormField label="HTML" :error="errors.html">
        <textarea
          v-model="editor.form.html"
          class="form-textarea code-font"
          rows="8"
          placeholder="<html>...</html>"
        ></textarea>
      </FormField>

      <FormField label="Plain text" :error="errors.text">
        <textarea
          v-model="editor.form.text"
          class="form-textarea code-font"
          rows="4"
          placeholder="The same message, without markup..."
        ></textarea>
      </FormField>

      <template #footer>
        <button class="btn btn-secondary" @click="editor = null">Cancel</button>
        <button
          class="btn btn-primary"
          :disabled="saving || !editor.form.language.trim() || !editor.form.subject.trim()"
          @click="save"
        >
          {{ saving ? 'Saving...' : editor.existing ? 'Save' : 'Add' }}
        </button>
      </template>
    </BaseModal>

    <TemplatePreviewDialog
      v-if="previewing"
      :template-id="templateId"
      :version-id="version.id"
      :language="previewing"
      :sample-data="sampleData"
      @close="previewing = ''"
    />
  </div>
</template>
