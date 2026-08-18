<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { emailsApi } from '../../api/emails'
import { apiErrorMessage } from '../../api/client'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import type { Email } from '../../api/types'
import { formatDate } from '../../composables/formatDate'
import { useAutoRefresh } from '../../composables/useAutoRefresh'
import RefreshControl from '../../components/RefreshControl.vue'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import PageHeader from '../../components/PageHeader.vue'

const PAGE_SIZE = 25

const router = useRouter()
const notify = useNotificationStore()
const projStore = useProjectStore()

const loading = ref(true)
const loadingMore = ref(false)
const emails = ref<Email[]>([])
const status = ref('')
// The term the last load ran with. Search is SUBMITTED, not typed:
// this table grows per message, so a query per keystroke is a scan per
// keystroke. Separate from the input so loadMore pages the search that
// is on screen rather than whatever is half-typed in the box.
const search = ref('')
const searchInput = ref('')
// Server keyset paging: when a page comes back full there may be more
// rows before the last created_at cursor.
const hasMore = ref(false)
const retryingId = ref<string | null>(null)
// True once older pages have been pulled in, which is what stops an
// automatic refresh from collapsing the list back to the newest page.
const pagedBack = ref(false)

const STATUS_OPTIONS = [
  { value: '', label: 'All statuses' },
  { value: 'pending', label: 'Pending' },
  { value: 'queued', label: 'Queued' },
  { value: 'processing', label: 'Processing' },
  { value: 'sent', label: 'Sent' },
  { value: 'failed', label: 'Failed' },
  { value: 'suppressed', label: 'Suppressed' },
  { value: 'scheduled', label: 'Scheduled' },
]

async function load(quiet = false) {
  if (!quiet) loading.value = true
  try {
    const res = await emailsApi.list({
      status: status.value || undefined,
      search: search.value || undefined,
      limit: PAGE_SIZE,
    })
    emails.value = res.data.emails ?? []
    hasMore.value = emails.value.length === PAGE_SIZE
  } catch (e) {
    // A failed automatic refresh is not worth a toast every ten
    // seconds - the rows on screen are still the last good answer.
    if (!quiet) notify.error(apiErrorMessage(e, 'Failed to load emails'))
  } finally {
    if (!quiet) loading.value = false
  }
}

async function loadMore() {
  const last = emails.value[emails.value.length - 1]
  if (!last || loadingMore.value) return
  loadingMore.value = true
  try {
    const res = await emailsApi.list({
      status: status.value || undefined,
      search: search.value || undefined,
      limit: PAGE_SIZE,
      before: last.created_at,
      before_id: last.id,
    })
    const batch = res.data.emails ?? []
    emails.value = emails.value.concat(batch)
    pagedBack.value = true
    hasMore.value = batch.length === PAGE_SIZE
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load more emails'))
  } finally {
    loadingMore.value = false
  }
}

function runSearch() {
  const next = searchInput.value.trim()
  if (next === search.value) return
  search.value = next
  load()
}

function clearSearch() {
  searchInput.value = ''
  runSearch()
}

// Rows here are written by the sender, not by the reader, so the page
// keeps itself current. Paused once the reader has pulled in older pages:
// a refresh returns the newest page, and doing that under somebody
// reading history is worse than stale data.
const { refreshing, refresh, auto, paused, everySeconds } = useAutoRefresh(() => load(true), {
  storageKey: 'mailyard.autorefresh.emails',
  pauseWhen: () => pagedBack.value,
})

onMounted(() => load())
watch(status, () => {
  pagedBack.value = false
  load()
})
// Reload when the active project changes.
watch(
  () => projStore.currentProjectId,
  () => {
    pagedBack.value = false
    load()
  },
)

async function retryEmail(em: Email) {
  if (retryingId.value) return
  retryingId.value = em.id
  try {
    const res = await emailsApi.retry(em.id)
    em.status = res.data.email.status
    em.error_message = undefined
    notify.success('Email re-queued for delivery')
  } catch (err) {
    notify.error(apiErrorMessage(err, 'Failed to retry email'))
  } finally {
    retryingId.value = null
  }
}

// Compact recipients summary so wide fan-out sends do not blow up rows.
function recipientsSummary(recipients: string[]): string {
  if (!recipients || recipients.length === 0) return '-'
  if (recipients.length <= 2) return recipients.join(', ')
  return `${recipients[0]} and ${recipients.length - 1} more`
}
</script>

