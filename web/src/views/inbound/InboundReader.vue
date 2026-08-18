<script setup lang="ts">
// One received message.
//
// The SHELL is MessageReader, shared with the sandbox: same act, reading
// down a list of mail that arrived, and the two pages sat in one nav
// section looking like different products.
//
// What is this one's own is the AUTHENTICATION verdict. A captured
// message came from a credential we issued, so who sent it is never in
// question. This one arrived from the internet, and whether the From
// domain vouched for it is the first thing worth knowing.
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { inboundApi, type InboundEmail } from '../../api/inbound'
import { apiErrorMessage, browserURL } from '../../api/client'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import { formatDate } from '../../composables/formatDate'
import { humanSize } from '../../composables/humanSize'
import { quoteForReply } from './reply'
import LoadingBlock from '../../components/LoadingBlock.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import MessageReader from '../../components/MessageReader.vue'
import MessageViewer, { type ViewerAttachment } from '../../components/MessageViewer.vue'

const props = defineProps<{ id: string }>()

const emit = defineEmits<{ (e: 'delete', email: InboundEmail): void }>()

const router = useRouter()
const notify = useNotificationStore()
const projStore = useProjectStore()

const loading = ref(true)
const email = ref<InboundEmail | null>(null)
const retrying = ref(false)

// Green ONLY when the From domain actually vouched for the message. A
// valid signature from some other domain is not reassurance, so
// alignment is the only thing this reflects.
const authClass = computed(() =>
  email.value?.auth?.aligned ? 'badge badge-success' : 'badge badge-warning',
)

const authTitle = computed(() =>
  email.value?.auth?.aligned
    ? 'The From domain vouched for this message (aligned SPF or DKIM pass)'
    : 'Nothing the From domain vouches for passed, so the sender address may be forged',
)

// The download URLs only this caller can build - see ViewerAttachment.
const viewerAttachments = computed<ViewerAttachment[]>(() => {
  const e = email.value
  if (!e) return []

  return (e.attachments ?? []).map((a, i) => ({
    ...a,
    url: browserURL(`/inbound-emails/${e.id}/attachments/${i}`),
  }))
})

async function load() {
  loading.value = true
  try {
    email.value = (await inboundApi.get(props.id)).data.inbound_email
  } catch (e) {
    email.value = null
    notify.error(apiErrorMessage(e, 'Failed to load the message'))
  } finally {
    loading.value = false
  }
}

// Asks the PAGE to delete, rather than deleting itself. The page owns
// the list, the selection and the wording of the question - and having
// both write that question is how one act comes to be described two
// ways.
function remove() {
  if (email.value) emit('delete', email.value)
}

async function retryWebhook() {
  if (!email.value || retrying.value) return

  retrying.value = true
  try {
    await inboundApi.retryWebhook(email.value.id)
    notify.success('inbound.received webhook re-sent')
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to re-send the webhook'))
  } finally {
    retrying.value = false
  }
}

/**
 * Opens the compose form addressed back to the sender.
 *
 * Worth having because of exactly the case that makes people write to a
 * no-reply address: they had something to say and the address they had
 * was one nobody reads. Mailyard receives it, so somebody can answer.
 */
function reply() {
  const src = email.value
  if (!src) return

  const subject = /^re:/i.test(src.subject ?? '')
    ? (src.subject as string)
    : `Re: ${src.subject || '(no subject)'}`

  const query: Record<string, string> = { to: src.sender, subject }
  // The envelope may name several recipients. The first is the one this
  // project was addressed at, and guessing further would be inventing an
  // identity to send as.
  if (src.recipients?.length) query.from = src.recipients[0]
  if (src.message_id) query.in_reply_to = src.message_id

  const quoted = quoteForReply(src)
  if (quoted) query.quote = quoted

  router.push({ name: 'email-send', query })
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
      <button v-if="projStore.can('inbound:write')" class="btn btn-primary btn-sm" @click="reply">
        Reply
      </button>
      <button
        v-if="projStore.can('inbound:write')"
        class="btn btn-secondary btn-sm"
        :disabled="retrying"
        title="Send the inbound.received webhook for this message again"
        @click="retryWebhook"
      >
        {{ retrying ? 'Sending...' : 'Re-send webhook' }}
      </button>
      <button v-if="projStore.can('inbound:delete')" class="btn btn-danger btn-sm" @click="remove">
        Delete
      </button>
    </template>

    <!-- The envelope sender is whatever the connecting host typed, so it
         is only worth anything beside the verdict on whether the domain
         actually vouched for it. -->
    <template #sender>
      <span v-if="email.auth" :class="authClass" :title="authTitle">
        {{ email.auth.aligned ? 'Authenticated' : 'Unauthenticated' }}
      </span>
      <span v-else class="badge badge-neutral" title="This message predates sender authentication">
        Not checked
      </span>
    </template>

    <template #envelope>
      <div v-if="email.auth">
        <dt>Authentication</dt>
        <dd class="auth-detail">
          <code>spf={{ email.auth.spf }}</code>
          <code>dkim={{ email.auth.dkim }}</code>
          <code>dmarc={{ email.auth.dmarc }}</code>
          <code v-if="email.auth.dmarc_policy">p={{ email.auth.dmarc_policy }}</code>
        </dd>
      </div>
      <div>
        <dt>Status</dt>
        <dd><StatusBadge :status="email.status" scope="inbound" /></dd>
      </div>
      <div v-if="email.message_id">
        <dt>Message ID</dt>
        <dd>
          <code>{{ email.message_id }}</code>
        </dd>
      </div>
      <!-- Absent on a message refused before a recipient was resolved,
           which is every rejection at RCPT. An empty <code> box there
           reads as a value that failed to load. -->
      <div v-if="email.domain_id">
        <dt>Domain</dt>
        <dd>
          <code>{{ email.domain_id }}</code>
        </dd>
      </div>
      <div>
        <dt>Size</dt>
        <dd>{{ humanSize(email.size) }}</dd>
      </div>
      <div>
        <dt>Received</dt>
        <dd>{{ formatDate(email.received_at) }}</dd>
      </div>
      <div v-if="email.error_message" class="details-note text-danger">
        {{ email.error_message }}
      </div>
      <!-- Raw bytes are kept only for messages that failed to parse,
           which is why this is not a permanent control. -->
      <div v-if="email.has_raw" class="details-note">
        <a
          class="btn btn-secondary btn-sm"
          :href="browserURL(`/inbound-emails/${email.id}/raw`)"
          target="_blank"
          rel="noopener"
        >
          Download the raw message
        </a>
      </div>
    </template>

    <MessageViewer
      :key="email.id"
      class="viewer"
      :html="email.html_body"
      :text="email.text_body"
      :title="email.subject || 'Received message'"
      :headers="email.headers"
      :attachments="viewerAttachments"
      fill
    />
  </MessageReader>
</template>

<style scoped>
.viewer {
  flex: 1 1 0;
}

/* Four short verdicts on one line, wrapping rather than stretching the
   column they sit in. */
.auth-detail {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.auth-detail code {
  font-size: 12px;
}
</style>
