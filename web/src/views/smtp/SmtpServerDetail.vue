<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { smtpApi, type SMTPServerPayload } from '../../api/smtp'
import { apiErrorMessage } from '../../api/client'
import type { SMTPServer } from '../../api/types'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import { useConfirm } from '../../composables/useConfirm'
import { formatDate } from '../../composables/formatDate'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import PageHeader from '../../components/PageHeader.vue'
import FormField from '../../components/FormField.vue'
import { useFieldErrors } from '../../composables/fieldErrors'
import Notice from '../../components/Notice.vue'

const route = useRoute()
const router = useRouter()
const notify = useNotificationStore()
const projStore = useProjectStore()
const { confirm } = useConfirm()

const server = ref<SMTPServer | null>(null)
const loading = ref(true)
const saving = ref(false)
const testing = ref(false)
const toggling = ref(false)

const form = ref({
  name: '',
  host: '',
  port: 587,
  username: '',
  password: '',
  encryption: 'starttls',
  skip_dkim: false,
})
const allowedEmailsText = ref('')
const allowedDomainsText = ref('')

const { errors: fieldErrors, capture, clear } = useFieldErrors()

function fillForm(s: SMTPServer) {
  form.value = {
    name: s.name,
    host: s.host,
    port: s.port,
    username: s.username ?? '',
    password: '',
    encryption: s.encryption,
    skip_dkim: s.skip_dkim ?? false,
  }
  allowedEmailsText.value = (s.allowed_emails ?? []).join('\n')
  allowedDomainsText.value = (s.allowed_domains ?? []).join('\n')
}

async function fetchServer() {
  loading.value = true
  try {
    const res = await smtpApi.get(String(route.params.id))
    server.value = res.data.smtp_server
    if (server.value) fillForm(server.value)
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load SMTP server'))
  } finally {
    loading.value = false
  }
}

function parseLines(text: string): string[] {
  return text
    .split(/\r?\n/)
    .map((e) => e.trim())
    .filter((e) => e.length > 0)
}

async function save() {
  clear()
  if (!server.value) return
  saving.value = true
  const payload: SMTPServerPayload = {
    name: form.value.name.trim(),
    host: form.value.host.trim(),
    port: form.value.port,
    username: form.value.username.trim(),
    encryption: form.value.encryption,
    skip_dkim: form.value.skip_dkim,
    allowed_emails: parseLines(allowedEmailsText.value),
    allowed_domains: parseLines(allowedDomainsText.value),
  }
  // An omitted password keeps the stored one.
  if (form.value.password !== '') {
    payload.password = form.value.password
  }
  try {
    const res = await smtpApi.update(server.value.id, payload)
    server.value = res.data.smtp_server
    if (server.value) fillForm(server.value)
    notify.success('SMTP server updated')
  } catch (e) {
    if (!capture(e)) notify.error(apiErrorMessage(e, 'Failed to update SMTP server'))
  } finally {
    saving.value = false
  }
}

async function testServer() {
  if (!server.value) return
  testing.value = true
  try {
    const res = await smtpApi.test(server.value.id)
    if (res.data.ok) {
      notify.success('Connection test succeeded')
    } else {
      notify.error(`Connection failed: ${res.data.error ?? 'unknown error'}`)
    }
    await fetchServer()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Connection test failed'))
  } finally {
    testing.value = false
  }
}

async function toggleStatus() {
  clear()
  if (!server.value) return
  toggling.value = true
  try {
    if (server.value.status === 'disabled') {
      const res = await smtpApi.enable(server.value.id)
      server.value = res.data.smtp_server
      notify.success('SMTP server enabled')
    } else {
      const res = await smtpApi.disable(server.value.id)
      server.value = res.data.smtp_server
      notify.success('SMTP server disabled')
    }
  } catch (e) {
    if (!capture(e)) notify.error(apiErrorMessage(e, 'Failed to change server status'))
  } finally {
    toggling.value = false
  }
}

