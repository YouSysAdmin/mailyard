<script setup lang="ts">
// The version lifecycle of one template: create, edit, activate, delete.
//
// It owns the list rather than taking it as a prop, because every one of
// those four writes it - a prop the child mutates is the shape that
// makes it impossible to say later who decided the row is gone. The
// page reads the list back through `versions` for its summary card, and
// the selected row through `select`, so the traffic runs one way.
import { computed, ref, watch } from 'vue'
import { templatesApi } from '../../api/templates'
import { apiErrorMessage } from '../../api/client'
import type { Stylesheet, TemplateVersion } from '../../api/types'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import { useConfirm } from '../../composables/useConfirm'
import { useFieldErrors } from '../../composables/fieldErrors'
import { formatDate } from '../../composables/formatDate'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import BaseModal from '../../components/BaseModal.vue'
import FormField from '../../components/FormField.vue'

const props = defineProps<{
  templateId: string
  stylesheets: Stylesheet[]
  /** The version the template currently sends. */
  activeId: string
  /** The template's own sample data, which a new version starts from. */
  defaultSampleData?: string
}>()

const emit = defineEmits<{
  (e: 'versions', list: TemplateVersion[]): void
  (e: 'select', version: TemplateVersion | null): void
  (e: 'activate', versionId: string): void
}>()

const notify = useNotificationStore()
const projects = useProjectStore()
const { confirm } = useConfirm()
const { errors, capture, clear } = useFieldErrors()

const versions = ref<TemplateVersion[]>([])
const loading = ref(true)
const selectedId = ref('')

const creating = ref(false)
const newStylesheetId = ref('')

const editing = ref<TemplateVersion | null>(null)
const form = ref({ stylesheet_id: '', sample_data: '' })
const saving = ref(false)

/** Stylesheet names by id, so a row does not scan the list per cell. */
const styleNames = computed(() => new Map(props.stylesheets.map((s) => [s.id, s.name])))

/**
 * Announce the list and the selection together.
 *
 * Every write ends here, which is what keeps the page from ever seeing
 * a list that no longer holds the row it thinks is selected.
 */
function publish(selected: TemplateVersion | null) {
  selectedId.value = selected?.id ?? ''
  emit('versions', versions.value)
  emit('select', selected)
}

/** The version to land on: the active one, else the newest. */
function landing(): TemplateVersion | null {
  return versions.value.find((v) => v.id === props.activeId) ?? versions.value[0] ?? null
}

async function load() {
  loading.value = true
  try {
    versions.value = (await templatesApi.listVersions(props.templateId)).data.versions ?? []
    publish(landing())
  } catch (e) {
    versions.value = []
    publish(null)
    notify.error(apiErrorMessage(e, 'Failed to load the versions'))
  } finally {
    loading.value = false
  }
}

function select(v: TemplateVersion) {
  if (v.id === selectedId.value) return

  publish(v)
}

async function create() {
  clear()
  creating.value = true
  try {
    const res = await templatesApi.createVersion(props.templateId, {
      stylesheet_id: newStylesheetId.value || undefined,
      sample_data: props.defaultSampleData || undefined,
    })
    const made = res.data.version
    // Newest first, matching what the list endpoint answers.
    versions.value = [made, ...versions.value]
    newStylesheetId.value = ''
    publish(made)
    notify.success(`Version ${made.version} created`)
  } catch (e) {
    if (!capture(e)) notify.error(apiErrorMessage(e, 'Failed to create the version'))
  } finally {
    creating.value = false
  }
}

function edit(v: TemplateVersion) {
  clear()
  editing.value = v
  form.value = { stylesheet_id: v.stylesheet_id || '', sample_data: v.sample_data || '' }
}

async function save() {
  const target = editing.value
  if (!target) return

  clear()
  if (form.value.sample_data.trim() && !parses(form.value.sample_data)) {
    notify.error('Sample data must be valid JSON')

    return
  }

  saving.value = true
  try {
    const res = await templatesApi.updateVersion(props.templateId, target.id, {
      stylesheet_id: form.value.stylesheet_id || undefined,
      sample_data: form.value.sample_data || undefined,
    })
    const saved = res.data.version
    versions.value = versions.value.map((v) => (v.id === saved.id ? saved : v))
    // Re-announce so the localizations below read the new sample data.
    publish(selectedId.value === saved.id ? saved : landing())
    editing.value = null
    notify.success('Version updated')
  } catch (e) {
    if (!capture(e)) notify.error(apiErrorMessage(e, 'Failed to update the version'))
  } finally {
    saving.value = false
  }
}

async function activate(v: TemplateVersion) {
  try {
    await templatesApi.activate(props.templateId, v.id)
    emit('activate', v.id)
    notify.success(`Version ${v.version} activated`)
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to activate the version'))
  }
}

