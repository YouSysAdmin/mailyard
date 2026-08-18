<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { emailsApi } from '../../api/emails'
import { apiErrorMessage, browserURL } from '../../api/client'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import type { Email } from '../../api/types'
import { formatDate } from '../../composables/formatDate'
import MessageViewer, { type ViewerAttachment } from '../../components/MessageViewer.vue'
import RefreshControl from '../../components/RefreshControl.vue'
import { useAutoRefresh } from '../../composables/useAutoRefresh'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import PageHeader from '../../components/PageHeader.vue'

const route = useRoute()
const router = useRouter()
const notify = useNotificationStore()
const projStore = useProjectStore()

const loading = ref(true)
const retrying = ref(false)
const email = ref<Email | null>(null)

// The backend streams attachment bytes (inline or blob-store backed)
// from a download endpoint - the URL is the one thing the viewer
// cannot build for itself.
const viewerAttachments = computed<ViewerAttachment[]>(() => {
  const e = email.value
  if (!e) return []

  return (e.attachments ?? []).map((a, i) => ({
    filename: a.filename,
    content_type: a.content_type,
    url: browserURL(`/emails/${e.id}/attachments/${i}`),
  }))
})

// hash -> original URL, so the preview can put real destinations back
// on the links whose tracking redirect it strips. Fetched once per
// page, not per poll: the body never changes after send.
const trackedLinks = ref<Record<string, string>>({})
let linksLoaded = false

async function loadTrackedLinks(e: Email) {
  if (linksLoaded || !e.html_body?.includes('/tracking/click/')) return
  linksLoaded = true
  try {
    const res = await emailsApi.trackedLinks(e.id)
    trackedLinks.value = res.data.links ?? {}
  } catch (err) {
    // The preview still renders, just with the hrefs stripped - the
    // documented fallback, not worth a toast.
    console.error('Failed to load tracked links', err)
  }
}

async function load(quiet = false) {
  try {
    const res = await emailsApi.get(route.params.id as string)
    email.value = res.data.email
    if (email.value) await loadTrackedLinks(email.value)
  } catch (e) {
    // The manual path says so. It used to go to the browser console
    // alone, so a message that failed to load was a page that stayed on
    // "Not found" with nothing to say whether it had been deleted or the
    // request had simply failed. The quiet path stays silent: the row on
    // screen is still the last good answer and this one polls every
    // three seconds while a send is in flight.
    if (!quiet) notify.error(apiErrorMessage(e, 'Failed to load the message'))
  } finally {
    if (!quiet) loading.value = false
  }
}

// A message that has not finished being delivered yet.
//
// `scheduled` is deliberately absent: it is waiting on purpose, possibly
// for days, and polling that is asking a question whose answer is a date
// already on the page.
const IN_FLIGHT = ['pending', 'queued', 'processing']
const inFlight = computed(() => !!email.value && IN_FLIGHT.includes(email.value.status))

// Sending from the console lands on this page, and the status was
// whatever it was at that instant - `queued`, almost always - and stayed
// there until somebody reloaded the browser. So the page follows the
// message until it settles.
//
// Three seconds rather than ten: this is one row by id, the reader is
// watching it, and the whole window is usually shorter than one ten
// second tick. Capped, because a queue that is not running would
// otherwise be polled forever by every open tab - after that the button
// is the way to ask. A Retry resets the count, which is why this pauses
// rather than stopping.
const POLL_LIMIT = 100
let polls = 0
const { refreshing, refresh, everySeconds } = useAutoRefresh(
  () => {
    polls++
    return load(true)
  },
  {
    intervalMs: 3_000,
    pauseWhen: () => !inFlight.value || polls >= POLL_LIMIT,
  },
)

onMounted(() => load())

async function retryEmail() {
  if (!email.value || retrying.value) return
  retrying.value = true
  try {
    const res = await emailsApi.retry(email.value.id)
    email.value = res.data.email
    // Back under the poll: the message is in flight again, and the count
    // that stopped watching the last attempt should not stop this one.
    polls = 0
    notify.success('Email re-queued for delivery')
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to retry email'))
  } finally {
    retrying.value = false
  }
}
</script>

