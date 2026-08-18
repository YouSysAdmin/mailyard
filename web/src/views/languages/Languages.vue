<script setup lang="ts">
// The languages this project writes templates in.
//
// A short list somebody types by hand, so it is fetched whole and paged
// in the browser. One of them is the DEFAULT, which is what a send falls
// back to when it asks for a language the template has no content for.
import { ref } from 'vue'
import { languagesApi } from '../../api/languages'
import { apiErrorMessage } from '../../api/client'
import type { Language } from '../../api/types'
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

const languages = ref<Language[]>([])
const loading = ref(true)
const promoting = ref(false)

const { pageable, pageItems, goToPage } = useClientPager(languages, 20)

// One ref for the whole dialog: whether it is open, whether it is an
// edit, and what it holds. It was four - showModal, editing, form and a
// resetForm to put them back - which is four things to keep agreeing.
const draft = ref<{
  editing: Language | null
  code: string
  name: string
  fallback: boolean
} | null>(null)
const saving = ref(false)

async function load() {
  loading.value = true
  try {
    languages.value = (await languagesApi.list()).data.languages ?? []
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load the languages'))
  } finally {
    loading.value = false
  }
}

function open(lang?: Language) {
  clear()
  draft.value = {
    editing: lang ?? null,
    code: lang?.code ?? '',
    name: lang?.name ?? '',
    fallback: lang?.is_default ?? false,
  }
}

async function save() {
  const d = draft.value
  if (!d || !d.code.trim() || !d.name.trim()) return

  clear()
  saving.value = true
  const body = { code: d.code.trim(), name: d.name.trim(), is_default: d.fallback }
  try {
    if (d.editing) await languagesApi.update(d.editing.id, body)
    else await languagesApi.create(body)

    draft.value = null
    notify.success(d.editing ? 'Language updated' : 'Language added')
    await load()
  } catch (e) {
    // capture() places a server field error on the control it names. It
    // was missing here, so a refused code arrived as a toast in the
    // corner while the dialog stayed open saying nothing.
    if (!capture(e)) {
      notify.error(apiErrorMessage(e, d.editing ? 'Failed to save' : 'Failed to add it'))
    }
  } finally {
    saving.value = false
  }
}

/** Move the fallback to this language. */
async function promote(lang: Language) {
  if (lang.is_default) return

  promoting.value = true
  try {
    // A PUT replaces the record, so the unchanged fields go with it.
    await languagesApi.update(lang.id, { code: lang.code, name: lang.name, is_default: true })
    notify.success(`${lang.name} is now the fallback`)
    await load()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to change the fallback'))
  } finally {
    promoting.value = false
  }
}

async function remove(lang: Language) {
  const confirmed = await confirm({
    title: 'Delete language',
    message: `Delete ${lang.name} (${lang.code})? Template content already written in it stays, but nothing will select it.`,
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return

  try {
    await languagesApi.remove(lang.id)
    notify.success('Language deleted')
    await load()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to delete the language'))
  }
}

void load()
</script>

<template>
  <div>
    <PageHeader title="Languages">
      <button v-if="projects.can('templates:write')" class="btn btn-primary" @click="open()">
        Add language
      </button>
    </PageHeader>

    <LoadingBlock v-if="loading" />

    <div v-else class="card">
      <EmptyState
        v-if="languages.length === 0"
        title="No languages yet"
        text="Add one for every language your templates are written in. The first becomes the fallback."
      />

      <template v-else>
        <div class="table-wrapper">
          <table>
            <thead>
              <tr>
                <th>Code</th>
                <th>Name</th>
                <th>Fallback</th>
                <th>Added</th>
                <th v-if="projects.can('templates:write')" class="text-right"></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="lang in pageItems" :key="lang.id">
                <td>
                  <span class="badge badge-neutral">{{ lang.code }}</span>
                </td>
                <td>{{ lang.name }}</td>
                <td>
                  <span v-if="lang.is_default" class="badge badge-success">Fallback</span>
                  <span v-else class="text-muted">-</span>
                </td>
                <td>{{ formatDate(lang.created_at) }}</td>
                <td v-if="projects.can('templates:write')" class="text-right">
                  <div class="table-actions">
                    <button class="btn btn-secondary btn-sm" @click="open(lang)">Edit</button>
                    <button
                      v-if="!lang.is_default"
                      class="btn btn-secondary btn-sm"
                      :disabled="promoting"
                      @click="promote(lang)"
                    >
                      Make fallback
                    </button>
                    <!-- The fallback has no Delete: removing it would
                         leave a project with content and nothing to fall
                         back to. Promote another one first. -->
                    <button
                      v-if="projects.can('templates:delete') && !lang.is_default"
                      class="btn btn-danger btn-sm"
                      @click="remove(lang)"
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
      :title="draft.editing ? 'Edit language' : 'Add a language'"
      size="modal-w480"
      form
      @submit="save"
      @close="draft = null"
    >
      <FormField
        label="Code"
        :error="errors.code"
        hint="An ISO 639-1 code - what a send asks for by name."
      >
        <input v-model="draft.code" class="form-input" placeholder="en" maxlength="10" required />
      </FormField>

      <FormField label="Name" :error="errors.name" hint="What this console shows in a picker.">
        <input v-model="draft.name" class="form-input" placeholder="English" required />
      </FormField>

      <!-- No error slot: a checkbox can only send true or false, so there
           is nothing here the server refuses by field. -->
      <FormField
        hint="Where a send lands when it asks for a language the template has no content for. Only one language can be it."
      >
        <label class="checkbox-label">
          <input v-model="draft.fallback" type="checkbox" />
          Use as the fallback
        </label>
      </FormField>

      <template #footer>
        <button type="button" class="btn btn-secondary" @click="draft = null">Cancel</button>
        <button
          type="submit"
          class="btn btn-primary"
          :disabled="saving || !draft.code.trim() || !draft.name.trim()"
        >
          {{ saving ? 'Saving...' : draft.editing ? 'Save' : 'Add' }}
        </button>
      </template>
    </BaseModal>
  </div>
</template>
