<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  emailsApi,
  type SendEmailPayload,
  type SendLimits,
  type SendTemplatePayload,
} from '../../api/emails'
import { templatesApi } from '../../api/templates'
import { languagesApi } from '../../api/languages'
import { type Sender, sendersApi } from '../../api/senders'
import { smtpGroupApi } from '../../api/smtpGroups'
import { type UnsubscribeList, unsubscribeListsApi } from '../../api/unsubscribeLists'
import { apiErrorMessage } from '../../api/client'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import SenderSelect from '../../components/SenderSelect.vue'
import type { EmailAttachment, Language, SMTPServerGroup, Template } from '../../api/types'
import PageHeader from '../../components/PageHeader.vue'
import FormField from '../../components/FormField.vue'
import AttachmentPicker, { type PendingAttachment } from './AttachmentPicker.vue'
import HeaderEditor from '../../components/HeaderEditor.vue'
import { type HeaderRow, headerRowsProblem, rowsToHeaders } from '../../composables/headerRows'
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
const ccText = ref('')
const bccText = ref('')

// Cc and Bcc are folded away until asked for. Most sends have neither,
// and two empty textareas push the body off the first screen. Once
// either holds a value it stays open - a field hiding text the form is
// about to send is worse than a long form.
const showCopies = ref(false)
const copiesOpen = computed(() => showCopies.value || ccText.value !== '' || bccText.value !== '')
const sendAt = ref('')
// Which SMTP pool to send through. Empty means the project's default
// group, which is what every send did before groups existed. Mostly
// useful here for trying a specific pool by hand before pointing an
// integration at it.
const smtpGroup = ref('')
const smtpGroups = ref<SMTPServerGroup[]>([])

// A Mailyard-managed unsubscribe list: pick one and the send mints the
// one-click link, writes the List-Unsubscribe headers and skips whoever
// opted out of it. The caller's own List-Unsubscribe targets exist on
// the API and not here - three more fields on this form read as noise,
// and an application with its own opt-out endpoint is not sending from
// a browser.
const unsubscribeLists = ref<UnsubscribeList[]>([])
const unsubscribeListId = ref('')
// Custom headers. Always on the page, like the attachment picker: an
// empty editor is one button, and folded behind a text link in the To
// hint it was not found. A reply arrives with its two threading headers
// already in the rows, so they are visible and can be dropped rather
// than riding along unseen.
const headerRows = ref<HeaderRow[]>([])

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

