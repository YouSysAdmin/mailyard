<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { smtpApi } from '../../api/smtp'
import { smtpGroupApi } from '../../api/smtpGroups'
import { apiErrorMessage } from '../../api/client'
import type { MailProvider, SMTPServer, SMTPServerGroup } from '../../api/types'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import { useConfirm } from '../../composables/useConfirm'
import { useClientPager } from '../../composables/usePagination'
import Pagination from '../../components/Pagination.vue'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import PageHeader from '../../components/PageHeader.vue'
import SmtpServerForm from './SmtpServerForm.vue'

const notify = useNotificationStore()
const projStore = useProjectStore()
const { confirm } = useConfirm()

const servers = ref<SMTPServer[]>([])
const loading = ref(true)
const { pageable, pageItems, goToPage } = useClientPager(servers)

const showModal = ref(false)
const editing = ref<SMTPServer | null>(null)
const testingId = ref<string | null>(null)
const togglingId = ref<string | null>(null)

// Groups are loaded alongside the servers so the form can offer them.
// An empty selection means the project's default group, which is
// where every server lived before groups existed.
const groups = ref<SMTPServerGroup[]>([])
const providers = ref<MailProvider[]>([])

function providerLabel(id?: string): string {
  const p = providers.value.find((x) => x.id === (id || 'smtp'))
  return p?.label ?? id ?? 'SMTP'
}

async function load() {
  loading.value = true
  try {
    const [srvRes, grpRes] = await Promise.all([smtpApi.list(), smtpGroupApi.list()])
    servers.value = srvRes.data.smtp_servers ?? []
    // What this build can send through, and what each one asks for. From
    // the server, so the form cannot offer fields the write side refuses.
    providers.value = srvRes.data.providers ?? []
    groups.value = grpRes.data.smtp_server_groups ?? []
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load SMTP servers'))
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editing.value = null
  showModal.value = true
}

function openEdit(server: SMTPServer) {
  editing.value = server
  showModal.value = true
}

async function onSaved() {
  showModal.value = false
  await load()
}

// collectOptions keeps only the keys this provider declares, so a field
// left behind by switching providers in the form is not stored.

async function testServer(server: SMTPServer) {
  testingId.value = server.id
  try {
    const res = await smtpApi.test(server.id)
    if (res.data.ok) {
      notify.success(`Connection to ${server.host} succeeded`)
    } else {
      notify.error(`Connection failed: ${res.data.error ?? 'unknown error'}`)
    }
    await load()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Connection test failed'))
  } finally {
    testingId.value = null
  }
}

async function toggleStatus(server: SMTPServer) {
  togglingId.value = server.id
  try {
    if (server.status === 'disabled') {
      await smtpApi.enable(server.id)
      notify.success('SMTP server enabled')
    } else {
      await smtpApi.disable(server.id)
      notify.success('SMTP server disabled')
    }
    await load()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to change server status'))
  } finally {
    togglingId.value = null
  }
}

async function deleteServer(server: SMTPServer) {
  const ok = await confirm({
    title: 'Delete SMTP Server',
    message: `Delete "${server.name}" (${server.host})? Emails using this server will no longer be delivered.`,
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!ok) return
  try {
    await smtpApi.remove(server.id)
    notify.success('SMTP server deleted')
    await load()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to delete SMTP server'))
  }
}

watch(() => projStore.currentProjectId, load)
onMounted(load)
</script>

<template>
  <div>
    <PageHeader title="SMTP Servers">
      <button v-if="projStore.can('smtp:write')" class="btn btn-primary" @click="openCreate">
        Add Server
      </button>
    </PageHeader>

    <LoadingBlock v-if="loading" />

    <div v-else class="card">
      <EmptyState
        v-if="servers.length === 0"
        title="No SMTP servers"
        text="Add an SMTP server to start sending emails."
      />

      <template v-else>
        <div class="table-wrapper">
          <table>
            <thead>
              <tr>
                <th>Name</th>
                <!-- Without this an API row is indistinguishable from an
                     SMTP one with a blank host. -->
                <th>Provider</th>
                <th>Host</th>
                <th>Port</th>
                <th>Encryption</th>
                <th>Status</th>
                <th>Allowed Senders</th>
                <th class="text-right">Actions</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="server in pageItems" :key="server.id">
                <td>
                  <router-link :to="`/smtp-servers/${server.id}`" class="cell-link">{{
                    server.name
                  }}</router-link>
                </td>
                <td>{{ providerLabel(server.provider) }}</td>
                <td>
                  <span v-if="server.host">{{ server.host }}</span>
                  <span
                    v-else
                    class="text-muted"
                    :title="providerLabel(server.provider) + ' is reached over its own API'"
                  >
                    -
                  </span>
                </td>
                <td>
                  <span v-if="server.port">{{ server.port }}</span>
                  <span v-else class="text-muted">-</span>
                </td>
                <td>
                  <span v-if="server.encryption === 'ssl'" class="badge badge-success">SSL</span>
                  <span v-else-if="server.encryption === 'starttls'" class="badge badge-info"
                    >STARTTLS</span
                  >
                  <span v-else-if="server.host" class="badge badge-neutral">None</span>
                  <!-- Not "None", which would read as cleartext. An API
                       call is HTTPS and the field simply does not apply. -->
                  <span v-else class="text-muted">-</span>
                </td>
                <td>
                  <span v-if="server.status === 'enabled'" class="badge badge-success badge-dot"
                    >Enabled</span
                  >
                  <span
                    v-else-if="server.status === 'invalid'"
                    class="badge badge-danger badge-dot"
                    :title="server.validation_error || 'Connection test failed'"
                    >Invalid</span
                  >
                  <span v-else class="badge badge-neutral badge-dot">Disabled</span>
                </td>
                <td>
                  <span
                    v-if="server.allowed_emails && server.allowed_emails.length > 0"
                    class="text-sm"
                  >
                    {{ server.allowed_emails.join(', ') }}
                  </span>
                  <span v-else class="text-muted">Any</span>
                </td>
                <td>
                  <div class="table-actions">
                    <button
                      v-if="projStore.can('smtp:write')"
                      class="btn btn-secondary btn-sm"
                      :disabled="testingId === server.id"
                      @click="testServer(server)"
                    >
                      {{ testingId === server.id ? 'Testing...' : 'Test' }}
                    </button>
                    <button
                      v-if="projStore.can('smtp:write')"
                      class="btn btn-secondary btn-sm"
                      :disabled="togglingId === server.id"
                      @click="toggleStatus(server)"
                    >
                      {{ server.status === 'disabled' ? 'Enable' : 'Disable' }}
                    </button>
                    <button
                      v-if="projStore.can('smtp:write')"
                      class="btn btn-secondary btn-sm"
                      @click="openEdit(server)"
                    >
                      Edit
                    </button>
                    <button
                      v-if="projStore.can('smtp:delete')"
                      class="btn btn-danger btn-sm"
                      @click="deleteServer(server)"
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

    <!-- Create/Edit Modal -->
    <SmtpServerForm
      v-if="showModal"
      :server="editing"
      :providers="providers"
      :groups="groups"
      @saved="onSaved"
      @close="showModal = false"
    />
  </div>
</template>
