<script setup lang="ts">
// The project's machine credentials.
//
// A key is minted once and never edited: its permissions, its sandbox
// flag, its address list and its expiry are all fixed at creation. So
// this page has three states and they are three components - the list,
// a create dialog, and a record of an existing key.
import { computed, ref, watch } from 'vue'
import { apiKeysApi } from '../../api/apikeys'
import { permissionsApi } from '../../api/projects'
import { apiErrorMessage } from '../../api/client'
import type { APIKey, PermissionResource } from '../../api/types'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import { useConfirm } from '../../composables/useConfirm'
import { useClientPager } from '../../composables/usePagination'
import { formatDate } from '../../composables/formatDate'
import Pagination from '../../components/Pagination.vue'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import PageHeader from '../../components/PageHeader.vue'
import ApiKeyCreate from './ApiKeyCreate.vue'
import ApiKeyDetail from './ApiKeyDetail.vue'
import ApiKeyToken from './ApiKeyToken.vue'

const notify = useNotificationStore()
const projects = useProjectStore()
const { confirm } = useConfirm()

const keys = ref<APIKey[]>([])

// The catalogue comes from the server that enforces it, so the grid
// cannot offer a permission no route honours.
const catalog = ref<PermissionResource[]>([])
const loading = ref(true)

const { pageable, pageItems, goToPage } = useClientPager(keys)

const creating = ref(false)
const viewing = ref<APIKey | null>(null)
const minted = ref<{ key: APIKey; token: string } | null>(null)

async function load() {
  loading.value = true
  try {
    const [list, cat] = await Promise.all([apiKeysApi.list(), permissionsApi.catalog()])
    keys.value = list.data.api_keys ?? []
    catalog.value = cat.data.resources ?? []
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load the API keys'))
  } finally {
    loading.value = false
  }
}

function onCreated(key: APIKey, token: string) {
  creating.value = false
  // Straight into the token dialog: the plaintext is readable exactly
  // once, so nothing may sit between minting it and showing it.
  minted.value = { key, token }
  notify.success('API key created')
  void load()
}

function expired(key: APIKey): boolean {
  return !!key.expires_at && new Date(key.expires_at) < new Date()
}

/** Revoked, expired or working - in that order, because revoked wins. */
function state(key: APIKey): { label: string; variant: string } {
  if (key.revoked) return { label: 'Revoked', variant: 'badge-danger' }
  if (expired(key)) return { label: 'Expired', variant: 'badge-warning' }

  return { label: 'Active', variant: 'badge-success' }
}

async function revoke(key: APIKey) {
  const ok = await confirm({
    title: 'Revoke this key',
    message: `"${key.name}" stops working immediately and cannot be turned back on. Anything using it starts failing.`,
    confirmText: 'Revoke',
    variant: 'danger',
  })
  if (!ok) return

  try {
    await apiKeysApi.revoke(key.id)
    notify.success('Key revoked')
    await load()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to revoke the key'))
  }
}

async function remove(key: APIKey) {
  const ok = await confirm({
    title: 'Delete this key',
    message: `Delete "${key.name}"? Revoking already stops it working - deleting also drops the record that it existed.`,
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!ok) return

  try {
    await apiKeysApi.remove(key.id)
    notify.success('Key deleted')
    await load()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to delete the key'))
  }
}

const anyRowActions = computed(
  () => projects.can('apikeys:write') || projects.can('apikeys:delete'),
)

watch(() => projects.currentProjectId, load)

void load()
</script>

<template>
  <div>
    <PageHeader title="API keys">
      <button v-if="projects.can('apikeys:write')" class="btn btn-primary" @click="creating = true">
        New key
      </button>
    </PageHeader>

    <LoadingBlock v-if="loading" />

    <div v-else class="card">
      <EmptyState
        v-if="keys.length === 0"
        title="No API keys"
        text="A key is how something other than a person sends through this project."
      />

      <template v-else>
        <div class="table-wrapper">
          <table>
            <thead>
              <tr>
                <th>Name</th>
                <th>Prefix</th>
                <th>Mode</th>
                <th>State</th>
                <th>Last used</th>
                <th>Expires</th>
                <th v-if="anyRowActions" class="text-right"></th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="key in pageItems"
                :key="key.id"
                class="row-clickable"
                @click="viewing = key"
              >
                <td>
                  <strong>{{ key.name }}</strong>
                </td>
                <td>
                  <code>{{ key.key_prefix }}...</code>
                </td>
                <td>
                  <span class="badge" :class="key.sandbox ? 'badge-warning' : 'badge-neutral'">
                    {{ key.sandbox ? 'sandbox' : 'live' }}
                  </span>
                </td>
                <td>
                  <span class="badge badge-dot" :class="state(key).variant">
                    {{ state(key).label }}
                  </span>
                </td>
                <td>{{ formatDate(key.last_used_at, 'Never') }}</td>
                <td>{{ formatDate(key.expires_at, 'Never') }}</td>
                <td v-if="anyRowActions" class="text-right">
                  <div class="table-actions" @click.stop>
                    <!-- Nothing to revoke on a key that is already
                         revoked or past its date. -->
                    <button
                      v-if="projects.can('apikeys:write') && !key.revoked && !expired(key)"
                      class="btn btn-warning btn-sm"
                      @click="revoke(key)"
                    >
                      Revoke
                    </button>
                    <button
                      v-if="projects.can('apikeys:delete')"
                      class="btn btn-danger btn-sm"
                      @click="remove(key)"
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

    <ApiKeyCreate
      v-if="creating"
      :catalog="catalog"
      @created="onCreated"
      @close="creating = false"
    />

    <ApiKeyDetail v-if="viewing" :api-key="viewing" :catalog="catalog" @close="viewing = null" />

    <ApiKeyToken v-if="minted" :token="minted.token" :api-key="minted.key" @close="minted = null" />
  </div>
</template>
