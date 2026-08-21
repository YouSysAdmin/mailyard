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
import { sandboxApi, type SandboxEmail, type SandboxInfo } from '../../api/sandbox'
import type { SMTPCredential } from '../../api/types'
import { apiErrorMessage } from '../../api/client'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import { useConfirm } from '../../composables/useConfirm'
import { formatTimeParts } from '../../composables/formatDate'
import { useAutoRefresh } from '../../composables/useAutoRefresh'
import RefreshControl from '../../components/RefreshControl.vue'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import PageHeader from '../../components/PageHeader.vue'
import MessageListRow from '../../components/MessageListRow.vue'
import SandboxReader from './SandboxReader.vue'
import SandboxConnection from './SandboxConnection.vue'

const PAGE_SIZE = 25

const route = useRoute()
const router = useRouter()
const notify = useNotificationStore()
const projStore = useProjectStore()
const { confirm } = useConfirm()

const loading = ref(true)
const loadingMore = ref(false)
const emails = ref<SandboxEmail[]>([])
const total = ref(0)
// True once older captures have been pulled in, which is what keeps an
// automatic refresh from collapsing the list back to the newest page.
const pagedBack = ref(false)
const info = ref<SandboxInfo | null>(null)
const deletingId = ref<string | null>(null)
const clearing = ref(false)

const credentials = ref<SMTPCredential[]>([])

const hasMore = computed(() => emails.value.length < total.value)

// The selection lives in the URL, not in a ref. One source of truth, so
// a deep link, the back button and a click in the list all arrive the
// same way.
const selectedId = computed(() => (route.params.id ? String(route.params.id) : ''))

function select(id: string) {
  if (id === selectedId.value) return
  // replace, not push: reading down a list of captures is not twenty
  // steps of history to walk back out through.
  router.replace(`/sandbox/${id}`)
}

// Connection details live in a DIALOG, not on the page.
//
// Everything in it - host, port, credentials, and the warning when the
// listener is off - is read once, when somebody wires an application
// up. On a page people leave open while a suite runs it was three
// hundred pixels of permanent furniture above the thing they came to
// read.
const showConnection = ref(false)

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
    const res = await sandboxApi.list({ limit: PAGE_SIZE, offset: 0 })
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

async function loadMore() {
  loadingMore.value = true
  try {
    const res = await sandboxApi.list({ limit: PAGE_SIZE, offset: emails.value.length })
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
  router.replace(next ? `/sandbox/${next.id}` : '/sandbox')
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

async function clearAll() {
  const ok = await confirm({
    title: 'Empty the sandbox',
    message: `Delete all ${total.value} captured messages? Nothing here was ever delivered, so this affects no recipient.`,
    confirmText: 'Delete all',
    variant: 'danger',
  })
  if (!ok) return
  clearing.value = true
  try {
    const res = await sandboxApi.clear()
    notify.success(`Deleted ${res.data.deleted} messages`)
    // Emptied locally rather than refetched. The server just told us
    // it deleted everything, so a reload asks a question whose answer
    // we hold - and when this list is served by a read replica, that
    // question can briefly come back with the rows we were just told
    // are gone.
    emails.value = []
    total.value = 0
    if (selectedId.value) router.replace('/sandbox')
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to empty the sandbox'))
  } finally {
    clearing.value = false
  }
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
    if (!sel && list.length > 0) router.replace(`/sandbox/${list[0].id}`)
  },
  { immediate: true },
)

onMounted(() => {
  load()
  loadInfo()
  loadCredentials()
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
      <template v-if="emails.length > 0 && projStore.can('sandbox:delete')">
        <button class="btn btn-danger" :disabled="clearing" @click="clearAll">
          {{ clearing ? 'Deleting...' : 'Empty sandbox' }}
        </button>
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
