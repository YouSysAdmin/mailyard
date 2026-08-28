<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  emailsApi,
  type SendEmailPayload,
  type SendTemplatePayload,
  type SendLimits,
} from '../../api/emails'
import { templatesApi } from '../../api/templates'
import { languagesApi } from '../../api/languages'
import { sendersApi, type Sender } from '../../api/senders'
import { smtpGroupApi } from '../../api/smtpGroups'
import { apiErrorMessage } from '../../api/client'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import SenderSelect from '../../components/SenderSelect.vue'
import type { Template, Language, EmailAttachment, SMTPServerGroup } from '../../api/types'
import PageHeader from '../../components/PageHeader.vue'
import FormField from '../../components/FormField.vue'
import AttachmentPicker, { type PendingAttachment } from './AttachmentPicker.vue'
import { useFieldErrors } from '../../composables/fieldErrors'
import Notice from '../../components/Notice.vue'

const router = useRouter()
const route = useRoute()
const notify = useNotificationStore()
const projStore = useProjectStore()

const mode = ref<'raw' | 'template'>('raw')
const sending = ref(false)

// Shared fields
const from = ref('')
const replyTo = ref('')
const recipientsText = ref('')
const sendAt = ref('')
// Which SMTP pool to send through. Empty means the project's default
// group, which is what every send did before groups existed. Mostly
// useful here for trying a specific pool by hand before pointing an
// integration at it.
const smtpGroup = ref('')
const smtpGroups = ref<SMTPServerGroup[]>([])
// Threading headers for a reply, set from the query and attached to
// whatever is sent. Not an editable field: there is no custom-header
// UI here, and inventing one to carry two values nobody types by hand
// would be the wrong shape.
const replyHeaders = ref<Record<string, string> | null>(null)

// Raw mode
const subject = ref('')
const html = ref('')
const text = ref('')

// Template mode
const templates = ref<Template[]>([])
const languages = ref<Language[]>([])
const templateId = ref('')
const language = ref('')
const dataText = ref('')

// Approved sender addresses for the From selector. Errors are
// tolerated as an empty list, which keeps the free-text input.
const senders = ref<Sender[]>([])

// Attachments. These ride along in the JSON body as base64 and are
// stored with the email row - there is no staging upload, so nothing
// is left behind if the form is abandoned.

const attachments = ref<PendingAttachment[]>([])

// Server-reported caps, so the form refuses a file the send would
// reject anyway. Defaults are conservative and only apply if the
// request fails - the real values arrive on mount.
const limits = ref<SendLimits>({
  max_recipients: 50,
  max_attachments: 10,
  max_attachment_size: 10 * 1024 * 1024,
  max_total_attachment_size: 25 * 1024 * 1024,
})

const { errors: fieldErrors, capture, clear } = useFieldErrors()

// Strip the local-only size field before the payload goes out.
function attachmentPayload(): EmailAttachment[] | undefined {
  if (attachments.value.length === 0) return undefined
  return attachments.value.map((a) => ({
    filename: a.filename,
    content: a.content,
    content_type: a.content_type,
  }))
}

// Prefill from the query string.
//
// The compose form is reached from a contact, a subscriber, or a
// reply to received mail, and each of those already knows the
// recipient. Handing it over in the URL reuses this whole page -
// templates, attachments, server groups, scheduling - instead of
// growing a second, poorer compose form in a modal.
//
// in_reply_to becomes the threading headers. Without them a reply
// arrives as a new conversation, which for somebody who wrote to a
// no-reply address is exactly the confusion being fixed.
function prefillFromQuery() {
  const one = (v: unknown): string => (typeof v === 'string' ? v : '')

  const to = one(route.query.to)
  if (to) recipientsText.value = to

  // Replying from the address the mail was sent TO is the whole point
  // when that address was a no-reply nobody watches. Prefilled, not
  // forced: the server still decides whether this project may send as
  // it, and a rejection there says so plainly.
  const sender = one(route.query.from)
  if (sender) from.value = sender

  const subj = one(route.query.subject)
  if (subj) {
    subject.value = subj
    // A prefilled subject means raw mode - a template would overwrite
    // it with its own.
    mode.value = 'raw'
  }

  // The quoted original goes below two blank lines, so the cursor
  // starts on an empty first line and the reply is written above the
  // quote - which is what every mail client does and what the person
  // receiving it expects to read first.
  const quote = one(route.query.quote)
  if (quote) {
    text.value = `\n\n${quote}`
    mode.value = 'raw'
  }

  const replyTo = one(route.query.in_reply_to)
  if (replyTo) {
    // References is what most clients actually thread on, and a
    // single-message thread makes it identical to In-Reply-To.
    const id = replyTo.startsWith('<') ? replyTo : `<${replyTo}>`
    replyHeaders.value = { 'In-Reply-To': id, References: id }
  }
}

