<script setup lang="ts">
// Who is in this project, and what each of them can reach.
//
// Its own card because members and invitations are two resources with
// two sets of routes, and they shared one 561-line file that also
// fetched the project, resolved permissions and drove the tabs.
//
// THREE controls that look alike and are not. A role is members:write. A
// removal is members:delete, which is narrower. Ownership is neither -
// it hands over deleting the project, so only an owner may grant it.
import { computed, ref } from 'vue'
import { projectApi } from '../../../api/projects'
import { apiErrorMessage } from '../../../api/client'
import type { ProjectMember, ProjectRole } from '../../../api/types'
import { useNotificationStore } from '../../../stores/notification'
import { useConfirm } from '../../../composables/useConfirm'
import { useFieldErrors } from '../../../composables/fieldErrors'
import { formatDate } from '../../../composables/formatDate'
import { roleOptions } from './roleOptions'
import EmptyState from '../../../components/EmptyState.vue'
import BaseModal from '../../../components/BaseModal.vue'
import FormField from '../../../components/FormField.vue'

const props = defineProps<{
  projectId: string
  members: ProjectMember[]
  roles: ProjectRole[]
  /** The role a member with none of their own carries, or null. */
  defaultRole: ProjectRole | null
  canManage: boolean
  canRemove: boolean
  /** Only an owner may grant or revoke ownership. */
  iAmOwner: boolean
}>()

const emit = defineEmits<{
  /** The list changed and the page should read it again. */
  (e: 'changed'): void
  /** Ownership changed, which may have been the reader's own. */
  (e: 'owner-changed'): void
}>()

const notify = useNotificationStore()
const { confirm } = useConfirm()
const { errors, capture, clear } = useFieldErrors()

const showAdd = ref(false)
const email = ref('')
const roleID = ref('')
const adding = ref(false)

const options = computed(() => roleOptions(props.roles, props.defaultRole))

/** The role a row is showing, or the reason there is none. */
function roleLabel(m: ProjectMember): string {
  if (m.role_name) return m.role_name

  return props.defaultRole ? props.defaultRole.name : 'no access'
}

function openAdd() {
  clear()
  email.value = ''
  roleID.value = ''
  showAdd.value = true
}

async function add() {
  clear()
  if (!email.value.trim()) return

  adding.value = true
  try {
    await projectApi.addMember(props.projectId, {
      email: email.value.trim(),
      role_id: roleID.value,
    })
    notify.success('Member added')
    showAdd.value = false
    emit('changed')
  } catch (err) {
    if (!capture(err)) notify.error(apiErrorMessage(err, 'Failed to add member'))
  } finally {
    adding.value = false
  }
}

async function updateRole(member: ProjectMember, id: string) {
  clear()
  try {
    await projectApi.updateMember(props.projectId, member.user_id, { role_id: id })
    notify.success(
      id
        ? 'Role assigned - it applies on their next request'
        : props.defaultRole
          ? `Role cleared - they carry the project default (${props.defaultRole.name})`
          : 'Role cleared - this project names no default, so they can reach nothing',
    )
  } catch (err) {
    if (!capture(err)) notify.error(apiErrorMessage(err, 'Failed to update the role'))
  }
  // Either way: the row on screen is showing what the reader picked, and
  // only the server knows whether it took.
  emit('changed')
}

async function updateOwner(member: ProjectMember, owner: boolean) {
  clear()
  try {
    await projectApi.updateMember(props.projectId, member.user_id, { owner })
    notify.success(owner ? 'Ownership granted' : 'Ownership revoked')
    // Revoking your own leaves you without the control you just used, so
    // the page has to re-read whose project this is.
    emit('owner-changed')
  } catch (err) {
    if (!capture(err)) notify.error(apiErrorMessage(err, 'Failed to change ownership'))
    emit('changed')
  }
}

