<script setup lang="ts">
// The sandbox, as a mail client rather than a list that navigates away.
//
// Developers keep this open while a suite runs, so reading one capture
// must not cost the list. The page is a fixed-height shell: the list
// scrolls on the left, the reader fills the right, and the browser
// window itself does not scroll at all. That is also what removes the
// second scrollbar - a page that scrolls with a message frame that also
// scrolls is two scrollbars for one document.
//
// The route still carries the id, so a link to one capture opens the
// page with it selected.
import { ref, computed, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  sandboxApi,
  type SandboxEmail,
  type SandboxInbox,
  type SandboxInfo,
} from '../../api/sandbox'
import type { SMTPCredential } from '../../api/types'
import { apiErrorMessage } from '../../api/client'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import { formatTimeParts } from '../../composables/formatDate'
import { useAutoRefresh } from '../../composables/useAutoRefresh'
import RefreshControl from '../../components/RefreshControl.vue'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import PageHeader from '../../components/PageHeader.vue'
import MessageListRow from '../../components/MessageListRow.vue'
import MessageSearch, { type MessageSearchTerms } from '../../components/MessageSearch.vue'
import SandboxReader from './SandboxReader.vue'
import SandboxConnection from './SandboxConnection.vue'
import SandboxInboxes from './SandboxInboxes.vue'
import SandboxClear from './SandboxClear.vue'

const PAGE_SIZE = 25

const route = useRoute()
const router = useRouter()
const notify = useNotificationStore()
const projStore = useProjectStore()

const loading = ref(true)
const loadingMore = ref(false)
const emails = ref<SandboxEmail[]>([])
const total = ref(0)
// True once older captures have been pulled in, which is what keeps an
// automatic refresh from collapsing the list back to the newest page.
const pagedBack = ref(false)
const info = ref<SandboxInfo | null>(null)
const deletingId = ref<string | null>(null)
const showClear = ref(false)

const credentials = ref<SMTPCredential[]>([])
const inboxes = ref<SandboxInbox[]>([])

// The applied envelope search. ANDed with the inbox, and kept in the
// page rather than the URL: it is a question asked while reading, not a
// view somebody links to.
const sender = ref('')
const recipient = ref('')
const searching = computed(() => sender.value !== '' || recipient.value !== '')

function search(terms: MessageSearchTerms) {
  sender.value = terms.sender
  recipient.value = terms.recipient
}

const hasMore = computed(() => emails.value.length < total.value)

// The selection lives in the URL, not in a ref. One source of truth, so
// a deep link, the back button and a click in the list all arrive the
// same way. The inbox filter rides beside it as `?inbox=`, for the same
// reason: a refresh or a shared link lands on the same view.
const selectedId = computed(() => (route.params.id ? String(route.params.id) : ''))
const inbox = computed(() => (route.query.inbox ? String(route.query.inbox) : ''))

// Every navigation on this page goes through here so the filter is
// never dropped by a click in the list.
function go(id: string, box = inbox.value) {
  // replace, not push: reading down a list of captures is not twenty
  // steps of history to walk back out through.
  router.replace({ path: id ? `/sandbox/${id}` : '/sandbox', query: box ? { inbox: box } : {} })
}

function select(id: string) {
  if (id === selectedId.value) return
  go(id)
}

// Switching inbox keeps the selection: the watcher below drops it once
// the new page arrives without it, and only then, so the reader does not
// flash empty on every switch.
function setInbox(box: string) {
  if (box === inbox.value) return
  go(selectedId.value, box)
}

const inboxName = computed(() => inboxes.value.find((b) => b.id === inbox.value)?.name ?? '')

// Connection details live in a DIALOG, not on the page.
//
// Everything in it - host, port, credentials, and the warning when the
// listener is off - is read once, when somebody wires an application
// up, and this page is left open while a suite runs.
const showConnection = ref(false)
const showInboxes = ref(false)

const activeCredentials = computed(() => credentials.value.filter((c) => !c.revoked))

// What the button has to say for itself before it is pressed: nothing
// to send with, or a listener that is off, are both worth knowing
// without opening anything.
const connectionNeedsAttention = computed(
  () =>
    activeCredentials.value.length === 0 || (info.value !== null && !info.value.submission.enabled),
)

// Built here rather than in the template, where the whitespace
// between v-if blocks leaks into the rendered text - "7 days ,
// newest 500 messages" is what that looks like.
const keptForLabel = computed(() => {
  const parts: string[] = []
  if (info.value && info.value.retention_days > 0) {
    parts.push(`${info.value.retention_days} days`)
  } else {
    parts.push('as long as there is room')
  }

  if (info.value && info.value.max_messages > 0) {
    parts.push(`newest ${info.value.max_messages} messages`)
  }

  return parts.join(', ')
})

