<script setup lang="ts">
// Received mail, as a mail client rather than a list that navigates
// away.
//
// The same shape as the sandbox, deliberately: the two sit in one nav
// section, they are the same act - reading down a list of mail that
// arrived - and they looked like different products. The page is a
// fixed-height shell, the list scrolls on the left, the reader fills the
// right, and the browser window itself does not scroll.
//
// The route still carries the id, so a link to one message opens the
// page with it selected.
import { ref, computed, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { inboundApi, type InboundEmail } from '../../api/inbound'
import { apiErrorMessage } from '../../api/client'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import { useConfirm } from '../../composables/useConfirm'
import { formatTimeParts } from '../../composables/formatDate'
import { useAutoRefresh } from '../../composables/useAutoRefresh'
import RefreshControl from '../../components/RefreshControl.vue'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import PageHeader from '../../components/PageHeader.vue'
import StatCard from '../../components/StatCard.vue'
import MessageListRow from '../../components/MessageListRow.vue'
import InboundReader from './InboundReader.vue'

const PAGE_SIZE = 25

const route = useRoute()
const router = useRouter()
const notify = useNotificationStore()
const projStore = useProjectStore()
const { confirm } = useConfirm()

const loading = ref(true)
const loadingMore = ref(false)
const emails = ref<InboundEmail[]>([])
const status = ref('')
const counts = ref<Record<string, number>>({})
// Server keyset paging over received_at: a full page means there may be
// more rows before the last cursor.
const hasMore = ref(false)
// True once older pages have been pulled in, which is what keeps an
// automatic refresh from collapsing the list back to the newest page.
const pagedBack = ref(false)
const deletingId = ref<string | null>(null)

const STATUS_OPTIONS = [
  { value: '', label: 'All statuses' },
  { value: 'received', label: 'Received' },
  { value: 'rejected', label: 'Rejected' },
  { value: 'failed', label: 'Failed' },
]

// The selection lives in the URL, not in a ref. One source of truth, so
// a deep link, the back button and a click in the list all arrive the
// same way.
const selectedId = computed(() => (route.params.id ? String(route.params.id) : ''))

function select(id: string) {
  if (id === selectedId.value) return
  // replace, not push: reading down a list of messages is not twenty
  // steps of history to walk back out through.
  router.replace(`/inbound-emails/${id}`)
}

async function load(quiet = false) {
  if (!quiet) loading.value = true
  try {
    const res = await inboundApi.list({ status: status.value || undefined, limit: PAGE_SIZE })
    emails.value = res.data.inbound_emails ?? []
    hasMore.value = emails.value.length === PAGE_SIZE
  } catch (e) {
    // A failed automatic refresh leaves the last good rows on screen
    // rather than raising a toast every ten seconds.
    if (!quiet) notify.error(apiErrorMessage(e, 'Failed to load inbound emails'))
  } finally {
    if (!quiet) loading.value = false
  }
}

async function loadStats() {
  try {
    counts.value = (await inboundApi.stats()).data.counts ?? {}
  } catch {
    // The counts are a summary of the list below them. Failing to read
    // them must not hide the messages, which are the point of the page.
  }
}

async function loadMore() {
  const last = emails.value[emails.value.length - 1]
  if (!last || loadingMore.value) return

  loadingMore.value = true
  try {
    const res = await inboundApi.list({
      status: status.value || undefined,
      limit: PAGE_SIZE,
      before: last.received_at,
      before_id: last.id,
    })
    const batch = res.data.inbound_emails ?? []
    emails.value = emails.value.concat(batch)
    pagedBack.value = true
    hasMore.value = batch.length === PAGE_SIZE
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load more inbound emails'))
  } finally {
    loadingMore.value = false
  }
}

function loadAll(quiet = false) {
  load(quiet)
  loadStats()
}

// Drops one message from the list and, when it was the one being read,
// moves the selection off it rather than leaving the reader pointed at
// something the server no longer has.
function forget(id: string) {
  const at = emails.value.findIndex((x) => x.id === id)
  emails.value = emails.value.filter((x) => x.id !== id)
  notify.success('Inbound email deleted')
  loadStats()
  if (selectedId.value !== id) return

  const next = emails.value[at] ?? emails.value[at - 1]
  router.replace(next ? `/inbound-emails/${next.id}` : '/inbound-emails')
}

/** Deleting from the ROW, which the reader does not have to be open for. */
async function deleteEmail(em: InboundEmail) {
  if (deletingId.value) return

  const ok = await confirm({
    title: 'Delete inbound email',
    message: `Delete the message from ${em.sender}? This cannot be undone.`,
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!ok) return

  deletingId.value = em.id
  try {
    await inboundApi.remove(em.id)
    forget(em.id)
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to delete inbound email'))
  } finally {
    deletingId.value = null
  }
}

// A received message is minutes to days old, never years, so the full
// locale string spends three lines saying things nobody reads.
function formatReceived(date: string): string {
  return formatTimeParts(date, {
    day: 'numeric',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
  })
}

// Mail arrives here on its own, so the page keeps itself current. Only
// the list and its counts: everything else on screen belongs to the
// message being read, and refetching that under somebody would move the
// text they are looking at.
const { refreshing, refresh, auto, paused, everySeconds } = useAutoRefresh(() => loadAll(true), {
  storageKey: 'mailyard.autorefresh.inbound',
  pauseWhen: () => pagedBack.value,
})

// Landing on the list with messages already loaded opens the newest one.
// Only when nothing is selected - it must never steal a selection back.
watch(
  [emails, selectedId],
  ([list, sel]) => {
    if (!sel && list.length > 0) router.replace(`/inbound-emails/${list[0].id}`)
  },
  { immediate: true },
)

watch(status, () => {
  pagedBack.value = false
  // The rows AND the selection belong to the PREVIOUS filter, and both
  // go before the load. The selection, so the auto-select watch can
  // open the newest message of the new filter - and so an empty answer
  // leaves the reader pane free to say so instead of showing a message
  // the list no longer lists. The rows, because dropping the selection
  // alone is a race: the route updates before the fetch answers, and
  // the auto-select watch would put the old filter's newest message
  // right back.
  emails.value = []
  hasMore.value = false
  if (selectedId.value) router.replace('/inbound-emails')
  load()
})

watch(
  () => projStore.currentProjectId,
  () => {
    pagedBack.value = false
    loadAll()
  },
)

onMounted(() => loadAll())
</script>

<template>
  <div class="reader-page">
    <PageHeader title="Inbound Emails">
      <RefreshControl
        :every-seconds="everySeconds"
        :refreshing="refreshing"
        :auto="auto"
        :paused="paused"
        @refresh="refresh"
        @update:auto="auto = $event"
      />
    </PageHeader>

    <div class="stats-grid inbound-stats">
      <StatCard label="Received" icon="received" :value="String(counts.received ?? 0)" />
      <StatCard label="Rejected" icon="rejected" :value="String(counts.rejected ?? 0)" />
      <StatCard label="Failed" icon="failed" :value="String(counts.failed ?? 0)" />
    </div>

    <LoadingBlock v-if="loading" />

    <!-- The split renders whether or not there is anything in it. It
         used to be replaced by an EmptyState when the list was empty,
         and the STATUS FILTER lives inside the split - so a filter that
         matched nothing removed the one control that could undo it,
         and the only way out was reloading the page. An empty list is
         now said in the reader pane, with every control still standing.

         The split is the only thing that grows, so the two panes share
         exactly what the page has left and neither the window nor the
         panes' parent scrolls. -->
    <div v-else class="card reader-split">
      <div class="list-pane">
        <!-- The filter rides with the list rather than sitting in a card
             of its own above the split: it narrows the left pane and
             nothing else, and as a separate row it took height from the
             thing being read. -->
        <div class="list-filter">
          <select v-model="status" class="form-select" aria-label="Status">
            <option v-for="o in STATUS_OPTIONS" :key="o.value" :value="o.value">
              {{ o.label }}
            </option>
          </select>
        </div>

        <MessageListRow
          v-for="em in emails"
          :key="em.id"
          :subject="em.subject"
          :time="formatReceived(em.received_at)"
          :sender="em.sender"
          :recipients="em.recipients"
          :selected="em.id === selectedId"
          :deletable="projStore.can('inbound:delete')"
          :deleting="deletingId === em.id"
          delete-label="Delete this message"
          @open="select(em.id)"
          @delete="deleteEmail(em)"
        >
          <!-- Received is the ordinary case and says nothing worth a
               badge. The other two are why somebody opened this page. -->
          <template v-if="em.status !== 'received'" #badge>
            <StatusBadge :status="em.status" scope="inbound" />
          </template>
        </MessageListRow>

        <div v-if="hasMore" class="list-more">
          <button class="btn btn-secondary btn-sm" :disabled="loadingMore" @click="loadMore">
            {{ loadingMore ? 'Loading...' : 'Load older' }}
          </button>
        </div>
      </div>

      <div class="reader-pane">
        <InboundReader v-if="selectedId" :id="selectedId" @delete="deleteEmail" />
        <EmptyState v-else-if="emails.length === 0" title="No inbound emails">
          <p v-if="status">No messages with this status yet. Try another filter.</p>
          <p v-else>Messages received for your verified domains will appear here.</p>
        </EmptyState>
        <EmptyState v-else title="Nothing selected">
          <p>Pick a message on the left to read it.</p>
        </EmptyState>
      </div>
    </div>
  </div>
</template>

<style scoped>
.inbound-stats {
  margin-bottom: 16px;
}
</style>
