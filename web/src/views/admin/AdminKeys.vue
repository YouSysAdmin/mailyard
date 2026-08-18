<script setup lang="ts">
// Platform credentials: the key type that can stand an installation
// up from a script.
//
// Deliberately plainer than the project API keys page - there is no
// permission grid, because there is nothing to narrow. A key here is
// admin. What CAN be narrowed is where it may be used from and how
// long it lives, so those two are given prominence instead.
import { onMounted, ref } from 'vue'
import { adminKeysApi, type AdminAPIKey, type CreateAdminKeyPayload } from '../../api/adminKeys'
import { apiErrorMessage } from '../../api/client'
import { useNotificationStore } from '../../stores/notification'
import { useConfirm } from '../../composables/useConfirm'
import { useExpiry } from '../../composables/useExpiry'
import { formatDate } from '../../composables/formatDate'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import PageHeader from '../../components/PageHeader.vue'
import BaseModal from '../../components/BaseModal.vue'
import CopyButton from '../../components/CopyButton.vue'
import FormField from '../../components/FormField.vue'
import { useFieldErrors } from '../../composables/fieldErrors'

const notify = useNotificationStore()
const { confirm } = useConfirm()

const keys = ref<AdminAPIKey[]>([])
const loading = ref(true)

const showCreateModal = ref(false)
const creating = ref(false)
const newName = ref('')
const newIPs = ref('')
const {
  never: expiresNever,
  at: expiresAt,
  reset: resetExpiry,
  invalid: expiryInvalid,
  payload: expiryPayload,
} = useExpiry()

// The one-time plaintext token shown after create.
const createdToken = ref('')
const showTokenModal = ref(false)

// The token is dropped with the dialog: it is shown exactly once and
// keeping it in memory afterwards serves nobody.
const { errors: fieldErrors, capture, clear } = useFieldErrors()

function closeTokenModal() {
  showTokenModal.value = false
  createdToken.value = ''
}

// Not @click.self: that fires when a drag STARTED inside the modal
// ends on the overlay, closing a form somebody was selecting text in.

async function load() {
  loading.value = true
  try {
    const res = await adminKeysApi.list()
    keys.value = res.data.admin_api_keys ?? []
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load platform credentials'))
  } finally {
    loading.value = false
  }
}

function openCreate() {
  newName.value = ''
  newIPs.value = ''
  resetExpiry()
  showCreateModal.value = true
}

async function createKey() {
  clear()
  if (!newName.value.trim()) return
  creating.value = true
  const payload: CreateAdminKeyPayload = { name: newName.value.trim() }
  const ips = newIPs.value
    .split(/[\n,]/)
    .map((ip) => ip.trim())
    .filter((ip) => ip.length > 0)
  if (ips.length > 0) payload.allowed_ips = ips
  const expires = expiryPayload()
  if (expires) payload.expires_at = expires
  try {
    const res = await adminKeysApi.create(payload)
    createdToken.value = res.data.token
    showCreateModal.value = false
    showTokenModal.value = true
    notify.success('Platform credential created')
    await load()
  } catch (e) {
    if (!capture(e)) notify.error(apiErrorMessage(e, 'Failed to create the credential'))
  } finally {
    creating.value = false
  }
}

async function revokeKey(key: AdminAPIKey) {
  const ok = await confirm({
    title: 'Revoke platform credential',
    message: `Revoke "${key.name}"? Anything authenticating with it stops working immediately.`,
    confirmText: 'Revoke',
    variant: 'danger',
  })
  if (!ok) return
  try {
    await adminKeysApi.revoke(key.id)
    notify.success('Credential revoked')
    await load()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to revoke'))
  }
}

