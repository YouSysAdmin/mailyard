<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { permissionsApi, projectApi, type RolePayload } from '../../api/projects'
import { apiErrorMessage } from '../../api/client'
import type { PermissionResource, Project, ProjectRole } from '../../api/types'
import { useNotificationStore } from '../../stores/notification'
import { useConfirm } from '../../composables/useConfirm'
import PermissionGrid from '../../components/PermissionGrid.vue'
import LoadingBlock from '../../components/LoadingBlock.vue'
import PageHeader from '../../components/PageHeader.vue'
import BaseModal from '../../components/BaseModal.vue'
import FormField from '../../components/FormField.vue'
import { useFieldErrors } from '../../composables/fieldErrors'

const route = useRoute()
const router = useRouter()
const notify = useNotificationStore()
const { confirm } = useConfirm()

const projId = route.params.id as string
const proj = ref<Project | null>(null)
// From this page's own response, not the store: the path id may name
// a project other than the store's current one.
const perms = ref<string[]>([])
const loading = ref(true)

const { errors: fieldErrors, capture, clear } = useFieldErrors()

function can(p: string): boolean {
  return perms.value.includes('*') || perms.value.includes(p)
}
const canManage = computed(() => can('members:write'))
// Deleting a role is members:delete, and it is the narrower half:
// writing roles is how a project is set up, removing one changes what
// its holders can already do.
const canDelete = computed(() => can('members:delete'))

const roles = ref<ProjectRole[]>([])
// The catalogue comes from the server that enforces it, so the grid
// cannot offer a permission that does not exist.
const catalog = ref<PermissionResource[]>([])

// --- editor state ---
const showEditor = ref(false)
const editingId = ref('')
const formName = ref('')
const formDescription = ref('')
// The editor's truth: the "resource:action" strings PermissionGrid
// renders and edits. The grid owns the checkbox/policy-text sync and
// reports back when its text form will not parse.
const selected = ref<string[]>([])
const gridInvalid = ref(false)
const saving = ref(false)

function openCreate() {
  editingId.value = ''
  formName.value = ''
  formDescription.value = ''
  selected.value = []
  showEditor.value = true
}

function openEdit(r: ProjectRole) {
  editingId.value = r.id
  formName.value = r.name
  formDescription.value = r.description || ''
  selected.value = [...r.permissions]
  showEditor.value = true
}

async function save() {
  clear()
  if (gridInvalid.value) return
  saving.value = true
  const payload: RolePayload = {
    name: formName.value.trim(),
    description: formDescription.value.trim(),
    permissions: [...selected.value].sort(),
  }
  try {
    if (editingId.value) {
      await projectApi.updateRole(projId, editingId.value, payload)
      notify.success('Role updated - it applies to every holder on their next request')
    } else {
      await projectApi.createRole(projId, payload)
      notify.success('Role created')
    }
    showEditor.value = false
    await loadRoles()
  } catch (e) {
    if (!capture(e)) notify.error(apiErrorMessage(e, 'Failed to save the role'))
  } finally {
    saving.value = false
  }
}

