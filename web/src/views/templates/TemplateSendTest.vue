<script setup lang="ts">
// Send the ACTIVE version to a real mailbox.
//
// A real send, through the ordinary path - so it counts against the
// project's quota and lands in the email log like any other message.
// That is the point: a preview cannot tell you what a client does with
// the markup, and this can.
import { computed, ref } from 'vue'
import { templatesApi } from '../../api/templates'
import { sendersApi, type Sender } from '../../api/senders'
import { apiErrorMessage } from '../../api/client'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import { useFieldErrors } from '../../composables/fieldErrors'
import SenderSelect from '../../components/SenderSelect.vue'
import BaseModal from '../../components/BaseModal.vue'
import FormField from '../../components/FormField.vue'

const props = defineProps<{
  templateId: string
  /** Languages this template actually has content for. */
  languages: string[]
  defaultLanguage: string
  /** Pre-filled into the data box - the version's own sample data. */
  sampleData?: string
}>()

const emit = defineEmits<{ (e: 'close'): void }>()

const notify = useNotificationStore()
const projects = useProjectStore()
const { errors, capture, clear } = useFieldErrors()

const FALLBACK_DATA = '{\n  "name": "John",\n  "company": "Acme"\n}'

const to = ref('')
const from = ref('')
const language = ref('')
const data = ref(props.sampleData || FALLBACK_DATA)
const sending = ref(false)

/**
 * The project's approved From addresses.
 *
 * Senders is a resource of its own and this dialog belongs to another,
 * so a role that may draft mail without seeing the approved addresses
 * is ordinary - asking anyway is a 403 in the console for an endpoint
 * this person is not meant to know exists. Without them the field stays
 * free text, which is what the server accepts anyway.
 */
const senders = ref<Sender[]>([])

async function loadSenders() {
  if (!projects.can('senders:read')) return

  try {
    senders.value = (await sendersApi.list()).data.senders ?? []
  } catch {
    senders.value = []
  }
}

/** Comma separated, because a test usually goes to more than one client. */
const recipients = computed(() =>
  to.value
    .split(',')
    .map((a) => a.trim())
    .filter(Boolean),
)

const ready = computed(() => recipients.value.length > 0 && from.value.trim().length > 0)

async function send() {
  clear()
  if (!ready.value) return

  let values: Record<string, unknown>
  try {
    values = JSON.parse(data.value || '{}')
  } catch {
    notify.error('The sample data is not valid JSON')

    return
  }

  sending.value = true
  try {
    await templatesApi.sendTest(props.templateId, {
      from: from.value.trim(),
      to: recipients.value,
      language: language.value || undefined,
      data: values,
    })
    notify.success(recipients.value.length === 1 ? 'Test sent' : 'Tests sent')
    emit('close')
  } catch (e) {
    if (!capture(e)) notify.error(apiErrorMessage(e, 'Failed to send the test'))
  } finally {
    sending.value = false
  }
}

void loadSenders()
</script>

<template>
  <BaseModal title="Send a test" size="modal-w560" @close="$emit('close')">
    <FormField label="To" :error="errors.to" hint="Comma separated, up to five addresses.">
      <input v-model="to" class="form-input" placeholder="you@example.com" />
    </FormField>

    <FormField label="From" :error="errors.from">
      <SenderSelect v-model="from" :senders="senders" placeholder="noreply@example.com" />
    </FormField>

    <FormField label="Language" :error="errors.language">
      <select v-model="language" class="form-select">
        <option value="">Template default ({{ defaultLanguage }})</option>
        <option v-for="code in languages" :key="code" :value="code">{{ code }}</option>
      </select>
    </FormField>

    <FormField label="Sample data (JSON)">
      <textarea
        v-model="data"
        class="form-textarea code-font"
        rows="4"
        placeholder='{"name": "John"}'
      ></textarea>
    </FormField>

    <template #footer>
      <button class="btn btn-secondary" @click="$emit('close')">Cancel</button>
      <button class="btn btn-primary" :disabled="sending || !ready" @click="send">
        {{ sending ? 'Sending...' : 'Send' }}
      </button>
    </template>
  </BaseModal>
</template>
