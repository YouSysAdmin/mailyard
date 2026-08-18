<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { projectApi } from '../../api/projects'
import { apiErrorMessage } from '../../api/client'
import type { Project } from '../../api/types'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import { formatDate } from '../../composables/formatDate'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import PageHeader from '../../components/PageHeader.vue'
import BaseModal from '../../components/BaseModal.vue'
import FormField from '../../components/FormField.vue'
import { useFieldErrors } from '../../composables/fieldErrors'

const route = useRoute()
const router = useRouter()
const notify = useNotificationStore()
const projStore = useProjectStore()

const loading = ref(true)

const showCreateModal = ref(false)
const newName = ref('')
const newDescription = ref('')
const newLanguage = ref('en')
const creating = ref(false)

const { errors: fieldErrors, capture, clear } = useFieldErrors()

async function fetchData() {
  loading.value = true
  try {
    await projStore.fetchProjects()
  } finally {
    loading.value = false
  }
}

function openCreateModal() {
  newName.value = ''
  newDescription.value = ''
  newLanguage.value = 'en'
  showCreateModal.value = true
}

async function createProject() {
  clear()
  if (!newName.value.trim()) return
  creating.value = true
  try {
    await projectApi.create({
      name: newName.value.trim(),
      description: newDescription.value.trim(),
      default_language: newLanguage.value.trim(),
    })
    notify.success('Project created')
    showCreateModal.value = false
    await projStore.fetchProjects(true)
  } catch (err) {
    if (!capture(err)) notify.error(apiErrorMessage(err, 'Failed to create project'))
  } finally {
    creating.value = false
  }
}

async function switchToProject(proj: Project) {
  await projStore.setProject(proj.id)
  router.push('/')
}

onMounted(fetchData)

// The sidebar project switcher links here with ?create=1 to open the
// create modal directly.
//
// A WATCHER, not a one-off read. Clicking Create project while already
// standing on this page changes only the query, so vue-router reuses
// the component and <script setup> never runs again: the URL gained
// ?create=1 and nothing opened. immediate covers arriving from
// elsewhere.
//
// GATED, like the button. ?create=1 is a URL anybody can type, and a
// dialog whose save answers 403 is a worse answer than no dialog. The
// query is cleared either way, so a link that did nothing does not sit
// there re-firing on the next navigation.
watch(
  () => route.query.create,
  (create) => {
    if (create === undefined) return
    if (projStore.canCreateProjects) openCreateModal()
    router.replace({ query: {} })
  },
  { immediate: true },
)
</script>

<template>
  <div>
    <PageHeader title="Projects">
      <button v-if="projStore.canCreateProjects" class="btn btn-primary" @click="openCreateModal">
        Create Project
      </button>
    </PageHeader>

    <LoadingBlock v-if="loading" />

    <div v-else class="card">
      <!-- The empty state has to say something different when there is
           no button beside it. "Create a project to share resources
           with your team" in front of an account that may not create
           one describes a control that is not there, and the reader is
           left looking for it. -->
      <EmptyState
        v-if="projStore.projects.length === 0"
        title="No projects"
        :text="
          projStore.canCreateProjects
            ? 'Create a project to share resources with your team.'
            : 'Projects are created by an administrator on this installation. You will see one here once you are invited to it.'
        "
      />

      <div v-else class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th>Name</th>
              <th>Slug</th>
              <th>Language</th>
              <th>Created</th>
              <th class="text-right">Actions</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="proj in projStore.projects" :key="proj.id">
              <td>
                <div class="flex items-center gap-2">
                  <router-link :to="`/projects/${proj.id}`" class="cell-link">
                    {{ proj.name }}
                  </router-link>
                </div>
                <div v-if="proj.description" class="proj-description">{{ proj.description }}</div>
              </td>
              <td>
                <code>{{ proj.slug }}</code>
              </td>
              <td>{{ proj.default_language }}</td>
              <td>{{ formatDate(proj.created_at) }}</td>
              <td>
                <div class="table-actions justify-end">
                  <button
                    class="btn btn-primary btn-sm"
                    :disabled="projStore.currentProjectId === proj.id"
                    @click="switchToProject(proj)"
                  >
                    {{ projStore.currentProjectId === proj.id ? 'Active' : 'Switch' }}
                  </button>
                  <!-- The same permissions the two routes declare, asked
                       of THIS row's project rather than the active one.
                       Both pages refuse without them, so offering the
                       button was offering a 403. -->
                  <button
                    v-if="projStore.canIn(proj.id, 'settings:read')"
                    class="btn btn-secondary btn-sm"
                    @click="router.push(`/projects/${proj.id}`)"
                  >
                    Settings
                  </button>
                  <button
                    v-if="projStore.canIn(proj.id, 'members:read')"
                    class="btn btn-secondary btn-sm"
                    @click="router.push(`/projects/${proj.id}/members`)"
                  >
                    Members
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- Create Project Modal -->
    <BaseModal
      v-if="showCreateModal"
      title="Create Project"
      form
      @submit="createProject"
      @close="showCreateModal = false"
    >
      <FormField label="Name" :error="fieldErrors.name">
        <input v-model="newName" class="form-input" placeholder="My Team" required />
      </FormField>
      <FormField label="Description (optional)" :error="fieldErrors.description">
        <input
          v-model="newDescription"
          class="form-input"
          placeholder="What is this project for?"
        />
      </FormField>
      <FormField
        label="Default Language"
        :error="fieldErrors.default_language"
        hint="Language code such as en, de, or fr."
      >
        <input v-model="newLanguage" class="form-input" placeholder="en" maxlength="10" />
      </FormField>
      <template #footer>
        <button type="button" class="btn btn-secondary" @click="showCreateModal = false">
          Cancel
        </button>
        <button type="submit" class="btn btn-primary" :disabled="creating || !newName.trim()">
          {{ creating ? 'Creating...' : 'Create' }}
        </button>
      </template>
    </BaseModal>
  </div>
</template>

<style scoped>
.proj-description {
  font-size: 12px;
  color: var(--text-muted);
  margin-top: 2px;
}
</style>
