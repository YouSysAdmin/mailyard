<script setup lang="ts">
// People asked to join who do not have an account here yet.
//
// A separate resource from members, with its own routes, and behind
// members:write - which is why listing them is not attempted at all for
// a reader who cannot manage them.
//
// The TOKEN is returned exactly once, on create. Whether the invitee
// also received it by mail depends on whether the installation has
// platform mail configured, and the server says which - so the dialog
// reports what actually happened rather than guessing.
import { computed, ref } from 'vue'
import { projectApi } from '../../../api/projects'
import { apiErrorMessage } from '../../../api/client'
import type { ProjectInvitation, ProjectRole } from '../../../api/types'
import { useNotificationStore } from '../../../stores/notification'
import { useConfirm } from '../../../composables/useConfirm'
import { useFieldErrors } from '../../../composables/fieldErrors'
import { formatDate, isPast } from '../../../composables/formatDate'
import { roleOptions } from './roleOptions'
import EmptyState from '../../../components/EmptyState.vue'
import BaseModal from '../../../components/BaseModal.vue'
import FormField from '../../../components/FormField.vue'
import CopyButton from '../../../components/CopyButton.vue'

const props = defineProps<{
  projectId: string
  invitations: ProjectInvitation[]
  roles: ProjectRole[]
  defaultRole: ProjectRole | null
  /** members:delete, which revoking needs and creating does not. */
  canRemove: boolean
}>()

const emit = defineEmits<{ (e: 'changed'): void }>()

const notify = useNotificationStore()
const { confirm } = useConfirm()
const { errors, capture, clear } = useFieldErrors()

const showCreate = ref(false)
const email = ref('')
const roleID = ref('')
const creating = ref(false)

// What create answered with, shown once and never readable again.
const link = ref('')
const emailed = ref(false)
const invited = ref('')

const options = computed(() => roleOptions(props.roles, props.defaultRole))

/** Pending and past its expiry is not "pending" to a reader. */
function label(inv: ProjectInvitation): string {
  return inv.status === 'pending' && isPast(inv.expires_at) ? 'expired' : inv.status
}

function badgeClass(inv: ProjectInvitation): string {
  if (inv.status === 'accepted') return 'badge badge-success'

  return isPast(inv.expires_at) ? 'badge badge-danger' : 'badge badge-info'
}

function openCreate() {
  clear()
  email.value = ''
  roleID.value = ''
  showCreate.value = true
}

async function create() {
  clear()
  if (!email.value.trim()) return

  creating.value = true
  try {
    const res = await projectApi.createInvitation(props.projectId, {
      email: email.value.trim(),
      role_id: roleID.value,
    })
    notify.success('Invitation created')
    showCreate.value = false
    invited.value = email.value.trim()
    emailed.value = res.data.emailed === true
    link.value = `${window.location.origin}/app/invitations?token=${encodeURIComponent(res.data.token)}`
    emit('changed')
  } catch (err) {
    if (!capture(err)) notify.error(apiErrorMessage(err, 'Failed to create invitation'))
  } finally {
    creating.value = false
  }
}

async function revoke(inv: ProjectInvitation) {
  const ok = await confirm({
    title: 'Delete Invitation',
    message: `Delete the invitation for ${inv.email}?`,
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!ok) return

  try {
    await projectApi.deleteInvitation(props.projectId, inv.id)
    notify.success('Invitation deleted')
    emit('changed')
  } catch (err) {
    notify.error(apiErrorMessage(err, 'Failed to delete invitation'))
  }
}
</script>

<template>
  <div class="card">
    <div class="card-header">
      <h2>Invitations</h2>
      <button class="btn btn-primary btn-sm" @click="openCreate">Create Invitation</button>
    </div>

    <EmptyState
      v-if="invitations.length === 0"
      text="No invitations. Create one to invite someone without an account here yet."
    />

    <div v-else class="table-wrapper">
      <table>
        <thead>
          <tr>
            <th>Email</th>
            <th>Role</th>
            <th>Status</th>
            <th>Expires</th>
            <th>Created</th>
            <th class="text-right">Actions</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="inv in invitations" :key="inv.id">
            <td>{{ inv.email }}</td>
            <td>
              <span v-if="inv.role_name" class="badge badge-neutral">{{ inv.role_name }}</span>
              <span v-else class="text-sm text-muted">project default</span>
            </td>
            <td>
              <span :class="badgeClass(inv)">{{ label(inv) }}</span>
            </td>
            <td>{{ formatDate(inv.expires_at) }}</td>
            <td>{{ formatDate(inv.created_at) }}</td>
            <td>
              <div class="table-actions justify-end">
                <button v-if="canRemove" class="btn btn-danger btn-sm" @click="revoke(inv)">
                  Delete
                </button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <BaseModal
      v-if="showCreate"
      title="Create Invitation"
      form
      @submit="create"
      @close="showCreate = false"
    >
      <!-- It does NOT say invitations are never emailed, which is what
           it used to: whether one is sent depends on whether platform
           mail is configured, and the link dialog two steps later
           reports what actually happened. A flat "not emailed" was
           contradicted by that dialog on any install with mail set up. -->
      <FormField
        label="Email Address"
        :error="errors.email"
        hint="You get a link to share. On an installation with platform mail configured, it is emailed as well."
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
        hint="Ownership is not offered here - it is granted afterwards, by an owner."
      >
        <select v-model="roleID" class="form-select">
          <option v-for="o in options" :key="o.value" :value="o.value">{{ o.label }}</option>
        </select>
      </FormField>

      <template #footer>
        <button type="button" class="btn btn-secondary" @click="showCreate = false">Cancel</button>
        <button type="submit" class="btn btn-primary" :disabled="creating || !email.trim()">
          {{ creating ? 'Creating...' : 'Create Invitation' }}
        </button>
      </template>
    </BaseModal>

    <!-- PERSISTENT, like every other dialog that shows a secret once.
         The token is returned on create and by no route afterwards, so
         a stray Escape here costs the only copy - and on an install
         without platform mail it is the only way to hand it over at
         all. -->
    <BaseModal v-if="link" title="Invitation Link" persistent>
      <p v-if="emailed" class="form-hint mb-2">
        The link was emailed to {{ invited }}. Here it is as well, in case the message does not
        arrive - it is shown only once. The invitee must sign in with the invited email address to
        accept.
      </p>
      <p v-else class="form-hint mb-2">
        Share this link with the invitee. It is shown only once - copy it now. The invitee must sign
        in with the invited email address to accept.
      </p>

      <div class="invite-link-row">
        <input
          :value="link"
          class="form-input"
          readonly
          @focus="($event.target as HTMLInputElement).select()"
        />
        <CopyButton :value="link" variant="btn btn-primary" announce="Invite link copied" />
      </div>

      <template #footer>
        <button class="btn btn-secondary" @click="link = ''">Close</button>
      </template>
    </BaseModal>
  </div>
</template>

<style scoped>
.invite-link-row {
  display: flex;
  gap: 8px;
  align-items: center;
}

.invite-link-row .form-input {
  flex: 1;
  font-size: 12px;
}
</style>