<template>
  <div>
    <PageHeader title="Emails">
      <RefreshControl
        :every-seconds="everySeconds"
        :refreshing="refreshing"
        :auto="auto"
        :paused="paused"
        @refresh="refresh"
        @update:auto="auto = $event"
      />
      <button
        v-if="projStore.can('emails:write')"
        class="btn btn-primary"
        @click="router.push('/emails/send')"
      >
        Send email
      </button>
    </PageHeader>

    <div class="card">
      <div class="card-body emails-filter">
        <!-- No labels: the first option reads "All statuses" and the
             placeholder says what the box takes, so a label above each
             one repeated the control and cost a line of height. The
             name a screen reader needs is on aria-label instead. -->
        <div class="emails-filter__field">
          <select v-model="status" class="form-select" aria-label="Filter by status">
            <option v-for="o in STATUS_OPTIONS" :key="o.value" :value="o.value">
              {{ o.label }}
            </option>
          </select>
        </div>
        <div class="emails-filter__field emails-filter__field--grow">
          <div class="flex gap-2">
            <input
              v-model="searchInput"
              class="form-input flex-1"
              type="search"
              placeholder="Recipient address or subject"
              aria-label="Search by recipient address or subject"
              title="A whole recipient address, or part of the subject. Not the message body."
              @keyup.enter="runSearch"
            />
            <button class="btn btn-secondary" @click="runSearch">Search</button>
            <button v-if="search" class="btn btn-secondary" @click="clearSearch">Clear</button>
          </div>
        </div>
      </div>

      <LoadingBlock v-if="loading" />

      <template v-else>
        <EmptyState v-if="emails.length === 0" title="No emails found">
          <p v-if="status">No emails with this status yet. Try another filter.</p>
          <p v-else>Emails sent through the API or console will appear here.</p>
        </EmptyState>
        <template v-else>
          <div class="table-wrapper">
            <table>
              <thead>
                <tr>
                  <th>Subject</th>
                  <th>From</th>
                  <th>Recipients</th>
                  <th>Template</th>
                  <th>Status</th>
                  <!-- One column for both, because the question asked of
                       a delivery log is "did this land and was it read",
                       and two more columns for two small numbers pushes
                       the subject off a narrow screen. -->
                  <th title="Opens / clicks. A dash means the message went out untracked.">
                    Opens
                  </th>
                  <th>Created</th>
                  <th class="col-actions"></th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="email in emails"
                  :key="email.id"
                  class="row-clickable"
                  @click="router.push(`/emails/${email.id}`)"
                >
                  <td>{{ email.subject }}</td>
                  <td>{{ email.sender }}</td>
                  <td :title="email.recipients.join(', ')">
                    {{ recipientsSummary(email.recipients) }}
                  </td>
                  <td>{{ email.template_name || '-' }}</td>
                  <td><StatusBadge :status="email.status" scope="email" /></td>
                  <td class="text-sm">
                    <span v-if="!email.tracked" class="text-muted" title="Sent without tracking"
                      >-</span
                    >
                    <span v-else :class="email.opened_at ? '' : 'text-muted'">
                      {{ email.open_count ?? 0 }} / {{ email.click_count ?? 0 }}
                    </span>
                  </td>
                  <td>{{ formatDate(email.created_at) }}</td>
                  <td class="col-actions">
                    <button
                      v-if="email.status === 'failed' && projStore.can('emails:write')"
                      class="btn btn-secondary btn-sm"
                      :disabled="retryingId === email.id"
                      @click.stop="retryEmail(email)"
                    >
                      {{ retryingId === email.id ? 'Retrying...' : 'Retry' }}
                    </button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          <div v-if="hasMore" class="load-more">
            <button class="btn btn-secondary" :disabled="loadingMore" @click="loadMore">
              {{ loadingMore ? 'Loading...' : 'Load more' }}
            </button>
          </div>
        </template>
      </template>
    </div>
  </div>
</template>

<style scoped>
.emails-filter {
  display: flex;
  flex-wrap: wrap;
  gap: 16px;
  align-items: flex-end;
}

.emails-filter__field {
  min-width: 180px;
  max-width: 240px;
}

/* The search box needs room the fixed-width filters do not, so it
   overrides the shared max-width rather than the block growing a
   second field definition. */
.emails-filter__field--grow {
  max-width: none;
  flex: 1;
  min-width: 320px;
}

/* Reserve a stable width for the action column so the table does not
   shift depending on whether a row has a Retry button. */
.col-actions {
  width: 96px;
  text-align: right;
}
</style>
