<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { auditApi, type AuditEvent } from '../../api/audit'
import { apiErrorMessage } from '../../api/client'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import { useAuthStore } from '../../stores/auth'
import { formatDate } from '../../composables/formatDate'
import { useAutoRefresh } from '../../composables/useAutoRefresh'
import RefreshControl from '../../components/RefreshControl.vue'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import PageHeader from '../../components/PageHeader.vue'
import AuditEventDetail from './AuditEventDetail.vue'
import AuditExport from './AuditExport.vue'

const notify = useNotificationStore()
const projStore = useProjectStore()
const auth = useAuthStore()

type Tab = 'project' | 'security'
// The project trail needs audit:read on the server, while the
// security trail is every caller's own - /api/security-log carries no
// project gate at all. Opening on the tab this user cannot read would
// greet them with a 403 toast on a page that does have something for
// them.
const tab = ref<Tab>(projStore.can('audit:read') ? 'project' : 'security')
const events = ref<AuditEvent[]>([])
const loading = ref(true)
const allAccounts = ref(false)

const PAGE = 50
const offset = ref(0)
// The endpoints page by limit/offset with no total, so paging is
// driven by whether a full page came back.
const hasMore = computed(() => events.value.length === PAGE)

async function load(quiet = false) {
  if (!quiet) loading.value = true
  try {
    const res =
      tab.value === 'project'
        ? await auditApi.projectLog(PAGE, offset.value)
        : await auditApi.securityLog(PAGE, offset.value, allAccounts.value)
    events.value = res.data.events ?? []
  } catch (e) {
    // A failed automatic refresh keeps the rows already on screen: they
    // are the last thing the trail actually said.
    if (!quiet) {
      events.value = []
      notify.error(apiErrorMessage(e, 'Failed to load the audit log'))
    }
  } finally {
    if (!quiet) loading.value = false
  }
}

function switchTab(next: Tab) {
  if (tab.value === next) return
  tab.value = next
  offset.value = 0
  load()
}

function page(delta: number) {
  offset.value = Math.max(0, offset.value + delta * PAGE)
  load()
}

// Failure events are the ones worth spotting in a wall of rows.
function isFailure(e: AuditEvent) {
  return e.type.endsWith('.failed') || e.type.endsWith('.denied')
}

// The row opens the whole record.
//
// The method and full path go in the modal rather than a column. They
// are the widest thing an event carries and would push the columns
// people scan by off the screen. Nothing is fetched - the list response
// already has every field, so the modal reads the row it was given.
const selected = ref<AuditEvent | null>(null)

// Export
const showExport = ref(false)

function openExport() {
  // Empty means the whole trail, which is the ordinary ask - so the
  // dates start blank rather than defaulting to a window somebody then
  // has to notice and widen.
  showExport.value = true
}

// The trail is written by everybody else, so the page keeps itself
// current. Paused on an older page: this list pages by offset, and rows
// arriving at the top would shift what page 2 even means.
const { refreshing, refresh, auto, paused, everySeconds } = useAutoRefresh(() => load(true), {
  storageKey: 'mailyard.autorefresh.audit',
  pauseWhen: () => offset.value > 0,
})

// Back to the first page, like the allAccounts watcher below. Without
// the reset, paging into project A's history and then switching to a
// project with fewer events kept the offset and showed the "Nothing
// recorded yet" empty state for a trail that has records.
watch(
  () => projStore.currentProjectId,
  () => {
    offset.value = 0
    load()
  },
)
watch(allAccounts, () => {
  offset.value = 0
  load()
})
onMounted(() => load())
</script>

<template>
  <div>
    <PageHeader title="Audit Log">
      <RefreshControl
        :every-seconds="everySeconds"
        :refreshing="refreshing"
        :auto="auto"
        :paused="paused"
        @refresh="refresh"
        @update:auto="auto = $event"
      />
      <button class="btn btn-secondary" @click="openExport">Export</button>
    </PageHeader>

    <div class="tabs">
      <button
        v-if="projStore.can('audit:read')"
        :class="['tab', { active: tab === 'project' }]"
        @click="switchTab('project')"
      >
        Project activity
      </button>
      <button :class="['tab', { active: tab === 'security' }]" @click="switchTab('security')">
        Account security
      </button>
    </div>

    <div class="card">
      <div class="card-body">
        <p v-if="tab === 'project'" class="text-sm text-muted">
          Configuration changes in {{ projStore.currentProject?.name || 'this project' }} -
          credentials, templates, servers, and webhooks. Requires the project admin role.
        </p>
        <p v-else class="text-sm text-muted">
          Sign-ins, two-factor changes, and password resets for your account.
        </p>
        <label v-if="tab === 'security' && auth.isAdmin" class="all-toggle">
          <input v-model="allAccounts" type="checkbox" />
          <span>Show every account (platform admin)</span>
        </label>
      </div>

      <LoadingBlock v-if="loading" />

      <template v-else>
        <EmptyState
          v-if="events.length === 0"
          title="Nothing recorded yet"
          text="Actions appear here as they happen."
        />

        <div v-else class="table-wrapper">
          <table>
            <thead>
              <tr>
                <th>When</th>
                <th>Event</th>
                <th>Actor</th>
                <th>IP</th>
                <th>Detail</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="e in events" :key="e.id" class="row-clickable" @click="selected = e">
                <td class="nowrap">{{ formatDate(e.created_at) }}</td>
                <td>
                  <span class="badge" :class="isFailure(e) ? 'badge-danger' : 'badge-info'">
                    {{ e.type }}
                  </span>
                </td>
                <td>{{ e.actor_email || '-' }}</td>
                <td>
                  <code v-if="e.client_ip">{{ e.client_ip }}</code
                  ><span v-else>-</span>
                </td>
                <td class="detail">{{ e.detail || '-' }}</td>
              </tr>
            </tbody>
          </table>
        </div>

        <div v-if="offset > 0 || hasMore" class="pager">
          <button class="btn btn-secondary btn-sm" :disabled="offset === 0" @click="page(-1)">
            Previous
          </button>
          <span class="text-sm text-muted">Showing from {{ offset + 1 }}</span>
          <button class="btn btn-secondary btn-sm" :disabled="!hasMore" @click="page(1)">
            Next
          </button>
        </div>
      </template>
    </div>

    <!-- One event, in full -->
    <AuditEventDetail v-if="selected" :event="selected" @close="selected = null" />

    <AuditExport
      v-if="showExport"
      :trail="tab"
      :all-accounts="allAccounts"
      @close="showExport = false"
    />
  </div>
</template>

<style scoped>
.tabs {
  display: flex;
  gap: 4px;
  margin-bottom: 14px;
}
.all-toggle {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 10px;
  font-size: 13px;
  cursor: pointer;
}
.nowrap {
  white-space: nowrap;
}
.detail {
  max-width: 420px;
  word-break: break-word;
}
</style>
