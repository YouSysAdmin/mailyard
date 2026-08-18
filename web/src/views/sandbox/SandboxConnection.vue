<script setup lang="ts">
// Where to point a client, and what to authenticate with.
//
// A DIALOG, not part of the page. Everything in it is read once, when
// somebody wires a staging application up - and on a page people leave
// open while a suite runs it was three hundred pixels of permanent
// furniture above the thing they came to read.
//
// It owns the credentials because they are the only writes here, and
// because the page behind it has no use for them: the message list does
// not care what submitted a capture.
import { ref } from 'vue'
import { sandboxApi, type SandboxInfo } from '../../api/sandbox'
import type { SMTPCredential } from '../../api/types'
import { apiErrorMessage } from '../../api/client'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import { useConfirm } from '../../composables/useConfirm'
import { formatDate } from '../../composables/formatDate'
import BaseModal from '../../components/BaseModal.vue'
import FormField from '../../components/FormField.vue'
import CopyButton from '../../components/CopyButton.vue'
import Notice from '../../components/Notice.vue'

const props = defineProps<{
  /** Null while it is still being read, or when the read failed. */
  info: SandboxInfo | null
  credentials: SMTPCredential[]
  /** How long a capture is kept, already worded by the page. */
  keptFor: string
}>()

const emit = defineEmits<{
  /** A credential was created or revoked - the list needs rereading. */
  (e: 'changed'): void
  (e: 'close'): void
}>()

const notify = useNotificationStore()
const projStore = useProjectStore()
const { confirm } = useConfirm()

const showCreate = ref(false)
const newName = ref('')
const creating = ref(false)

// Shown exactly once, like every other secret in the product. There is
// no route that can read it back, which is why the dialog holding it is
// persistent - a stray Escape here loses the password for good.
const mintedUsername = ref('')
const mintedPassword = ref('')

/** The port alone. `addr` is a bind address, which may carry a host. */
function port(): string {
  return (props.info?.submission.addr ?? ':587').replace(/^.*:/, '')
}

async function create() {
  if (!newName.value.trim()) return

  creating.value = true
  try {
    const res = await sandboxApi.createCredential(newName.value.trim())
    mintedUsername.value = res.data.smtp_credential.username
    mintedPassword.value = res.data.password
    showCreate.value = false
    newName.value = ''
    emit('changed')
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to create the credential'))
  } finally {
    creating.value = false
  }
}

async function revoke(cred: SMTPCredential) {
  const ok = await confirm({
    title: 'Revoke credential',
    message: `Anything still authenticating as ${cred.username} stops being accepted.`,
    confirmText: 'Revoke',
    variant: 'danger',
  })
  if (!ok) return

  try {
    await sandboxApi.revokeCredential(cred.id)
    emit('changed')
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to revoke the credential'))
  }
}
</script>