<template>
  <div>
    <PageHeader title="Email Detail">
      <div class="flex gap-2">
        <RefreshControl :refreshing="refreshing" :every-seconds="everySeconds" @refresh="refresh" />
        <button
          v-if="email && email.status === 'failed' && projStore.can('emails:write')"
          class="btn btn-primary"
          :disabled="retrying"
          @click="retryEmail"
        >
          {{ retrying ? 'Retrying...' : 'Retry' }}
        </button>
        <button class="btn btn-secondary" @click="router.push('/emails')">Back to Emails</button>
      </div>
    </PageHeader>

    <LoadingBlock v-if="loading" />

    <template v-else-if="email">
      <div class="card">
        <div class="card-header">
          <h2>{{ email.subject }}</h2>
          <StatusBadge :status="email.status" scope="email" />
        </div>
        <div class="card-body">
          <table>
            <tbody>
              <tr>
                <td class="meta-label">From</td>
                <td>{{ email.sender }}</td>
              </tr>
              <tr>
                <td class="meta-label">To</td>
                <td>{{ email.recipients.join(', ') }}</td>
              </tr>
              <tr>
                <td class="meta-label">Status</td>
                <td><StatusBadge :status="email.status" scope="email" /></td>
              </tr>
              <tr v-if="email.error_message">
                <td class="meta-label">Error</td>
                <td class="text-danger">{{ email.error_message }}</td>
              </tr>
              <tr>
                <td class="meta-label">Attempts</td>
                <td>{{ email.attempts }} of {{ email.max_attempts }}</td>
              </tr>
              <tr v-if="email.next_attempt_at">
                <td class="meta-label">Next Attempt At</td>
                <td>{{ formatDate(email.next_attempt_at) }}</td>
              </tr>
              <tr>
                <td class="meta-label">Template</td>
                <td>{{ email.template_name || '-' }}</td>
              </tr>
              <tr>
                <td class="meta-label">Created At</td>
                <td>{{ formatDate(email.created_at) }}</td>
              </tr>
              <tr v-if="email.scheduled_at">
                <td class="meta-label">Scheduled At</td>
                <td>{{ formatDate(email.scheduled_at) }}</td>
              </tr>
              <tr>
                <td class="meta-label">Sent At</td>
                <td>{{ formatDate(email.sent_at) }}</td>
              </tr>
              <tr v-if="email.list_unsubscribe_url || email.list_unsubscribe_mailto">
                <td class="meta-label">List-Unsubscribe</td>
                <td>
                  <div class="text-xs mb-1" v-if="email.list_unsubscribe_mailto">
                    <code>{{ email.list_unsubscribe_mailto }}</code>
                  </div>
                  <div class="text-xs" v-if="email.list_unsubscribe_url">
                    <code>{{ email.list_unsubscribe_url }}</code>
                    <span v-if="email.list_unsubscribe_post" class="badge badge-info ml-2"
                      >One-Click</span
                    >
                  </div>
                </td>
              </tr>
              <!-- Opens and clicks. Recorded since tracking existed and
                   shown nowhere until now, so a project with tracking on
                   had a pixel in every message and no page that admitted
                   it had ever fired.

                   `tracked` is what separates "nobody opened it" from
                   "we never asked" - the email row keeps the flag for
                   exactly this, and showing a bare 0 for an untracked
                   message reads as the first when it means the second. -->
              <tr>
                <td class="meta-label">Opens</td>
                <td>
                  <template v-if="!email.tracked">
                    <span class="text-muted">not tracked</span>
                  </template>
                  <template v-else-if="email.opened_at">
                    {{ formatDate(email.opened_at) }}
                    <span v-if="(email.open_count ?? 0) > 1" class="text-muted text-sm">
                      &middot; {{ email.open_count }} times
                    </span>
                  </template>
                  <span v-else class="text-muted">not opened yet</span>
                </td>
              </tr>
              <tr>
                <td class="meta-label">Clicks</td>
                <td>
                  <template v-if="!email.tracked">
                    <span class="text-muted">not tracked</span>
                  </template>
                  <template v-else-if="email.clicked_at">
                    {{ formatDate(email.clicked_at) }}
                    <span v-if="(email.click_count ?? 0) > 1" class="text-muted text-sm">
                      &middot; {{ email.click_count }} times
                    </span>
                  </template>
                  <span v-else class="text-muted">no clicks</span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <!-- The same message-view form the sandbox reader uses - tabs,
           the device-width switcher, headers and attachments in one
           place. Custom headers and the attachments table used to be
           a row and a card of their own above, which is why they are
           absent from the meta table now. No Raw tab: the log stores
           the parts, not the bytes. -->
      <div class="card">
        <MessageViewer
          :key="email.id"
          :html="email.html_body"
          :text="email.text_body"
          :title="email.subject"
          :headers="email.headers"
          :attachments="viewerAttachments"
          :tracked-links="trackedLinks"
          min-height="480px"
        />
      </div>
    </template>

    <EmptyState
      v-else
      title="Email not found"
      text="The email you are looking for does not exist."
    />
  </div>
</template>

<style scoped>
.meta-label {
  font-weight: 600;
  width: 160px;
  color: var(--text-primary);
}
</style>
