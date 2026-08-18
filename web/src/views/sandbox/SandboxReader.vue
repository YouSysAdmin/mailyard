<script setup lang="ts">
// One captured message.
//
// A component and not a page, because the sandbox is a mail client -
// developers keep it open while a suite runs, and navigating away from
// the list to read one message and back again for the next is the thing
// that made it tiring.
//
// The SHELL is MessageReader, shared with the inbound log. What is this
// one's own is the envelope: a capture came from a credential we issued,
// so the question is not who sent it but what they sent it WITH, and
// how long it will be kept.
import { computed, ref, watch } from 'vue'
import { sandboxApi, type SandboxEmail } from '../../api/sandbox'
import { apiErrorMessage, browserURL } from '../../api/client'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import { formatDate } from '../../composables/formatDate'
import { humanSize } from '../../composables/humanSize'
import LoadingBlock from '../../components/LoadingBlock.vue'
import MessageReader from '../../components/MessageReader.vue'
import MessageViewer, { type ViewerAttachment } from '../../components/MessageViewer.vue'

const props = defineProps<{ id: string }>()
const emit = defineEmits<{ (e: 'deleted', id: string): void }>()

const notify = useNotificationStore()
const projStore = useProjectStore()

const loading = ref(true)
const email = ref<SandboxEmail | null>(null)
const raw = ref('')
const rawLoading = ref(false)

// The download URLs only this caller can build - see ViewerAttachment.
const viewerAttachments = computed<ViewerAttachment[]>(() => {
  const e = email.value
  if (!e) return []

  return (e.attachments ?? []).map((a, i) => ({ ...a, url: sandboxApi.attachmentUrl(e.id, i) }))
})

async function load() {
  loading.value = true
  raw.value = ''
  try {
    email.value = (await sandboxApi.get(props.id)).data.sandbox_email
  } catch (e) {
    email.value = null
    notify.error(apiErrorMessage(e, 'Failed to load the message'))
  } finally {
    loading.value = false
  }
}

// The viewer owns which tab is open and says so - raw is the one tab
// whose content is fetched separately, and only when somebody looks.
async function onTab(tab: string) {
  if (tab === 'raw') await loadRaw()
}

async function loadRaw() {
  if (raw.value || rawLoading.value) return

  rawLoading.value = true
  try {
    const res = await sandboxApi.raw(props.id)
    raw.value = typeof res.data === 'string' ? res.data : String(res.data)
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load the raw message'))
  } finally {
    rawLoading.value = false
  }
}

async function copyRaw() {
  await loadRaw()
  try {
    await navigator.clipboard.writeText(raw.value)
    notify.success('Raw message copied')
  } catch {
    notify.error('Could not copy to the clipboard')
  }
}

async function remove() {
  try {
    await sandboxApi.remove(props.id)
    // The list owns the selection, so it decides what to show next.
    emit('deleted', props.id)
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to delete the message'))
  }
}

// Reloads when the selection changes, which in this layout is a click in
// the list rather than a navigation.
watch(() => props.id, load, { immediate: true })
</script>

<template>
  <LoadingBlock v-if="loading" />

  <MessageReader
    v-else-if="email"
    :subject="email.subject"
    :sender="email.sender"
    :recipients="email.recipients"
    :message-id="email.id"
  >
    <template #actions>
      <button v-if="projStore.can('sandbox:delete')" class="btn btn-danger btn-sm" @click="remove">
        Delete
      </button>
    </template>

    <template #envelope>
      <div>
        <dt>Submitted via</dt>
        <dd>{{ email.source === 'api' ? 'HTTP API' : 'SMTP submission' }}</dd>
      </div>
      <div>
        <dt>Credential</dt>
        <dd>
          <code>{{ email.credential_id || email.api_key_id || '-' }}</code>
        </dd>
      </div>
      <div>
        <dt>Client address</dt>
        <dd>
          <code>{{ email.client_ip || '-' }}</code>
        </dd>
      </div>
      <div>
        <dt>Size</dt>
        <dd>{{ humanSize(email.size) }}</dd>
      </div>
      <div>
        <dt>Captured</dt>
        <dd>{{ formatDate(email.received_at) }}</dd>
      </div>
      <div>
        <dt>Expires</dt>
        <dd>{{ email.expires_at ? formatDate(email.expires_at) : 'kept until the cap' }}</dd>
      </div>
      <div class="details-note">
        The envelope is what a receiving server would have routed on. It differs from the From and
        To headers whenever a Bcc or a separate return path is in play, which is usually the thing
        worth checking here.
      </div>
    </template>

    <MessageViewer
      :key="email.id"
      class="viewer"
      :html="email.html_body"
      :text="email.text_body"
      :title="email.subject || 'Captured message'"
      :headers="email.headers"
      :attachments="viewerAttachments"
      raw
      fill
      @tab="onTab"
    >
      <template #raw>
        <div class="raw-actions">
          <button class="btn btn-secondary btn-sm" @click="copyRaw">Copy</button>
          <a
            class="btn btn-secondary btn-sm"
            :href="browserURL(`/sandbox/${email.id}/raw`)"
            target="_blank"
            rel="noopener"
          >
            Open
          </a>
        </div>
        <LoadingBlock v-if="rawLoading" />
        <pre v-else class="reader-pre">{{ raw }}</pre>
      </template>
    </MessageViewer>
  </MessageReader>
</template>

<style scoped>
.viewer {
  flex: 1 1 0;
}

.reader-pre {
  margin: 0;
  font-family: var(--font-mono);
  font-size: 12px;
  line-height: 1.6;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.raw-actions {
  display: flex;
  gap: 8px;
  margin-bottom: 12px;
}
</style>