async function deleteServer() {
  if (!server.value) return
  const ok = await confirm({
    title: 'Delete SMTP Server',
    message: `Delete "${server.value.name}" (${server.value.host})? Emails using this server will no longer be delivered.`,
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!ok) return
  try {
    await smtpApi.remove(server.value.id)
    notify.success('SMTP server deleted')
    router.push('/smtp-servers')
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to delete SMTP server'))
  }
}

onMounted(fetchServer)
</script>

<template>
  <div>
    <PageHeader :title="server ? server.name : 'SMTP Server'">
      <button class="btn btn-secondary" @click="router.push('/smtp-servers')">Back</button>
    </PageHeader>

    <LoadingBlock v-if="loading" />

    <template v-else-if="server">
      <!-- Status card -->
      <div class="card">
        <div class="card-header">
          <h2>{{ server.host }}:{{ server.port }}</h2>
          <span v-if="server.status === 'enabled'" class="badge badge-success badge-dot"
            >Enabled</span
          >
          <span v-else-if="server.status === 'invalid'" class="badge badge-danger badge-dot"
            >Invalid</span
          >
          <span v-else class="badge badge-neutral badge-dot">Disabled</span>
        </div>
        <div class="card-body">
          <Notice
            v-if="server.status === 'invalid' && server.validation_error"
            kind="danger"
            title="Last connection test failed"
            class="mb-4"
          >
            <p>{{ server.validation_error }}</p>
          </Notice>
          <div class="status-grid">
            <div>
              <div class="status-label">Last Validated</div>
              <div class="status-value">{{ formatDate(server.validated_at, 'Never') }}</div>
            </div>
            <div>
              <div class="status-label">Created</div>
              <div class="status-value">{{ formatDate(server.created_at) }}</div>
            </div>
            <div>
              <div class="status-label">Encryption</div>
              <div class="status-value">
                <span v-if="server.encryption === 'ssl'" class="badge badge-success">SSL</span>
                <span v-else-if="server.encryption === 'starttls'" class="badge badge-info"
                  >STARTTLS</span
                >
                <span v-else class="badge badge-neutral">None</span>
              </div>
            </div>
            <div>
              <div class="status-label">Allowed Senders</div>
              <div class="status-value">
                <template v-if="server.allowed_emails && server.allowed_emails.length > 0">
                  <span
                    v-for="e in server.allowed_emails"
                    :key="e"
                    class="badge badge-neutral mr-2"
                    >{{ e }}</span
                  >
                </template>
                <span v-else class="text-muted">Any sender</span>
              </div>
            </div>
            <div>
              <div class="status-label">Allowed Domains</div>
              <div class="status-value">
                <template v-if="server.allowed_domains && server.allowed_domains.length > 0">
                  <span
                    v-for="d in server.allowed_domains"
                    :key="d"
                    class="badge badge-neutral mr-2"
                    >{{ d }}</span
                  >
                </template>
                <span v-else class="text-muted">Any domain</span>
              </div>
            </div>
          </div>
          <div v-if="projStore.can('smtp:write')" class="flex gap-2 mt-5">
            <button class="btn btn-secondary" :disabled="testing" @click="testServer">
              {{ testing ? 'Testing...' : 'Test Connection' }}
            </button>
            <button class="btn btn-secondary" :disabled="toggling" @click="toggleStatus">
              {{ server.status === 'disabled' ? 'Enable' : 'Disable' }}
            </button>
            <button
              v-if="projStore.can('smtp:delete')"
              class="btn btn-danger"
              @click="deleteServer"
            >
              Delete
            </button>
          </div>
        </div>
      </div>

      <!-- Edit form -->
      <div class="card">
        <div class="card-header">
          <h2>Settings</h2>
        </div>
        <div class="card-body">
          <form @submit.prevent="save">
            <FormField label="Name" :error="fieldErrors.name">
              <input
                v-model="form.name"
                type="text"
                class="form-input"
                :disabled="!projStore.can('smtp:write')"
                required
              />
            </FormField>
            <div class="host-port">
              <FormField class="flex-2" label="Host" :error="fieldErrors.host">
                <input
                  v-model="form.host"
                  type="text"
                  class="form-input"
                  :disabled="!projStore.can('smtp:write')"
                  required
                />
              </FormField>
              <FormField class="flex-1" label="Port" :error="fieldErrors.port">
                <input
                  v-model.number="form.port"
                  type="number"
                  class="form-input"
                  min="1"
                  max="65535"
                  :disabled="!projStore.can('smtp:write')"
                  required
                />
              </FormField>
            </div>
            <FormField :error="fieldErrors.username">
              <template #label>Username <span class="text-muted">(optional)</span></template>
              <input
                v-model="form.username"
                type="text"
                class="form-input"
                autocomplete="off"
                :disabled="!projStore.can('smtp:write')"
              />
            </FormField>
            <FormField label="Password" :error="fieldErrors.password">
              <input
                v-model="form.password"
                type="password"
                class="form-input"
                autocomplete="new-password"
                placeholder="Leave blank to keep current"
                :disabled="!projStore.can('smtp:write')"
              />
            </FormField>
            <FormField label="Encryption" :error="fieldErrors.encryption">
              <select
                v-model="form.encryption"
                class="form-select"
                :disabled="!projStore.can('smtp:write')"
              >
                <option value="none">None</option>
                <option value="starttls">STARTTLS (port 587)</option>
                <option value="ssl">SSL/TLS (port 465)</option>
              </select>
            </FormField>
            <FormField
              hint="For providers that rewrite headers and sign with their own key, like Amazon SES with Easy DKIM. Mailyard's signature would arrive broken through them, so it is omitted instead."
            >
              <label class="checkbox-label">
                <input
                  v-model="form.skip_dkim"
                  type="checkbox"
                  :disabled="!projStore.can('smtp:write')"
                />
                <span>Skip DKIM signing</span>
              </label>
            </FormField>
            <FormField
              hint="One per line. Exact addresses or *@domain wildcards. Leave empty to allow any sender."
            >
              <template #label
                >Allowed Sender Emails <span class="text-muted">(optional)</span></template
              >
              <textarea
                v-model="allowedEmailsText"
                class="form-textarea"
                rows="3"
                placeholder="user@example.com&#10;*@example.com"
                :disabled="!projStore.can('smtp:write')"
              ></textarea>
            </FormField>
            <FormField
              hint="One per line. Matched exactly, so a subdomain needs its own line - SPF is written per name. Leave empty to carry any domain."
            >
              <template #label
                >Allowed Sender Domains <span class="text-muted">(optional)</span></template
              >
              <textarea
                v-model="allowedDomainsText"
                class="form-textarea"
                rows="3"
                placeholder="example.com&#10;mail.example.com"
                :disabled="!projStore.can('smtp:write')"
              ></textarea>
            </FormField>
            <div v-if="projStore.can('smtp:write')" class="text-right">
              <button type="submit" class="btn btn-primary" :disabled="saving">
                {{ saving ? 'Saving...' : 'Save Changes' }}
              </button>
            </div>
          </form>
        </div>
      </div>
    </template>

    <EmptyState v-else title="SMTP server not found" />
  </div>
</template>

<style scoped>
.status-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 16px;
}
.status-label {
  font-size: 12px;
  font-weight: 600;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.05em;
  margin-bottom: 4px;
}
.status-value {
  font-size: 14px;
  color: var(--text-primary);
}
/* Host and port, and NOT the global .form-row - that is two equal halves
   and a port is four characters. The ratio comes from .flex-2 / .flex-1
   on the two fields, which a grid of equal columns cannot express. */
.host-port {
  display: flex;
  gap: 16px;
}
</style>
