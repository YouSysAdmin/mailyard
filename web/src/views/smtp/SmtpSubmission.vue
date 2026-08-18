<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { smtpCredentialsApi, type CreateCredentialPayload } from '../../api/smtpCredentials'
import { apiErrorMessage } from '../../api/client'
import type { SubmissionInfo, SMTPCredential } from '../../api/types'
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
import CopyButton from '../../components/CopyButton.vue'
import { useFieldErrors } from '../../composables/fieldErrors'
import Notice from '../../components/Notice.vue'

const notify = useNotificationStore()
const projStore = useProjectStore()
const { confirm } = useConfirm()

const creds = ref<SMTPCredential[]>([])
const submission = ref<SubmissionInfo | null>(null)
const loading = ref(true)
const { pageable, pageItems, goToPage } = useClientPager(creds)

const showCreateModal = ref(false)
const creating = ref(false)
const newName = ref('')
const newIPs = ref('')
const newSandbox = ref(false)

// The one-time plaintext password shown after create.
const createdCred = ref<SMTPCredential | null>(null)
const createdPassword = ref('')
const showPasswordModal = ref(false)

const { errors: fieldErrors, capture, clear } = useFieldErrors()

async function load() {
  loading.value = true
  try {
    const res = await smtpCredentialsApi.list()
    creds.value = res.data.smtp_credentials ?? []
    submission.value = res.data.submission ?? null
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load SMTP credentials'))
  } finally {
    loading.value = false
  }
}

function openCreate() {
  newName.value = ''
  newIPs.value = ''
  newSandbox.value = false
  showCreateModal.value = true
}

async function createCredential() {
  clear()
  if (!newName.value.trim()) return
  creating.value = true
  const payload: CreateCredentialPayload = { name: newName.value.trim() }
  const ips = newIPs.value
    .split(/[\n,]/)
    .map((ip) => ip.trim())
    .filter((ip) => ip.length > 0)
  if (ips.length > 0) payload.allowed_ips = ips
  if (newSandbox.value) payload.sandbox = true
  try {
    const res = await smtpCredentialsApi.create(payload)
    createdCred.value = res.data.smtp_credential
    createdPassword.value = res.data.password
    submission.value = res.data.submission ?? submission.value
    showCreateModal.value = false
    showPasswordModal.value = true
    notify.success('SMTP credential created')
    await load()
  } catch (e) {
    if (!capture(e)) notify.error(apiErrorMessage(e, 'Failed to create SMTP credential'))
  } finally {
    creating.value = false
  }
}

function closePasswordModal() {
  showPasswordModal.value = false
  createdPassword.value = ''
  createdCred.value = null
}

async function revokeCredential(cred: SMTPCredential) {
  const ok = await confirm({
    title: 'Revoke SMTP Credential',
    message: `Revoke "${cred.name}"? The next connection using it is refused and it cannot be reactivated.`,
    confirmText: 'Revoke',
    variant: 'danger',
  })
  if (!ok) return
  try {
    await smtpCredentialsApi.revoke(cred.id)
    notify.success('SMTP credential revoked')
    await load()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to revoke SMTP credential'))
  }
}