<template>
  <div>
    <BaseModal title="Connection" size="modal-w860" @close="emit('close')">
      <!-- The listener warning belongs here rather than on the page: it
           is about the same thing, and as a banner it was permanent
           furniture for a condition an operator either already knows or
           is about to read below. -->
      <Notice
        v-if="info && !info.submission.enabled"
        kind="warning"
        title="The SMTP submission listener is switched off"
        class="mb-4"
      >
        <p>
          Nothing can be submitted over SMTP until an operator sets
          <code>submission.enabled: true</code> and restarts the server. The HTTP API still
          captures.
        </p>
      </Notice>

      <p class="lead">
        Mail sent with a <strong>sandbox credential</strong> is captured here and never delivered.
        Point a staging application at Mailyard with one and read exactly what it sent, headers and
        all.
      </p>

      <dl class="facts">
        <div>
          <dt>SMTP host</dt>
          <dd>
            <code>{{ info?.submission.host ?? 'localhost' }}</code>
          </dd>
        </div>
        <div>
          <dt>Port</dt>
          <dd>
            <code>{{ port() }}</code>
          </dd>
        </div>
        <div>
          <dt>STARTTLS</dt>
          <dd>{{ info?.submission.starttls ? 'Available' : 'Not configured' }}</dd>
        </div>
        <div>
          <dt>Kept for</dt>
          <dd>{{ keptFor }}</dd>
        </div>
      </dl>

      <div class="creds-head">
        <h2>Credentials</h2>
        <button
          v-if="projStore.can('sandbox:write')"
          class="btn btn-primary btn-sm"
          @click="showCreate = true"
        >
          New credential
        </button>
      </div>

      <p v-if="credentials.length === 0" class="muted">
        None yet. Create one and point a staging application at the host and port above - nothing it
        sends leaves this page.
      </p>
      <div v-else class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th>Name</th>
              <th>Username</th>
              <th>Status</th>
              <th>Last used</th>
              <th class="text-right"></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="cred in credentials" :key="cred.id">
              <td>{{ cred.name }}</td>
              <td>
                <code>{{ cred.username }}</code>
              </td>
              <td>
                <span v-if="cred.revoked" class="badge badge-danger">revoked</span>
                <span v-else class="badge badge-success">active</span>
              </td>
              <td>{{ formatDate(cred.last_used_at) }}</td>
              <td class="text-right">
                <button
                  v-if="!cred.revoked && projStore.can('sandbox:write')"
                  class="btn btn-warning btn-sm"
                  @click="revoke(cred)"
                >
                  Revoke
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <p class="creds-note">
        These grant sandbox submission and nothing else. A credential from here can never send real
        mail, which is why anyone in the project may create one.
      </p>

      <template #footer>
        <button class="btn btn-primary" @click="emit('close')">Done</button>
      </template>
    </BaseModal>

    <BaseModal
      v-if="showCreate"
      title="New sandbox credential"
      @close="showCreate = false"
      @submit="create"
    >
      <FormField label="Name">
        <input v-model="newName" class="form-input" placeholder="staging app" required />
      </FormField>
      <template #footer>
        <button class="btn btn-secondary" @click="showCreate = false">Cancel</button>
        <button class="btn btn-primary" :disabled="creating" @click="create">
          {{ creating ? 'Creating...' : 'Create' }}
        </button>
      </template>
    </BaseModal>

    <!-- The password gets a row of its own, in the same .code-block the
         other four show-once dialogs use.
         It was a cell in the two-column facts grid beside the username,
         and a generated password is 64 unbreakable characters: measured
         at 539px of text in a 235px cell, spilling 287px past the right
         edge of a 520px dialog. .code-block scrolls INSIDE itself, so a
         secret too long for the box never pushes anything sideways. -->
    <BaseModal v-if="mintedPassword" title="Credential created" persistent>
      <Notice kind="warning" title="Save this password now" class="mb-4">
        <p>It is shown once and stored hashed, so nothing can read it back.</p>
      </Notice>

      <dl class="facts">
        <div>
          <dt>Username</dt>
          <dd>
            <code>{{ mintedUsername }}</code>
          </dd>
        </div>
      </dl>

      <p class="secret-label">Password</p>
      <div class="code-block">{{ mintedPassword }}</div>

      <template #footer>
        <CopyButton
          :value="`${mintedUsername} / ${mintedPassword}`"
          label="Copy"
          variant="btn btn-secondary"
        />
        <button class="btn btn-primary" @click="mintedPassword = ''">Done</button>
      </template>
    </BaseModal>
  </div>
</template>

<style scoped>
.lead {
  margin: 0 0 16px;
  color: var(--text-secondary);
}

/* As many columns as fit rather than a fixed count: four facts on a
   860px dialog, two on a phone, and nothing measured in the markup. */
.facts {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 16px;
  margin: 0;
}

.facts dt {
  font-size: 12px;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--text-tertiary);
  margin-bottom: 4px;
}

.facts dd {
  margin: 0;
  font-size: 14px;
}

/* Matches the facts' dt above it, so the password reads as one more
   labelled fact rather than as a heading over an unrelated block. */
.secret-label {
  margin: 16px 0 4px;
  font-size: 12px;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--text-tertiary);
}

.creds-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin: 24px 0 12px;
}

.creds-head h2 {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
}

.creds-note {
  margin: 16px 0 0;
  font-size: 13px;
  color: var(--text-tertiary);
}

.muted {
  margin: 0;
  color: var(--text-tertiary);
}
</style>