async function removeKey(key: AdminAPIKey) {
  const ok = await confirm({
    title: 'Delete platform credential',
    message: `Delete "${key.name}"? The record goes with it - revoke instead if you may need to explain later what this credential was.`,
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!ok) return
  try {
    await adminKeysApi.remove(key.id)
    notify.success('Credential deleted')
    await load()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to delete'))
  }
}

function status(key: AdminAPIKey): { label: string; cls: string } {
  if (key.revoked) return { label: 'revoked', cls: 'badge-danger' }
  if (key.expires_at && new Date(key.expires_at) < new Date()) {
    return { label: 'expired', cls: 'badge-warning' }
  }
  return { label: 'active', cls: 'badge-success' }
}

onMounted(load)
</script>

<template>
  <div>
    <PageHeader>
      <template #title>
        <h1>Platform Credentials</h1>
        <p class="text-muted">
          API keys for <code>/api/v1/admin</code> - users, plans, identity providers, the shared
          SMTP pool and installation settings. A project API key cannot reach that surface however
          wide its permissions are.
        </p>
      </template>
      <button class="btn btn-primary" @click="openCreate">New credential</button>
    </PageHeader>

    <div class="card callout">
      <strong>These have no permission list.</strong>
      A credential here can create users and rewrite this installation's settings. Narrow it with an
      IP allowlist and an expiry, and prefer a project API key whenever the job is actually about
      one project.
    </div>

    <LoadingBlock v-if="loading" />
    <div v-else class="card">
      <EmptyState
        v-if="keys.length === 0"
        title="No platform credentials"
        text="Everything platform-level is being done through the console, by a person."
      />
      <div v-else class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th>Name</th>
              <th>Prefix</th>
              <th>Allowed IPs</th>
              <th>Status</th>
              <th>Last Used</th>
              <th>Expires</th>
              <th class="text-right">Actions</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="key in keys" :key="key.id">
              <td class="cell-title">{{ key.name }}</td>
              <td>
                <code>{{ key.key_prefix }}...</code>
              </td>
              <td>
                <span v-if="key.allowed_ips.length === 0" class="text-muted">any</span>
                <span v-else class="text-sm">{{ key.allowed_ips.join(', ') }}</span>
              </td>
              <td>
                <span class="badge badge-dot" :class="status(key).cls">{{
                  status(key).label
                }}</span>
              </td>
              <td class="text-sm">{{ formatDate(key.last_used_at, 'Never') }}</td>
              <td class="text-sm">{{ formatDate(key.expires_at, 'Never') }}</td>
              <td class="table-actions">
                <button
                  v-if="!key.revoked"
                  class="btn btn-secondary btn-sm"
                  @click="revokeKey(key)"
                >
                  Revoke
                </button>
                <button class="btn btn-danger btn-sm" @click="removeKey(key)">Delete</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <BaseModal
      v-if="showCreateModal"
      title="New Platform Credential"
      form
      @submit="createKey"
      @close="showCreateModal = false"
    >
      <FormField label="Name" :error="fieldErrors.name">
        <input
          v-model="newName"
          type="text"
          class="form-input"
          placeholder="e.g. terraform, bootstrap"
          required
        />
      </FormField>
      <FormField
        hint="One per line or comma separated. Empty means any address, which for this credential is worth thinking about."
      >
        <template #label>Allowed IPs <span class="text-muted">(optional)</span></template>
        <textarea
          v-model="newIPs"
          class="form-textarea"
          rows="3"
          placeholder="203.0.113.5&#10;198.51.100.0/24"
        ></textarea>
      </FormField>
      <FormField label="Expires">
        <label class="checkbox-label">
          <input v-model="expiresNever" type="checkbox" />
          <span>Never expires</span>
        </label>
        <input
          v-if="!expiresNever"
          v-model="expiresAt"
          type="datetime-local"
          class="form-input"
          required
        />
      </FormField>
      <template #footer>
        <button type="button" class="btn btn-secondary" @click="showCreateModal = false">
          Cancel
        </button>
        <button
          type="submit"
          class="btn btn-primary"
          :disabled="creating || !newName.trim() || expiryInvalid"
        >
          {{ creating ? 'Creating...' : 'Create' }}
        </button>
      </template>
    </BaseModal>

    <BaseModal v-if="showTokenModal" title="Save this token now" persistent>
      <div class="code-block">
        <code>{{ createdToken }}</code>
      </div>
      <p class="text-sm text-muted mt-2">
        It appears here and never again - only its hash is stored, so nobody, including an operator
        with database access, can recover it.
      </p>
      <template #footer>
        <CopyButton :value="createdToken" variant="btn btn-secondary" />
        <button class="btn btn-primary" @click="closeTokenModal">Done</button>
      </template>
    </BaseModal>
  </div>
</template>

<style scoped>
.callout {
  padding: 12px 16px;
  margin-bottom: 16px;
  font-size: 14px;
  border-left: 3px solid var(--warning-fg, #d19a00);
}
</style>