async function removeRole(r: ProjectRole) {
  // Both refusals are re-checked by the server inside the DELETE.
  // These are the same messages, said before the round trip.
  if (r.default) {
    notify.error(`${r.name} is the project default - name a different default before deleting it`)
    return
  }
  if (r.members > 0) {
    notify.error(
      `${r.name} is assigned to ${r.members} member${r.members === 1 ? '' : 's'} - move them to another role first`,
    )
    return
  }
  const ok = await confirm({
    title: 'Delete role',
    message: `Delete the role "${r.name}"? Nobody carries it, so no access changes.`,
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!ok) return
  try {
    await projectApi.removeRole(projId, r.id)
    notify.success('Role deleted')
    await loadRoles()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to delete the role'))
  }
}

async function makeDefault(r: ProjectRole) {
  clear()
  try {
    await projectApi.setDefaultRole(projId, r.id)
    notify.success(`${r.name} is now the role members carry by default`)
    await loadRoles()
  } catch (e) {
    if (!capture(e)) notify.error(apiErrorMessage(e, 'Failed to set the default role'))
  }
}

async function loadRoles() {
  const res = await projectApi.listRoles(projId)
  roles.value = res.data.roles ?? []
}

onMounted(async () => {
  loading.value = true
  try {
    const [projRes, catRes] = await Promise.all([projectApi.get(projId), permissionsApi.catalog()])
    proj.value = projRes.data.project
    perms.value = projRes.data.permissions ?? []
    catalog.value = catRes.data.resources ?? []
    await loadRoles()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load roles'))
    router.push('/projects')
    return
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <div>
    <PageHeader>
      <template #title>
        <h1>Roles</h1>
        <p v-if="proj" class="text-sm text-muted">
          What people may do in {{ proj.name }}. There are no built-in roles - write the ones this
          project needs, and mark one as the default that members carry when nobody assigns them
          anything else.
        </p>
      </template>
      <button v-if="canManage" class="btn btn-primary" @click="openCreate">New Role</button>
    </PageHeader>

    <LoadingBlock v-if="loading" />

    <div v-else class="card">
      <div v-if="roles.length === 0" class="card-body">
        <p class="text-muted">
          No roles yet. Until this project has one and names it as the default, members other than
          owners can reach nothing here.
        </p>
      </div>
      <div v-else class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th>Name</th>
              <th>Permissions</th>
              <th>Members</th>
              <th class="text-right" v-if="canManage">Actions</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="r in roles" :key="r.id">
              <td>
                <strong>{{ r.name }}</strong>
                <span v-if="r.default" class="badge badge-info ml-2"> default </span>
                <div v-if="r.description" class="text-sm text-muted">{{ r.description }}</div>
              </td>
              <td>
                <span v-if="r.permissions.length === 0" class="badge badge-warning">
                  locked down - grants nothing
                </span>
                <span v-else class="text-sm">{{ r.permissions.length }} permissions</span>
              </td>
              <td>{{ r.members }}</td>
              <td class="text-right" v-if="canManage">
                <button
                  v-if="!r.default"
                  class="btn btn-secondary btn-sm"
                  title="Members with no role of their own will carry this one"
                  @click="makeDefault(r)"
                >
                  Make default
                </button>
                <button class="btn btn-secondary btn-sm" @click="openEdit(r)">Edit</button>
                <button
                  v-if="canDelete"
                  class="btn btn-danger btn-sm"
                  :disabled="r.members > 0 || r.default"
                  :title="
                    r.default
                      ? 'The project default - name a different one first'
                      : r.members > 0
                        ? 'Assigned to members - reassign them first'
                        : ''
                  "
                  @click="removeRole(r)"
                >
                  Delete
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- Editor modal -->
    <BaseModal
      v-if="showEditor"
      :title="editingId ? 'Edit Role' : 'New Role'"
      size="modal-w900"
      form
      @submit="save"
      @close="showEditor = false"
    >
      <FormField label="Name" :error="fieldErrors.name">
        <input v-model="formName" class="form-input" maxlength="100" required />
      </FormField>
      <FormField label="Description" :error="fieldErrors.description">
        <input
          v-model="formDescription"
          class="form-input"
          maxlength="500"
          placeholder="What this role is for"
        />
      </FormField>

      <PermissionGrid
        v-model="selected"
        :catalog="catalog"
        @update:invalid="gridInvalid = $event"
      />
      <template #footer>
        <button type="button" class="btn btn-secondary" @click="showEditor = false">Cancel</button>
        <button
          type="submit"
          class="btn btn-primary"
          :disabled="saving || !formName.trim() || gridInvalid"
        >
          {{ saving ? 'Saving...' : editingId ? 'Save' : 'Create Role' }}
        </button>
      </template>
    </BaseModal>
  </div>
</template>