async function load(quiet = false) {
  if (!quiet) loading.value = true
  try {
    const res = await sandboxApi.list({
      limit: PAGE_SIZE,
      offset: 0,
      inbox: inbox.value || undefined,
      sender: sender.value || undefined,
      recipient: recipient.value || undefined,
    })
    emails.value = res.data.sandbox_emails ?? []
    total.value = res.data.total ?? emails.value.length
  } catch (e) {
    // An automatic refresh that failed leaves the captures on screen.
    if (!quiet) notify.error(apiErrorMessage(e, 'Failed to load the sandbox'))
  } finally {
    if (!quiet) loading.value = false
  }
}

async function loadInfo() {
  try {
    info.value = (await sandboxApi.info()).data
  } catch {
    // Connection details are a convenience. Failing to read them must
    // not hide the messages, which are the point of the page.
  }
}

async function loadCredentials() {
  try {
    credentials.value = (await sandboxApi.listCredentials()).data.smtp_credentials ?? []
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load sandbox credentials'))
  }
}

async function loadInboxes() {
  try {
    inboxes.value = (await sandboxApi.listInboxes()).data.sandbox_inboxes ?? []
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load sandbox inboxes'))
  }
}

// An inbox that was deleted, or edited away from under the filter, is
// rereading the list: the dropdown must not keep offering a name the
// server no longer knows, and a filter on a gone inbox is "All mail".
async function inboxesChanged() {
  await loadInboxes()
  if (inbox.value && !inboxes.value.some((b) => b.id === inbox.value)) {
    setInbox('')

    return
  }

  pagedBack.value = false
  load()
}

async function loadMore() {
  loadingMore.value = true
  try {
    const res = await sandboxApi.list({
      limit: PAGE_SIZE,
      offset: emails.value.length,
      inbox: inbox.value || undefined,
      sender: sender.value || undefined,
      recipient: recipient.value || undefined,
    })
    emails.value = emails.value.concat(res.data.sandbox_emails ?? [])
    pagedBack.value = true
    total.value = res.data.total ?? total.value
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load more messages'))
  } finally {
    loadingMore.value = false
  }
}

// Drops one capture from the list and, when it was the one being read,
// moves the selection off it rather than leaving the reader pointed at
// a message the server no longer has.
function forget(id: string) {
  const at = emails.value.findIndex((x) => x.id === id)
  emails.value = emails.value.filter((x) => x.id !== id)
  total.value = Math.max(0, total.value - 1)
  if (selectedId.value !== id) return

  const next = emails.value[at] ?? emails.value[at - 1]
  go(next ? next.id : '')
}

async function deleteEmail(em: SandboxEmail) {
  deletingId.value = em.id
  try {
    await sandboxApi.remove(em.id)
    forget(em.id)
  } catch (err) {
    notify.error(apiErrorMessage(err, 'Failed to delete the message'))
  } finally {
    deletingId.value = null
  }
}

// After the dialog emptied something. Emptied locally rather than
// refetched when the answer is known: the server just said everything
// went, or everything this inbox shows did - and when this list is
// served by a read replica, a reload can briefly come back with the
// rows we were just told are gone. Other inboxes leave rows this page
// may not hold, so that case reloads.
function cleared(inboxIds: string[]) {
  showClear.value = false
  if (inboxIds.length === 0 || inboxIds.includes(inbox.value)) {
    emails.value = []
    total.value = 0
    if (selectedId.value) go('')

    return
  }

  load()
}

// A captured message is minutes to days old, never years, so the full
// locale string spends three lines saying things nobody reads. Day,
// month and time is what identifies a run.
function formatCaptured(date: string): string {
  return formatTimeParts(date, {
    day: 'numeric',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
  })
}

// Captures arrive while a developer's suite runs, so the page keeps
// itself current. Only the message list: the connection details and the
// credentials are configuration, and refetching them every ten seconds
// would ask three questions to answer one.
const { refreshing, refresh, auto, paused, everySeconds } = useAutoRefresh(() => load(true), {
  storageKey: 'mailyard.autorefresh.sandbox',
  pauseWhen: () => pagedBack.value,
})

// Landing on /sandbox with captures already loaded opens the newest one,
// which is what somebody watching a test run wants to see. Only when
// nothing is selected - it must never steal a selection back.
watch(
  [emails, selectedId],
  ([list, sel]) => {
    if (!sel && list.length > 0) go(list[0].id)
  },
  { immediate: true },
)

// A new filter is a new list. The selection is dropped only when the
// page that comes back does not hold it, which is what lets a switch to
// the inbox the open message belongs to keep it open.
watch([inbox, sender, recipient], async () => {
  pagedBack.value = false
  await load()
  if (selectedId.value && !emails.value.some((e) => e.id === selectedId.value)) go('')
})

