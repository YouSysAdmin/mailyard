<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { usersApi } from '../../api/users'
import { apiErrorMessage } from '../../api/client'
import type { User } from '../../api/types'
import { useAuthStore } from '../../stores/auth'
import { useNotificationStore } from '../../stores/notification'
import { useConfirm } from '../../composables/useConfirm'
import { useClientPager } from '../../composables/usePagination'
import Pagination from '../../components/Pagination.vue'
import { formatDate } from '../../composables/formatDate'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import PageHeader from '../../components/PageHeader.vue'
import BaseModal from '../../components/BaseModal.vue'
import FormField from '../../components/FormField.vue'
import UserForm from './UserForm.vue'
import { useFieldErrors } from '../../composables/fieldErrors'

const auth = useAuthStore()
const notify = useNotificationStore()
const { confirm } = useConfirm()

const loading = ref(true)
const users = ref<User[]>([])
const search = ref('')

const filtered = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return users.value
  return users.value.filter((u) => u.email.toLowerCase().includes(q))
})
const { pageable, pageItems, goToPage } = useClientPager(filtered, 20)

// Create modal
const showCreateModal = ref(false)
const creating = ref(false)
const newUser = ref({ email: '', password: '', admin: false })

// Edit modal
const editingUser = ref<User | null>(null)

function startEdit(u: User) {
  editingUser.value = u
}

// Anything the dialog changed - a save, a reset - lands in the list, and
// the dialog is re-pointed at the fresh row so its own controls follow.
async function onUserChanged() {
  const id = editingUser.value?.id
  await fetchUsers()
  if (id) editingUser.value = users.value.find((u) => u.id === id) ?? null
}

// Memberships shown in the edit modal, loaded when it opens.

const { errors: fieldErrors, capture, clear } = useFieldErrors()

function isSelf(u: User): boolean {
  return auth.user?.id === u.id
}

async function fetchUsers() {
  loading.value = true
  try {
    const res = await usersApi.list()
    users.value = res.data.users ?? []
  } catch (err) {
    notify.error(apiErrorMessage(err, 'Failed to load users'))
  } finally {
    loading.value = false
  }
}

function openCreateModal() {
  newUser.value = { email: '', password: '', admin: false }
  showCreateModal.value = true
}

async function createUser() {
  clear()
  if (!newUser.value.email.trim()) return
  if (newUser.value.password && newUser.value.password.length < 8) {
    notify.error('Password must be at least 8 characters')
    return
  }
  creating.value = true
  try {
    await usersApi.create({
      email: newUser.value.email.trim(),
      password: newUser.value.password,
      admin: newUser.value.admin,
    })
    notify.success('User created')
    showCreateModal.value = false
    await fetchUsers()
  } catch (err) {
    if (!capture(err)) notify.error(apiErrorMessage(err, 'Failed to create user'))
  } finally {
    creating.value = false
  }
}

async function deleteUser(u: User) {
  const ok = await confirm({
    title: 'Delete User',
    message: `Permanently delete "${u.email}"? This cannot be undone.`,
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!ok) return
  try {
    await usersApi.remove(u.id)
    notify.success('User deleted')
    await fetchUsers()
  } catch (err) {
    notify.error(apiErrorMessage(err, 'Failed to delete user'))
  }
}

function accountLabel(t: number): string {
  return t === 2 ? 'OIDC' : 'local'
}

onMounted(fetchUsers)
</script>

<template>
  <div>
    <PageHeader title="Users">
      <button class="btn btn-primary" @click="openCreateModal">Create User</button>
    </PageHeader>

    <div class="card">
      <div class="card-header">
        <h2>Platform Users</h2>
        <input
          v-model="search"
          type="text"
          class="form-input w-search"
          placeholder="Search by email..."
        />
      </div>

      <LoadingBlock v-if="loading" />

      <EmptyState v-else-if="filtered.length === 0" title="No users found">
        <p v-if="search">No users match "{{ search }}".</p>
        <p v-else>There are no users yet.</p>
      </EmptyState>

      <template v-else>
        <div class="table-wrapper">
          <table>
            <thead>
              <tr>
                <th>Email</th>
                <th>Account</th>
                <th>Admin</th>
                <th>Status</th>
                <th>Last Login</th>
                <th>Created</th>
                <th class="text-right">Actions</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="u in pageItems" :key="u.id">
                <td class="fw-medium">
                  {{ u.email }}
                  <span v-if="isSelf(u)" class="badge badge-secondary">you</span>
                </td>
                <td>
                  <span class="badge badge-neutral">{{ accountLabel(u.account_type) }}</span>
                </td>
                <td>
                  <span v-if="u.admin" class="badge badge-info">admin</span>
                  <span v-else>-</span>
                </td>
                <td>
                  <span
                    class="badge badge-dot"
                    :class="u.disabled ? 'badge-danger' : 'badge-success'"
                  >
                    {{ u.disabled ? 'Disabled' : 'Active' }}
                  </span>
                  <span
                    v-if="u.email_verified === false"
                    class="badge badge-warning"
                    title="Self-registered and has not confirmed the emailed link yet"
                  >
                    unverified
                  </span>
                </td>
                <td>{{ formatDate(u.last_login_at) }}</td>
                <td>{{ formatDate(u.created_at) }}</td>
                <td>
                  <div class="table-actions justify-end">
                    <button class="btn btn-secondary btn-sm" @click="startEdit(u)">Edit</button>
                    <button
                      class="btn btn-danger btn-sm"
                      :disabled="isSelf(u)"
                      :title="isSelf(u) ? 'You cannot delete your own account' : 'Delete user'"
                      @click="deleteUser(u)"
                    >
                      Delete
                    </button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <div class="card-body pt-0">
          <Pagination :pageable="pageable" @page="goToPage" />
        </div>
      </template>
    </div>

    <!-- Create User Modal -->
    <BaseModal
      v-if="showCreateModal"
      title="Create User"
      form
      @submit="createUser"
      @close="showCreateModal = false"
    >
      <FormField label="Email" :error="fieldErrors.email">
        <input
          v-model="newUser.email"
          type="email"
          class="form-input"
          placeholder="user@example.com"
          required
        />
      </FormField>
      <FormField
        label="Password"
        :error="fieldErrors.password"
        hint="Leave empty for an OIDC-only account that signs in through the identity provider."
      >
        <input
          v-model="newUser.password"
          type="password"
          class="form-input"
          placeholder="Minimum 8 characters"
          minlength="8"
        />
      </FormField>
      <FormField hint="Manages users, plans, identity providers and installation settings.">
        <label class="checkbox-label">
          <input v-model="newUser.admin" type="checkbox" />
          <span>Platform administrator</span>
        </label>
      </FormField>
      <template #footer>
        <button type="button" class="btn btn-secondary" @click="showCreateModal = false">
          Cancel
        </button>
        <button type="submit" class="btn btn-primary" :disabled="creating || !newUser.email.trim()">
          {{ creating ? 'Creating...' : 'Create User' }}
        </button>
      </template>
    </BaseModal>

    <!-- Edit User Modal -->
    <UserForm
      v-if="editingUser"
      :user="editingUser"
      @changed="onUserChanged"
      @close="editingUser = null"
    />
  </div>
</template>

<style scoped></style>
