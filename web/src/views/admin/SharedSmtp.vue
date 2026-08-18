<script setup lang="ts">
// The platform SMTP pool.
//
// These servers belong to the platform, not to any project. A project
// that has configured no server of its own delivers through them
// without ever seeing them - no project-scoped endpoint returns one,
// which is why this page exists only under Admin.
import { onMounted, ref } from 'vue'
import { sharedSmtpApi } from '../../api/sharedSmtp'
import { apiErrorMessage } from '../../api/client'
import type { MailProvider, SharedSMTPServer } from '../../api/types'
import { useNotificationStore } from '../../stores/notification'
import { useConfirm } from '../../composables/useConfirm'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import PageHeader from '../../components/PageHeader.vue'
import SharedServerForm from './SharedServerForm.vue'

const notify = useNotificationStore()
const { confirm } = useConfirm()

const servers = ref<SharedSMTPServer[]>([])
const providers = ref<MailProvider[]>([])
const loading = ref(true)
const testing = ref('')

const showForm = ref(false)
const editing = ref<SharedSMTPServer | null>(null)

function providerLabel(id?: string): string {
  return providers.value.find((x) => x.id === (id || 'smtp'))?.label ?? id ?? 'SMTP'
}

async function load() {
  loading.value = true
  try {
    const res = await sharedSmtpApi.list()
    servers.value = res.data.shared_smtp_servers ?? []
    providers.value = res.data.providers ?? []
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load the shared pool'))
  } finally {
    loading.value = false
  }
}

function open(srv: SharedSMTPServer | null) {
  editing.value = srv
  showForm.value = true
}

async function saved() {
  showForm.value = false
  await load()
}

async function test(srv: SharedSMTPServer) {
  testing.value = srv.id
  try {
    const res = await sharedSmtpApi.test(srv.id)
    if (res.data.ok) notify.success(`${srv.name}: connection successful`)
    else notify.error(`${srv.name}: ${res.data.error ?? 'connection failed'}`)
    await load()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Test failed'))
  } finally {
    testing.value = ''
  }
}

async function toggle(srv: SharedSMTPServer) {
  const next = srv.status === 'enabled' ? 'disabled' : 'enabled'
  try {
    await sharedSmtpApi.update(srv.id, { status: next })
    await load()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to change status'))
  }
}

async function remove(srv: SharedSMTPServer) {
  const ok = await confirm({
    title: 'Remove shared server',
    message: `Remove ${srv.name} from the shared pool?`,
    confirmText: 'Remove',
    variant: 'danger',
  })
  if (!ok) return

  try {
    await sharedSmtpApi.remove(srv.id)
    notify.success('Server removed')
    await load()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to remove the server'))
  }
}

onMounted(load)
</script>

<template>
  <div>
    <PageHeader title="Shared SMTP Pool">
      <button class="btn btn-primary" @click="open(null)">Add Server</button>
    </PageHeader>

    <div class="card">
      <div class="card-header">
        <div>
          <h2>Platform servers</h2>
          <p class="text-sm text-muted">
            Used by any project that has configured none of its own. The moment a project adds a
            server it stops using this pool, and projects never see these entries.
          </p>
        </div>
      </div>

      <LoadingBlock v-if="loading" />

      <EmptyState
        v-else-if="servers.length === 0"
        title="The pool is empty"
        text="Projects with no SMTP server of their own cannot send."
      />

      <div v-else class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th>Name</th>
              <th>Provider</th>
              <th>Host</th>
              <th>Domains</th>
              <th title="Strict requires the sending project to have verified the sender domain">
                Strict
              </th>
              <th>Status</th>
              <th class="text-right">Actions</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="srv in servers" :key="srv.id">
              <td>
                {{ srv.name }}
                <span v-if="srv.platform_only" class="badge badge-info">platform</span>
                <div class="text-sm text-muted">priority {{ srv.priority }}</div>
              </td>
              <td>{{ providerLabel(srv.provider) }}</td>
              <td>
                <template v-if="srv.host">
                  {{ srv.host }}:{{ srv.port }}
                  <div class="text-sm text-muted">{{ srv.encryption }}</div>
                </template>
                <!-- Not blank, and not "none" for encryption either: an
                     API row has no host, and "none" there would read as
                     cleartext when the call is HTTPS. -->
                <span v-else class="text-muted">over the provider API</span>
              </td>
              <!-- A count, not the list. The list was one cell holding an
                   unbounded number of names, so the column was sized by
                   whichever server had the most - the full names stay one
                   hover away, and the Edit dialog is where they are set. -->
              <td>
                <span v-if="!srv.allowed_domains?.length" class="text-muted">Any</span>
                <span v-else :title="srv.allowed_domains.join(', ')">
                  {{ srv.allowed_domains.length }}
                </span>
              </td>
              <td>{{ srv.security_mode === 'strict' ? 'Yes' : 'No' }}</td>
              <td>
                <span v-if="srv.status === 'enabled'" class="badge badge-success badge-dot"
                  >Enabled</span
                >
                <span
                  v-else-if="srv.status === 'invalid'"
                  class="badge badge-danger badge-dot"
                  :title="srv.validation_error || 'Connection test failed'"
                  >Invalid</span
                >
                <span v-else class="badge badge-neutral badge-dot">Disabled</span>
              </td>
              <td>
                <div class="table-actions">
                  <button
                    class="btn btn-secondary btn-sm"
                    :disabled="testing === srv.id"
                    @click="test(srv)"
                  >
                    {{ testing === srv.id ? 'Testing...' : 'Test' }}
                  </button>
                  <button class="btn btn-secondary btn-sm" @click="open(srv)">Edit</button>
                  <button class="btn btn-secondary btn-sm" @click="toggle(srv)">
                    {{ srv.status === 'enabled' ? 'Disable' : 'Enable' }}
                  </button>
                  <button class="btn btn-danger btn-sm" @click="remove(srv)">Delete</button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <SharedServerForm
      v-if="showForm"
      :providers="providers"
      :server="editing"
      @saved="saved"
      @close="showForm = false"
    />
  </div>
</template>