onMounted(() => {
  load()
  loadInfo()
  loadCredentials()
  loadInboxes()
})
</script>

<template>
  <div class="reader-page">
    <PageHeader title="Inbound Sandbox">
      <RefreshControl
        :every-seconds="everySeconds"
        :refreshing="refreshing"
        :auto="auto"
        :paused="paused"
        @refresh="refresh"
        @update:auto="auto = $event"
      />
      <button class="btn btn-secondary" @click="showConnection = true">
        Connection
        <span v-if="connectionNeedsAttention" class="attn-dot" aria-hidden="true"></span>
      </button>
      <button class="btn btn-secondary" @click="showInboxes = true">Inboxes</button>
      <template v-if="emails.length > 0 && projStore.can('sandbox:delete')">
        <button class="btn btn-danger" @click="showClear = true">Empty sandbox</button>
      </template>
    </PageHeader>

    <LoadingBlock v-if="loading" />

    <!-- The split renders whether or not there is anything in it, the
         same as the inbound page and for the same reason: swapping the
         whole split for an EmptyState makes the page change shape the
         moment the list empties - here, the instant "Empty sandbox"
         finishes - and an empty list is an ordinary state on a page
         people keep open while a suite runs. The reader pane says it.

         The split is the only thing that grows, so the two panes share
         exactly what the page has left and neither the window nor the
         panes' parent scrolls. -->
    <div v-else class="card reader-split">
      <div class="list-pane">
        <div class="list-filter">
          <MessageSearch :sender="sender" :recipient="recipient" @apply="search">
            <!-- Which mail the list shows, the way the status select does on
                 the inbound page. Always shown: "All mail" alone still says
                 what the list is, and the Inboxes button is where the first
                 one gets made. -->
            <select
              :value="inbox"
              class="form-select"
              aria-label="Filter by inbox"
              @change="setInbox(($event.target as HTMLSelectElement).value)"
            >
              <option value="">All mail</option>
              <option v-for="box in inboxes" :key="box.id" :value="box.id">{{ box.name }}</option>
            </select>
          </MessageSearch>
        </div>

        <MessageListRow
          v-for="em in emails"
          :key="em.id"
          :subject="em.subject"
          :time="formatCaptured(em.received_at)"
          :sender="em.sender"
          :recipients="em.recipients"
          :selected="em.id === selectedId"
          :deletable="projStore.can('sandbox:delete')"
          :deleting="deletingId === em.id"
          delete-label="Delete this capture"
          @open="select(em.id)"
          @delete="deleteEmail(em)"
        />

        <div v-if="hasMore" class="list-more">
          <button class="btn btn-secondary btn-sm" :disabled="loadingMore" @click="loadMore">
            {{ loadingMore ? 'Loading...' : `Load older (${emails.length} of ${total})` }}
          </button>
        </div>
      </div>

      <div class="reader-pane">
        <SandboxReader v-if="selectedId" :id="selectedId" @deleted="forget" />
        <EmptyState v-else-if="emails.length === 0 && searching" title="No matching captures">
          <p>Nothing here matches the From and To searched for. Clear them to see more.</p>
        </EmptyState>
        <EmptyState
          v-else-if="emails.length === 0 && inbox"
          :title="`Nothing from ${inboxName || 'this inbox'} yet`"
        >
          <p>
            No capture has an envelope sender on this inbox's list. Pick All mail to see everything
            the sandbox holds.
          </p>
        </EmptyState>
        <EmptyState v-else-if="emails.length === 0" title="Nothing captured yet">
          <p>
            Send a message with a sandbox credential and it will appear here instead of going to a
            recipient.
          </p>
        </EmptyState>
        <EmptyState v-else title="Nothing selected">
          <p>Pick a capture on the left to read it.</p>
        </EmptyState>
      </div>
    </div>

    <SandboxConnection
      v-if="showConnection"
      :info="info"
      :credentials="credentials"
      :kept-for="keptForLabel"
      @changed="loadCredentials"
      @close="showConnection = false"
    />

    <SandboxInboxes
      v-if="showInboxes"
      :inboxes="inboxes"
      @changed="inboxesChanged"
      @close="showInboxes = false"
    />

    <SandboxClear
      v-if="showClear"
      :inboxes="inboxes"
      :total="total"
      :current="inbox"
      @cleared="cleared"
      @close="showClear = false"
    />
  </div>
</template>

<style scoped>
/* The button says something is unset before it is pressed - no
   credential to send with, or a listener that is off. */
.attn-dot {
  display: inline-block;
  width: 6px;
  height: 6px;
  margin-left: 6px;
  border-radius: 50%;
  background: var(--warning-500);
  vertical-align: middle;
}
</style>
