<script setup lang="ts">
import { onMounted, ref } from 'vue'
import {
  oauthProvidersApi,
  type OAuthProvider,
  type OAuthTestResult,
} from '../../api/oauthProviders'
import { apiErrorMessage } from '../../api/client'
import { useNotificationStore } from '../../stores/notification'
import { useConfirm } from '../../composables/useConfirm'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import PageHeader from '../../components/PageHeader.vue'
import CopyButton from '../../components/CopyButton.vue'
import OAuthProviderForm from './OAuthProviderForm.vue'
import Notice from '../../components/Notice.vue'

const notify = useNotificationStore()
const { confirm } = useConfirm()

const providers = ref<OAuthProvider[]>([])
const loading = ref(true)
const testingId = ref('')
const testResults = ref<Record<string, OAuthTestResult>>({})

const showModal = ref(false)
const editing = ref<OAuthProvider | null>(null)
async function load() {
  loading.value = true
  try {
    const res = await oauthProvidersApi.list()
    providers.value = res.data.providers ?? []
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load identity providers'))
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editing.value = null
  showModal.value = true
}

function openEdit(p: OAuthProvider) {
  editing.value = p
  showModal.value = true
}

async function onSaved() {
  showModal.value = false
  await load()
}

async function remove(p: OAuthProvider) {
  const ok = await confirm({
    title: 'Delete Provider',
    message: `Delete "${p.name}"? Anyone who signs in only through it will lose access, and their account links are removed.`,
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!ok) return
  try {
    await oauthProvidersApi.remove(p.id)
    notify.success('Provider deleted')
    await load()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to delete provider'))
  }
}

async function runTest(p: OAuthProvider) {
  testingId.value = p.id
  try {
    const res = await oauthProvidersApi.test(p.id)
    testResults.value = { ...testResults.value, [p.id]: res.data.test }
    if (res.data.test.ok) {
      notify.success(`${p.name} responded`)
    } else {
      notify.error(`${p.name}: ${res.data.test.error || 'test failed'}`)
    }
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Test failed'))
  } finally {
    testingId.value = ''
  }
}

onMounted(load)
</script>

<template>
  <div>
    <PageHeader>
      <template #title>
        <h1>Identity Providers</h1>
        <p class="header-sub">
          Single sign-on for the console. Providers apply to the whole installation.
        </p>
      </template>
      <button class="btn btn-primary" @click="openCreate">Add Provider</button>
    </PageHeader>

    <LoadingBlock v-if="loading" />

    <template v-else>
      <div v-if="providers.length === 0" class="card">
        <EmptyState>
          <p>No identity providers are configured.</p>
          <p class="text-sm text-muted">
            Local sign-in with an email and password keeps working either way. Adding a provider
            puts a "Continue with ..." button on the sign-in page.
          </p>
        </EmptyState>
      </div>

      <div v-for="p in providers" :key="p.id" class="card">
        <div class="card-header">
          <div>
            <h2>
              {{ p.name }}
              <span class="badge badge-neutral">{{ p.type }}</span>
              <span v-if="!p.enabled" class="badge badge-neutral">disabled</span>
              <span v-else-if="!p.usable" class="badge badge-warning">incomplete</span>
              <span v-else-if="p.hidden" class="badge badge-info">hidden</span>
              <span v-else class="badge badge-success">on the sign-in page</span>
            </h2>
            <p class="text-sm text-muted">
              <code>{{ p.slug }}</code>
            </p>
          </div>
          <div class="table-actions">
            <button
              class="btn btn-secondary btn-sm"
              :disabled="testingId === p.id"
              @click="runTest(p)"
            >
              {{ testingId === p.id ? 'Testing...' : 'Test' }}
            </button>
            <button class="btn btn-secondary btn-sm" @click="openEdit(p)">Edit</button>
            <button class="btn btn-danger btn-sm" @click="remove(p)">Delete</button>
          </div>
        </div>
        <div class="card-body">
          <div class="detail-row">
            <span class="detail-label">Redirect URI</span>
            <span class="detail-value">
              <code>{{ p.callback_url }}</code>
              <CopyButton :value="p.callback_url" variant="btn btn-secondary btn-sm ml-2" />
            </span>
          </div>
          <div class="detail-row">
            <span class="detail-label">Issuer</span>
            <span class="detail-value">{{
              p.issuer || (p.type === 'google' ? 'accounts.google.com' : '-')
            }}</span>
          </div>
          <div class="detail-row">
            <span class="detail-label">Client ID</span>
            <span class="detail-value">{{ p.client_id || '-' }}</span>
          </div>
          <div class="detail-row">
            <span class="detail-label">Client secret</span>
            <span class="detail-value">{{ p.has_secret ? 'set' : 'not set' }}</span>
          </div>
          <div class="detail-row">
            <span class="detail-label">New accounts</span>
            <span class="detail-value">
              {{ p.auto_register ? 'created on first sign-in' : 'must already exist' }}
            </span>
          </div>
          <div
            v-if="p.allowed_domains.length || p.allowed_emails.length || p.allowed_groups.length"
            class="detail-row"
          >
            <span class="detail-label">Restricted to</span>
            <span class="detail-value">
              <template v-if="p.allowed_emails.length">{{ p.allowed_emails.join(', ') }}</template>
              <template v-else-if="p.allowed_domains.length">
                {{ p.allowed_domains.map((d) => '@' + d).join(', ') }}
              </template>
              <template v-if="p.allowed_groups.length">
                <span v-if="p.allowed_domains.length"> and </span>
                groups {{ p.allowed_groups.join(', ') }}
              </template>
            </span>
          </div>

          <Notice
            v-if="testResults[p.id]"
            :kind="testResults[p.id].ok ? 'success' : 'danger'"
            :title="testResults[p.id].ok ? 'Provider reachable' : 'Test failed'"
          >
            <p v-if="testResults[p.id].error">{{ testResults[p.id].error }}</p>
            <p v-else>
              {{ testResults[p.id].discovered ? 'Discovered' : 'Using the configured endpoints' }}:
              <code>{{ testResults[p.id].auth_url }}</code>
            </p>
            <p v-for="w in testResults[p.id].warnings" :key="w">{{ w }}</p>
          </Notice>
        </div>
      </div>
    </template>

    <!-- Add / Edit modal -->
    <OAuthProviderForm
      v-if="showModal"
      :provider="editing"
      @saved="onSaved"
      @close="showModal = false"
    />
  </div>
</template>

<style scoped>
.detail-row {
  display: flex;
  align-items: baseline;
  gap: 16px;
  padding: 6px 0;
  font-size: 13px;
}
.detail-label {
  min-width: 150px;
  color: var(--text-secondary);
  flex-shrink: 0;
}
.detail-value {
  color: var(--text-primary);
  word-break: break-all;
}
</style>