async function remove(v: TemplateVersion) {
  // Refused here as well as by the server, because the reason is worth
  // stating before somebody confirms a delete that cannot happen.
  if (v.id === props.activeId) {
    notify.error('The active version cannot be deleted - activate another one first')

    return
  }

  const confirmed = await confirm({
    title: 'Delete version',
    message: `Delete version ${v.version}? Its localizations go with it.`,
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return

  try {
    await templatesApi.deleteVersion(props.templateId, v.id)
    versions.value = versions.value.filter((x) => x.id !== v.id)
    publish(
      selectedId.value === v.id
        ? landing()
        : (versions.value.find((x) => x.id === selectedId.value) ?? null),
    )
    notify.success('Version deleted')
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to delete the version'))
  }
}

/** Whether a string is JSON, without keeping what it parsed to. */
function parses(s: string): boolean {
  try {
    JSON.parse(s)

    return true
  } catch {
    return false
  }
}

watch(() => props.templateId, load, { immediate: true })
</script>

<template>
  <div class="card">
    <div class="card-header">
      <h2>Versions</h2>

      <div v-if="projects.can('templates:write')" class="new-version">
        <select v-model="newStylesheetId" class="form-select stylesheet-pick">
          <option value="">No stylesheet</option>
          <option v-for="s in stylesheets" :key="s.id" :value="s.id">{{ s.name }}</option>
        </select>
        <button class="btn btn-primary btn-sm" :disabled="creating" @click="create">
          {{ creating ? 'Creating...' : 'New version' }}
        </button>
      </div>
    </div>

    <LoadingBlock v-if="loading" />

    <EmptyState
      v-else-if="versions.length === 0"
      text="No versions yet. Create one to start adding localizations."
    />

    <div v-else class="table-wrapper">
      <table>
        <thead>
          <tr>
            <th>Version</th>
            <th>Stylesheet</th>
            <th>Created</th>
            <th>State</th>
            <th class="text-right"></th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="v in versions"
            :key="v.id"
            class="row-clickable"
            :class="{ picked: v.id === selectedId }"
            @click="select(v)"
          >
            <td>
              <strong>v{{ v.version }}</strong>
            </td>
            <td>
              <span v-if="v.stylesheet_id" class="badge badge-neutral">
                {{ styleNames.get(v.stylesheet_id) ?? 'Unknown' }}
              </span>
              <span v-else class="text-muted">-</span>
            </td>
            <td>{{ formatDate(v.created_at) }}</td>
            <td>
              <span v-if="v.id === activeId" class="badge badge-success">Active</span>
              <span v-else class="badge badge-neutral">Draft</span>
            </td>
            <td class="text-right">
              <!-- The row itself selects, so the controls in it must not
                   also fire that. -->
              <div class="flex gap-2" @click.stop>
                <button
                  v-if="projects.can('templates:write')"
                  class="btn btn-secondary btn-sm"
                  @click="edit(v)"
                >
                  Edit
                </button>
                <button
                  v-if="projects.can('templates:write') && v.id !== activeId"
                  class="btn btn-primary btn-sm"
                  @click="activate(v)"
                >
                  Activate
                </button>
                <button
                  v-if="projects.can('templates:delete') && v.id !== activeId"
                  class="btn btn-danger btn-sm"
                  @click="remove(v)"
                >
                  Delete
                </button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <BaseModal v-if="editing" size="modal-w480" @close="editing = null">
      <template #header>
        <h3>Edit version v{{ editing.version }}</h3>
      </template>

      <FormField
        label="Stylesheet"
        :error="errors.stylesheet_id"
        hint="Its CSS is inlined into the rendered HTML."
      >
        <select v-model="form.stylesheet_id" class="form-select">
          <option value="">No stylesheet</option>
          <option v-for="s in stylesheets" :key="s.id" :value="s.id">{{ s.name }}</option>
        </select>
      </FormField>

      <FormField
        label="Sample data (JSON)"
        :error="errors.sample_data"
        hint="What the preview and the test send fill the template with."
      >
        <textarea
          v-model="form.sample_data"
          class="form-textarea code-font"
          rows="5"
          placeholder='{"name": "John"}'
        ></textarea>
      </FormField>

      <template #footer>
        <button class="btn btn-secondary" @click="editing = null">Cancel</button>
        <button class="btn btn-primary" :disabled="saving" @click="save">
          {{ saving ? 'Saving...' : 'Save' }}
        </button>
      </template>
    </BaseModal>
  </div>
</template>

<style scoped>
.new-version {
  display: flex;
  align-items: center;
  gap: 8px;
}

/* Narrow enough to sit in a header beside a button, since a stylesheet
   name is short and the select is a secondary control here. */
.stylesheet-pick {
  max-width: 180px;
  padding: 4px 8px;
  font-size: 13px;
}

/* Which row the localizations below belong to. It is the only thing on
   screen saying so, so it reads as a fill rather than an outline. */
.picked {
  background: var(--bg-tertiary);
}
</style>