async function deleteCredential(cred: SMTPCredential) {
  const ok = await confirm({
    title: 'Delete SMTP Credential',
    message: `Permanently delete "${cred.name}"? This action cannot be undone.`,
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!ok) return
  try {
    await smtpCredentialsApi.remove(cred.id)
    notify.success('SMTP credential deleted')
    await load()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to delete SMTP credential'))
  }
}

// Ready-to-paste client settings, filled with the created username
// while the one-time modal is open.
const connectionRows = computed(() => {
  const r = submission.value
  return [
    { label: 'Host', value: r?.host ?? 'localhost' },
    { label: 'Port', value: r?.port ?? '587' },
    { label: 'Encryption', value: r?.starttls ? 'STARTTLS' : 'None' },
    { label: 'Auth mechanism', value: 'PLAIN' },
  ]
})

watch(() => projStore.currentProjectId, load)
onMounted(load)
</script>

<template>
  <div>
    <PageHeader title="SMTP Submission">
      <button v-if="projStore.can('apikeys:write')" class="btn btn-primary" @click="openCreate">
        Create Credential
      </button>
    </PageHeader>

    <LoadingBlock v-if="loading" />

    <template v-else>
      <Notice
        v-if="submission && !submission.enabled"
        kind="warning"
        title="The submission listener is switched off"
        class="mb-4"
      >
        <p>
          Credentials can be issued now, but nothing will accept them until an operator sets
          <code>submission.enabled: true</code> and restarts the server.
        </p>
      </Notice>
      <Notice
        v-else-if="submission && !submission.starttls"
        kind="warning"
        title="Submission accepts cleartext AUTH"
        class="mb-4"
      >
        <p>
          No STARTTLS certificate is configured, so credentials and message bodies travel
          unencrypted. Keep the port on a private network, or set <code>submission.tls</code> in the
          server config.
        </p>
      </Notice>

      <div class="card connection-card">
        <div class="card-header">
          <h2>Connection settings</h2>
        </div>
        <div class="card-body">
          <p class="text-sm text-muted mb-3">
            Point an existing SMTP client at these settings and use a credential below as the
            username and password. Messages are parsed and pushed through the same pipeline as the
            HTTP send API.
          </p>
          <dl class="conn-grid">
            <template v-for="row in connectionRows" :key="row.label">
              <dt>{{ row.label }}</dt>
              <dd>
                <code>{{ row.value }}</code>
              </dd>
            </template>
          </dl>
        </div>
      </div>

      <div class="card">
        <EmptyState
          v-if="creds.length === 0"
          title="No SMTP credentials"
          text="Create one to let an application submit mail over SMTP."
        />

        <template v-else>
          <div class="table-wrapper">
            <table>
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Username</th>
                  <th>Allowed IPs</th>
                  <th>Mode</th>
                  <th>Status</th>
                  <th>Last Used</th>
                  <th>Created</th>
                  <th class="text-right">Actions</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="cred in pageItems" :key="cred.id">
                  <td class="cell-title">{{ cred.name }}</td>
                  <td>
                    <code>{{ cred.username }}</code>
                  </td>
                  <td>
                    <div
                      v-if="cred.allowed_ips && cred.allowed_ips.length > 0"
                      class="flex gap-2 flex-wrap"
                    >
                      <span v-for="ip in cred.allowed_ips" :key="ip" class="badge badge-info">{{
                        ip
                      }}</span>
                    </div>
                    <span v-else class="text-muted">Any</span>
                  </td>
                  <td>
                    <span v-if="cred.sandbox" class="badge badge-warning">sandbox</span>
                    <span v-else class="badge badge-neutral">live</span>
                  </td>
                  <td>
                    <span v-if="cred.revoked" class="badge badge-dot badge-danger">Revoked</span>
                    <span v-else class="badge badge-dot badge-success">Active</span>
                  </td>
                  <td>{{ formatDate(cred.last_used_at, 'Never') }}</td>
                  <td>{{ formatDate(cred.created_at) }}</td>
                  <td>
                    <div class="table-actions">
                      <button
                        v-if="projStore.can('apikeys:write') && !cred.revoked"
                        class="btn btn-warning btn-sm"
                        @click="revokeCredential(cred)"
                      >
                        Revoke
                      </button>
                      <button
                        v-if="projStore.can('apikeys:delete')"
                        class="btn btn-danger btn-sm"
                        @click="deleteCredential(cred)"
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
    </template>

    <!-- Create Credential Modal -->
    <BaseModal
      v-if="showCreateModal"
      title="Create SMTP Credential"
      form
      @submit="createCredential"
      @close="showCreateModal = false"
    >
      <FormField label="Name" :error="fieldErrors.name">
        <input
          v-model="newName"
          type="text"
          class="form-input"
          placeholder="e.g. Legacy app"
          required
        />
      </FormField>
      <FormField hint="One IP address or CIDR per line. Leave empty to allow any address.">
        <template #label>Allowed IPs <span class="text-muted">(optional)</span></template>
        <textarea
          v-model="newIPs"
          class="form-textarea"
          rows="3"
          placeholder="192.168.1.1&#10;10.0.0.0/24"
        ></textarea>
      </FormField>
      <FormField
        hint="Mail submitted with this credential is captured in the Inbound Sandbox and never delivered. It cannot be switched off later, and the credential cannot ask to send for real - which is the point of handing one out."
      >
        <label class="checkbox-label">
          <input v-model="newSandbox" type="checkbox" />
          <span>Sandbox credential</span>
        </label>
      </FormField>
      <p class="text-sm text-muted">
        A submission credential has no scopes and no expiry. It grants SMTP submission only and is
        either usable or revoked.
      </p>
      <template #footer>
        <button type="button" class="btn btn-secondary" @click="showCreateModal = false">
          Cancel
        </button>
        <button type="submit" class="btn btn-primary" :disabled="creating || !newName.trim()">
          {{ creating ? 'Creating...' : 'Create' }}
        </button>
      </template>
    </BaseModal>

    <!-- One-time Password Modal -->
    <BaseModal v-if="showPasswordModal" title="SMTP Credential Created" persistent>
      <Notice kind="warning" title="Save this password now" class="mb-4">
        <p>It will not be shown again. Store it in a secret manager.</p>
      </Notice>
      <dl class="conn-grid">
        <dt>Host</dt>
        <dd>
          <code>{{ submission?.host ?? 'localhost' }}</code>
        </dd>
        <dt>Port</dt>
        <dd>
          <code>{{ submission?.port ?? '587' }}</code>
        </dd>
        <dt>Username</dt>
        <dd>
          <code>{{ createdCred?.username }}</code>
        </dd>
      </dl>
      <div class="code-block mt-3">{{ createdPassword }}</div>
      <CopyButton
        :value="createdPassword"
        label="Copy Password"
        copied-label="Copied!"
        variant="btn btn-secondary btn-sm mt-4"
      />
      <template #footer>
        <button class="btn btn-primary" @click="closePasswordModal">Done</button>
      </template>
    </BaseModal>
  </div>
</template>

<style scoped>
.connection-card {
  margin-bottom: 16px;
}
.conn-grid {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: 6px 16px;
  margin: 0;
}
.conn-grid dt {
  font-size: 13px;
  color: var(--text-muted);
}
.conn-grid dd {
  margin: 0;
  font-size: 13px;
}
</style>