// The registered sender the From address names, when it signs its
// mail. The checkbox below the picker exists only then, and only to
// decline: whether an address signs is decided where its key is.
const signingSender = computed(() => {
  const addr = from.value.trim().toLowerCase()
  const s = senders.value.find((x) => x.email === addr)

  return s?.signing?.sign ? s : null
})
const signThis = ref(true)

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
    headerRows.value = [
      { name: 'In-Reply-To', value: id },
      { name: 'References', value: id },
    ]
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
  unsubscribeListsApi
    .list()
    .then((res) => {
      unsubscribeLists.value = (res.data.unsubscribe_lists ?? []).filter((l) => l.active)
    })
    .catch(() => {
      unsubscribeLists.value = []
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
function parseAddresses(text: string): string[] {
  return text
    .split(/[\n,]+/)
    .map((s) => s.trim())
    .filter(Boolean)
}

function unsubscribeFields(payload: SendEmailPayload | SendTemplatePayload) {
  if (unsubscribeListId.value) payload.unsubscribe_list_id = unsubscribeListId.value
}

// The Cc and Bcc lists, set on the payload only when filled in, so a
// send without them is the request it always was.
function copyRecipients(payload: { cc?: string[]; bcc?: string[] }) {
  const cc = parseAddresses(ccText.value)
  const bcc = parseAddresses(bccText.value)
  if (cc.length) payload.cc = cc
  if (bcc.length) payload.bcc = bcc
}

function baseValidation(): string[] | null {
  if (!from.value.trim()) {
    notify.error('Sender address is required')
    return null
  }
  const to = parseAddresses(recipientsText.value)
  if (to.length === 0) {
    notify.error('At least one recipient is required')
    return null
  }
  // The one-click link identifies a person, so the server refuses a
  // scoped send to several. Said here, before the request.
  if (
    unsubscribeListId.value &&
    to.length + parseAddresses(ccText.value).length + parseAddresses(bccText.value).length > 1
  ) {
    notify.error('A send scoped to an unsubscribe list goes to one recipient')
    return null
  }
  // The editor shows the same sentence under itself. Said again as a
  // toast because the editor may be scrolled off the screen by now.
  const problem = headerRowsProblem(headerRows.value)
  if (problem) {
    notify.error(`Custom headers: ${problem}`)
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
  copyRecipients(payload)
  if (replyTo.value.trim()) payload.reply_to = replyTo.value.trim()
  if (html.value) payload.html = html.value
  if (text.value) payload.text = text.value
  const files = attachmentPayload()
  if (files) payload.attachments = files
  if (sendAt.value) payload.send_at = new Date(sendAt.value).toISOString()
  if (smtpGroup.value) payload.smtp_group = smtpGroup.value
  payload.headers = rowsToHeaders(headerRows.value)
  unsubscribeFields(payload)
  if (signingSender.value && !signThis.value) payload.disable_signing = true
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
  copyRecipients(payload)
  if (replyTo.value.trim()) payload.reply_to = replyTo.value.trim()
  if (language.value) payload.language = language.value
  if (data) payload.data = data
  // Template attachments configured on the template itself are added
  // server-side, so these are appended to that set, not a substitute.
  const files = attachmentPayload()
  if (files) payload.attachments = files
  if (sendAt.value) payload.send_at = new Date(sendAt.value).toISOString()
  if (smtpGroup.value) payload.smtp_group = smtpGroup.value
  payload.headers = rowsToHeaders(headerRows.value)
  unsubscribeFields(payload)
  if (signingSender.value && !signThis.value) payload.disable_signing = true
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
            v-if="signingSender"
            hint="The address carries a signing key. Untick to send this one message unsigned."
          >
            <label class="checkbox-label">
              <input v-model="signThis" type="checkbox" />
              <span>
                Sign this message ({{ signingSender.signing?.kind === 'pgp' ? 'PGP' : 'S/MIME' }})
              </span>
            </label>
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

          <FormField label="To" for="send-to">
            <textarea
              id="send-to"
              v-model="recipientsText"
              class="form-textarea"
              rows="3"
              placeholder="one address per line or comma separated"
            ></textarea>
            <template #hint>
              Shown to every recipient.
              <button
                v-if="!copiesOpen"
                type="button"
                class="form-reveal"
                @click="showCopies = true"
              >
                Add Cc / Bcc
              </button>
            </template>
          </FormField>

          <FormField v-if="copiesOpen" label="Cc" for="send-cc" hint="Shown to every recipient.">
            <textarea
              id="send-cc"
              v-model="ccText"
              class="form-textarea"
              rows="2"
              placeholder="one address per line or comma separated"
            ></textarea>
          </FormField>

          <FormField
            v-if="copiesOpen"
            label="Bcc"
            for="send-bcc"
            hint="Delivered to these addresses, shown to nobody."
          >
            <textarea
              id="send-bcc"
              v-model="bccText"
              class="form-textarea"
              rows="2"
              placeholder="one address per line or comma separated"
            ></textarea>
          </FormField>

          <FormField
            label="Custom headers"
            :error="fieldErrors.headers"
            hint="Written into the message as given. Up to 20. From, To, Subject, Date, Message-ID and the other headers Mailyard writes itself cannot be set here, and the project's default headers are added underneath - a header named here wins."
          >
            <HeaderEditor v-model="headerRows" :disabled="!projStore.can('emails:write')" />
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
            label="Unsubscribe list (optional)"
            for="send-unsub-list"
            :error="fieldErrors.unsubscribe_list_id"
            :hint="
              unsubscribeLists.length > 0
                ? 'Mailyard mints a one-click opt-out link scoped to this list, adds the List-Unsubscribe headers and skips recipients who opted out of it. One recipient only.'
                : 'No active unsubscribe lists in this project yet.'
            "
          >
            <select id="send-unsub-list" v-model="unsubscribeListId" class="form-select">
              <option value="">None</option>
              <option v-for="l in unsubscribeLists" :key="l.id" :value="l.id">{{ l.name }}</option>
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