onMounted(async () => {
  prefillFromQuery()
  emailsApi
    .limits()
    .then((res) => {
      limits.value = res.data.limits
    })
    .catch(() => {
      // Keep the conservative defaults. A stricter client-side cap
      // than the server's only costs a rejected file the server
      // would have taken.
    })
  smtpGroupApi
    .list()
    .then((res) => {
      smtpGroups.value = res.data.smtp_server_groups ?? []
    })
    .catch(() => {
      // No selector rather than an error. Leaving it empty sends
      // through the default group, which is the right answer anyway.
      //
      // Indistinguishable from having one group, which is fine: both
      // mean there is no choice to offer here.
    })
  sendersApi
    .list()
    .then((res) => {
      senders.value = res.data.senders ?? []
    })
    .catch(() => {
      senders.value = []
    })
  try {
    const [tplRes, langRes] = await Promise.all([templatesApi.list(), languagesApi.list()])
    templates.value = tplRes.data.templates ?? []
    languages.value = langRes.data.languages ?? []
  } catch (e) {
    // Said, unlike the two above it. Those two failing means one fewer
    // choice on a form that works without it, where this one means the
    // template picker is empty - and an empty picker reads as "this
    // project has no templates", which is a different answer from "the
    // list did not load".
    notify.error(apiErrorMessage(e, 'Failed to load the templates'))
  }
})

// One address per line or comma separated.
function parseRecipients(): string[] {
  return recipientsText.value
    .split(/[\n,]+/)
    .map((s) => s.trim())
    .filter(Boolean)
}

function baseValidation(): string[] | null {
  if (!from.value.trim()) {
    notify.error('Sender address is required')
    return null
  }
  const to = parseRecipients()
  if (to.length === 0) {
    notify.error('At least one recipient is required')
    return null
  }
  return to
}

async function sendRaw() {
  const to = baseValidation()
  if (!to) return
  if (!subject.value.trim()) {
    notify.error('Subject is required')
    return
  }
  if (!html.value && !text.value) {
    notify.error('Provide an HTML or text body')
    return
  }
  const payload: SendEmailPayload = {
    from: from.value.trim(),
    to,
    subject: subject.value,
  }
  if (replyTo.value.trim()) payload.reply_to = replyTo.value.trim()
  if (html.value) payload.html = html.value
  if (text.value) payload.text = text.value
  const files = attachmentPayload()
  if (files) payload.attachments = files
  if (sendAt.value) payload.send_at = new Date(sendAt.value).toISOString()
  if (smtpGroup.value) payload.smtp_group = smtpGroup.value
  if (replyHeaders.value) payload.headers = replyHeaders.value
  await submit(() => emailsApi.send(payload))
}

async function sendTemplate() {
  const to = baseValidation()
  if (!to) return
  if (!templateId.value) {
    notify.error('Select a template')
    return
  }
  let data: Record<string, unknown> | undefined
  if (dataText.value.trim()) {
    try {
      const parsed = JSON.parse(dataText.value)
      if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
        notify.error('Template data must be a JSON object')
        return
      }
      data = parsed as Record<string, unknown>
    } catch {
      notify.error('Template data is not valid JSON')
      return
    }
  }
  const payload: SendTemplatePayload = {
    from: from.value.trim(),
    to,
    template_id: templateId.value,
  }
  if (replyTo.value.trim()) payload.reply_to = replyTo.value.trim()
  if (language.value) payload.language = language.value
  if (data) payload.data = data
  // Template attachments configured on the template itself are added
  // server-side, so these are appended to that set, not a substitute.
  const files = attachmentPayload()
  if (files) payload.attachments = files
  if (sendAt.value) payload.send_at = new Date(sendAt.value).toISOString()
  if (smtpGroup.value) payload.smtp_group = smtpGroup.value
  if (replyHeaders.value) payload.headers = replyHeaders.value
  await submit(() => emailsApi.sendTemplate(payload))
}

type SendResponse = { data: { email: { id: string }; suppressed_recipients: string[] } }

async function submit(fn: () => Promise<SendResponse>) {
  if (sending.value) return
  clear()
  sending.value = true
  try {
    const res = await fn()
    const blocked = res.data.suppressed_recipients ?? []
    if (blocked.length > 0) {
      notify.info(`Suppressed recipients skipped: ${blocked.join(', ')}`)
    }
    notify.success('Email accepted for delivery')
    router.push(`/emails/${res.data.email.id}`)
  } catch (e) {
    if (!capture(e)) notify.error(apiErrorMessage(e, 'Failed to send email'))
  } finally {
    sending.value = false
  }
}