async function remove(member: ProjectMember) {
  const ok = await confirm({
    title: 'Remove Member',
    message: `Remove ${member.email || member.user_id} from this project?`,
    confirmText: 'Remove',
    variant: 'danger',
  })
  if (!ok) return

  try {
    await projectApi.removeMember(props.projectId, member.user_id)
    notify.success('Member removed')
    emit('changed')
  } catch (err) {
    notify.error(apiErrorMessage(err, 'Failed to remove member'))
  }
}
</script>

<template>
  <div class="card">
    <div class="card-header">
      <h2>Members</h2>
      <button v-if="canManage" class="btn btn-primary btn-sm" @click="openAdd">Add Member</button>
    </div>

    <EmptyState v-if="members.length === 0" text="No members yet." />

    <div v-else class="table-wrapper">
      <table>
        <thead>
          <tr>
            <th>Email</th>
            <th>Role</th>
            <th>Owner</th>
            <th>Joined</th>
            <th v-if="canManage" class="text-right">Actions</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="m in members" :key="m.id">
            <td class="fw-medium">{{ m.email || m.user_id }}</td>
            <td>
              <!-- An owner's row shows no selector: a role would narrow
                   nothing, since ownership already reaches everything. -->
              <select
                v-if="canManage && !m.owner"
                :value="m.role_id && !m.inherited_role ? m.role_id : ''"
                class="form-select role-select"
                :title="
                  m.inherited_role
                    ? 'Carrying the project default'
                    : 'A role assigned to this member'
                "
                @change="updateRole(m, ($event.target as HTMLSelectElement).value)"
              >
                <option v-for="o in options" :key="o.value" :value="o.value">{{ o.label }}</option>
              </select>
              <span v-else-if="m.owner" class="text-sm text-muted">everything</span>
              <span v-else>
                <span class="badge" :class="m.role_name ? 'badge-neutral' : 'badge-warning'">
                  {{ roleLabel(m) }}
                </span>
                <span v-if="m.inherited_role" class="text-sm text-muted"> (default)</span>
              </span>
            </td>
            <td>
              <input
                v-if="iAmOwner"
                type="checkbox"
                :checked="m.owner"
                :aria-label="`${m.email || m.user_id} owns this project`"
                title="Owners may delete the project and rewrite its sign-on policy"
                @change="updateOwner(m, ($event.target as HTMLInputElement).checked)"
              />
              <span v-else-if="m.owner" class="badge badge-info">owner</span>
              <span v-else class="text-sm text-muted">-</span>
            </td>
            <td>{{ formatDate(m.created_at) }}</td>
            <td v-if="canManage">
              <div class="table-actions justify-end">
                <button v-if="canRemove" class="btn btn-danger btn-sm" @click="remove(m)">
                  Remove
                </button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <BaseModal v-if="showAdd" title="Add Member" form @submit="add" @close="showAdd = false">
      <FormField
        label="Email Address"
        :error="errors.email"
        hint="The user must already have an account. Create an invitation otherwise."
      >
        <input
          v-model="email"
          type="email"
          class="form-input"
          placeholder="user@example.com"
          required
        />
      </FormField>

      <FormField
        label="Role"
        :hint="
          !defaultRole && roles.length === 0
            ? 'This project has no roles yet, so a member added now can reach nothing. Create one under Roles first.'
            : 'Leave this on the default unless this person needs something narrower or wider.'
        "
      >
        <select v-model="roleID" class="form-select">
          <option v-for="o in options" :key="o.value" :value="o.value">{{ o.label }}</option>
        </select>
      </FormField>

      <template #footer>
        <button type="button" class="btn btn-secondary" @click="showAdd = false">Cancel</button>
        <button type="submit" class="btn btn-primary" :disabled="adding || !email.trim()">
          {{ adding ? 'Adding...' : 'Add Member' }}
        </button>
      </template>
    </BaseModal>
  </div>
</template>

<style scoped>
/* Narrow enough to sit in a table cell rather than stretching the
   column, and sized down to match the row's text. */
.role-select {
  width: auto;
  min-width: 110px;
  padding: 4px 28px 4px 8px;
  font-size: 13px;
}
</style>