function handleSubmit() {
  if (mode.value === 'raw') sendRaw()
  else sendTemplate()
}
</script>

<template>
  <div>
    <PageHeader title="Send Email">
      <button class="btn btn-secondary" @click="router.push('/emails')">Back to Emails</button>
    </PageHeader>

    <Notice v-if="!projStore.can('emails:write')" kind="warning" class="mb-5">
      <p>You have read-only access in this project and cannot send emails.</p>
    </Notice>

    <div class="card send-card">
      <div class="card-body">
        <div class="tabs">
          <button class="tab" :class="{ active: mode === 'raw' }" @click="mode = 'raw'">Raw</button>
          <button class="tab" :class="{ active: mode === 'template' }" @click="mode = 'template'">
            Template
          </button>
        </div>

        <form @submit.prevent="handleSubmit">
          <FormField label="From" for="send-from" :error="fieldErrors.from">
            <SenderSelect id="send-from" v-model="from" :senders="senders" />
          </FormField>

          <FormField
            label="Reply-To"
            for="send-reply-to"
            :error="fieldErrors.reply_to"
            hint="Where a reply lands when it should not go back to the From address."
          >
            <input
              id="send-reply-to"
              v-model="replyTo"
              type="email"
              class="form-input"
              placeholder="support@example.com"
            />
          </FormField>

          <FormField label="Recipients" for="send-to">
            <textarea
              id="send-to"
              v-model="recipientsText"
              class="form-textarea"
              rows="3"
              placeholder="one address per line or comma separated"
            ></textarea>
          </FormField>

          <template v-if="mode === 'raw'">
            <FormField label="Subject" for="send-subject" :error="fieldErrors.subject">
              <input id="send-subject" v-model="subject" type="text" class="form-input" />
            </FormField>
            <FormField label="Text Body" for="send-text" :error="fieldErrors.text">
              <textarea id="send-text" v-model="text" class="form-textarea" rows="6"></textarea>
            </FormField>
            <FormField
              label="HTML Body"
              for="send-html"
              :error="fieldErrors.html"
              hint="At least one of HTML or text body is required."
            >
              <textarea
                id="send-html"
                v-model="html"
                class="form-textarea"
                rows="10"
                placeholder="<html>...</html>"
              ></textarea>
            </FormField>
          </template>

          <template v-else>
            <FormField label="Template" for="send-template" :error="fieldErrors.template_id">
              <select id="send-template" v-model="templateId" class="form-select">
                <option value="" disabled>Select a template</option>
                <option v-for="t in templates" :key="t.id" :value="t.id">{{ t.name }}</option>
              </select>
            </FormField>
            <FormField label="Language" for="send-language" :error="fieldErrors.language">
              <select id="send-language" v-model="language" class="form-select">
                <option value="">Template default</option>
                <option v-for="l in languages" :key="l.id" :value="l.code">
                  {{ l.name }} ({{ l.code }})
                </option>
              </select>
            </FormField>
            <FormField
              label="Data (JSON)"
              for="send-data"
              hint="Values the template placeholders render against."
            >
              <textarea
                id="send-data"
                v-model="dataText"
                class="form-textarea"
                rows="8"
                placeholder='{"name": "Ada"}'
              ></textarea>
            </FormField>
          </template>

          <AttachmentPicker
            v-model="attachments"
            :limits="limits"
            :can-send="projStore.can('emails:write')"
          />

          <!-- Only when there is a choice to make. With one group,
               "Default group" and that group are the same thing. -->
          <FormField
            v-if="smtpGroups.length > 1"
            label="Server Group (optional)"
            for="smtp-group"
            :error="fieldErrors.smtp_group"
            hint="Which SMTP pool this send goes through."
          >
            <select id="smtp-group" v-model="smtpGroup" class="form-select">
              <option value="">Default group</option>
              <option v-for="g in smtpGroups" :key="g.id" :value="g.slug">
                {{ g.name }}{{ g.is_default ? ' (default)' : '' }}
              </option>
            </select>
          </FormField>

          <FormField
            label="Send At (optional)"
            for="send-at"
            :error="fieldErrors.send_at"
            hint="Leave empty to send immediately."
          >
            <input id="send-at" v-model="sendAt" type="datetime-local" class="form-input" />
          </FormField>

          <button
            type="submit"
            class="btn btn-primary"
            :disabled="sending || !projStore.can('emails:write')"
          >
            {{ sending ? 'Sending...' : sendAt ? 'Schedule email' : 'Send email' }}
          </button>
        </form>
      </div>
    </div>
  </div>
</template>

<style scoped>
.send-card {
  max-width: 760px;
}
</style>
