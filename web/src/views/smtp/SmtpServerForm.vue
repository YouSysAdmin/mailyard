<script setup lang="ts">
// Adding or editing one SMTP server.
//
// A dialog of its own because it is the page: 150 lines of form against
// a 90-line table, and the form is where every rule about providers
// lives. What it asks for FOLLOWS the provider - there is no second list
// here saying which fields SES wants, only `provider.dial` and the
// options the server declares.
import { computed, ref, watch } from 'vue'
import { smtpApi, type SMTPServerPayload } from '../../api/smtp'
import { apiErrorMessage } from '../../api/client'
import type { MailProvider, SMTPServer, SMTPServerGroup } from '../../api/types'
import { useNotificationStore } from '../../stores/notification'
import { useFieldErrors } from '../../composables/fieldErrors'
import BaseModal from '../../components/BaseModal.vue'
import FormField from '../../components/FormField.vue'

const props = defineProps<{
  /** The row being edited, or null when creating one. */
  server: SMTPServer | null
  /** What this build can send through, and what each one asks for. */
  providers: MailProvider[]
  groups: SMTPServerGroup[]
}>()

const emit = defineEmits<{
  (e: 'saved'): void
  (e: 'close'): void
}>()

const notify = useNotificationStore()
const { errors, capture, clear } = useFieldErrors()

const saving = ref(false)
const allowedEmailsText = ref('')

const form = ref(blank())

function blank() {
  return {
    name: '',
    provider: 'smtp',
    providerConfig: {} as Record<string, string>,
    host: '',
    port: 587,
    username: '',
    password: '',
    encryption: 'starttls',
    skip_dkim: false,
    ses_topic_arn: '',
    group_id: '',
    priority: 0,
  }
}

watch(
  () => props.server,
  (s) => {
    clear()
    if (!s) {
      form.value = blank()
      allowedEmailsText.value = ''

      return
    }

    form.value = {
      name: s.name,
      provider: s.provider || 'smtp',
      providerConfig: { ...(s.provider_config ?? {}) },
      host: s.host,
      port: s.port,
      username: s.username ?? '',
      // Never prefilled: the server does not return it, and an empty
      // box on an edit means "keep what is stored".
      password: '',
      encryption: s.encryption,
      skip_dkim: s.skip_dkim ?? false,
      ses_topic_arn: s.ses_topic_arn ?? '',
      group_id: s.group_id ?? '',
      priority: s.priority ?? 0,
    }
    allowedEmailsText.value = (s.allowed_emails ?? []).join('\n')
  },
  { immediate: true },
)

// The provider the form is on, and what it asks for. Everything the form
// shows or requires follows from this.
const provider = computed(() => props.providers.find((p) => p.id === form.value.provider) ?? null)
const dials = computed(() => provider.value?.dial ?? true)

// Only the keys THIS provider declares, so a value left behind by
// switching providers in the form is not stored.
function collectOptions(): Record<string, string> {
  const out: Record<string, string> = {}
  for (const opt of provider.value?.options ?? []) {
    const v = (form.value.providerConfig[opt.key] ?? '').trim()
    if (v !== '') out[opt.key] = v
  }

  return out
}

function parseAllowedEmails(): string[] {
  return allowedEmailsText.value
    .split(/\r?\n/)
    .map((e) => e.trim())
    .filter((e) => e.length > 0)
}

async function save() {
  clear()
  saving.value = true

  const payload: SMTPServerPayload = {
    name: form.value.name.trim(),
    username: form.value.username.trim(),
    skip_dkim: form.value.skip_dkim,
    ses_topic_arn: form.value.ses_topic_arn.trim(),
    allowed_emails: parseAllowedEmails(),
    priority: Number(form.value.priority),
  }
  if (form.value.group_id) payload.group_id = form.value.group_id

  // Only what this provider actually uses. Sending a host for a row
  // reached through an API would store a value nothing reads and that a
  // later change could read as real.
  if (dials.value) {
    payload.host = form.value.host.trim()
    payload.port = form.value.port
    payload.encryption = form.value.encryption
  } else {
    payload.provider_config = collectOptions()
  }

  // provider is CREATE-ONLY - the server refuses it on PATCH.
  if (!props.server) payload.provider = form.value.provider
  // On update an omitted password keeps the current one.
  if (!props.server || form.value.password !== '') payload.password = form.value.password

  try {
    if (props.server) {
      await smtpApi.update(props.server.id, payload)
      notify.success('SMTP server updated')
    } else {
      await smtpApi.create(payload)
      notify.success('SMTP server created')
    }
    emit('saved')
  } catch (e) {
    if (!capture(e)) notify.error(apiErrorMessage(e, 'Failed to save SMTP server'))
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <BaseModal
    :title="server ? 'Edit SMTP Server' : 'Add SMTP Server'"
    form
    @submit="save"
    @close="emit('close')"
  >
    <FormField label="Name" :error="errors.name">
      <input
        v-model="form.name"
        type="text"
        class="form-input"
        placeholder="e.g. Primary relay"
        required
      />
    </FormField>
    <FormField
      label="Provider"
      :error="errors.provider"
      :hint="
        server
          ? 'Not changeable on an existing server. The credentials mean something different to each provider, so switching would leave the wrong ones in place. Delete and recreate instead.'
          : ''
      "
    >
      <select v-model="form.provider" class="form-select" :disabled="!!server">
        <option v-for="p in providers" :key="p.id" :value="p.id">{{ p.label }}</option>
      </select>
    </FormField>
    <template v-if="dials">
      <FormField label="Host" :error="errors.host">
        <input
          v-model="form.host"
          type="text"
          class="form-input"
          placeholder="smtp.example.com"
          required
        />
      </FormField>
      <FormField label="Port" :error="errors.port">
        <input
          v-model.number="form.port"
          type="number"
          class="form-input"
          min="1"
          max="65535"
          placeholder="587"
          required
        />
      </FormField>
    </template>
    <!-- Whatever this provider says it reads. Rendered from the
         descriptor, so a provider added later needs no edit here. -->
    <FormField v-for="opt in provider?.options ?? []" :key="opt.key" :hint="opt.hint">
      <template #label
        >{{ opt.label }} <span v-if="!opt.required" class="text-muted">(optional)</span></template
      >
      <input
        v-model="form.providerConfig[opt.key]"
        type="text"
        class="form-input"
        :required="opt.required"
      />
    </FormField>
    <FormField :error="errors.username" :hint="provider?.credential_hint">
      <template #label>Username <span class="text-muted">(optional)</span></template>
      <input v-model="form.username" type="text" class="form-input" autocomplete="off" />
    </FormField>
    <FormField :error="errors.password">
      <template #label>Password <span class="text-muted">(optional)</span></template>
      <input
        v-model="form.password"
        type="password"
        class="form-input"
        autocomplete="new-password"
        :placeholder="server ? 'Leave blank to keep current' : ''"
      />
    </FormField>
    <FormField
      label="Server group"
      :error="errors.group_id"
      hint="Which pool this server belongs to."
    >
      <select v-model="form.group_id" class="form-select">
        <option value="">Default group</option>
        <option v-for="g in groups" :key="g.id" :value="g.id">
          {{ g.name }}{{ g.is_default ? ' (default)' : '' }}
        </option>
      </select>
    </FormField>
    <FormField
      label="Priority"
      :error="errors.priority"
      hint="Order within the group, lowest first. Failover walks it in this order."
    >
      <input v-model.number="form.priority" type="number" class="form-input" min="0" />
    </FormField>
    <FormField v-if="dials" label="Encryption" :error="errors.encryption">
      <select v-model="form.encryption" class="form-select">
        <option value="none">None</option>
        <option value="starttls">STARTTLS (port 587)</option>
        <option value="ssl">SSL/TLS (port 465)</option>
      </select>
    </FormField>
    <!-- Only where it is a real choice. A provider that signs the
         message itself makes this a question with one correct
         answer, and the wrong answer has no visible symptom: a
         broken signature is ignored rather than punished, so the
         mail just stops being authenticated by us. The server
         decides it for those, and says so below. -->
    <FormField
      v-if="!provider?.re_signs"
      hint="For providers that rewrite headers and sign with their own key. Mailyard's signature would arrive broken through them, so it is omitted instead."
    >
      <label class="checkbox-label">
        <input v-model="form.skip_dkim" type="checkbox" />
        <span>Skip DKIM signing</span>
      </label>
    </FormField>
    <p v-else class="form-hint">
      {{ provider?.label }} rewrites headers and signs with its own key, so Mailyard's signature is
      always omitted for this provider - there is nothing to choose.
    </p>
    <FormField
      :error="errors.ses_topic_arn"
      hint="Only for Amazon SES. SES replaces the envelope sender, so its bounces can never come back as a delivery report - they arrive over SNS instead. Paste the topic here and notifications from it will be accepted for mail this server sent. Leave empty for anything else."
    >
      <template #label>SES topic ARN <span class="text-muted">(optional)</span></template>
      <input
        v-model="form.ses_topic_arn"
        type="text"
        class="form-input"
        placeholder="arn:aws:sns:eu-west-1:123456789012:ses-feedback"
      />
    </FormField>
    <FormField
      hint="One per line. Exact addresses or *@domain wildcards. Leave empty to allow any sender."
    >
      <template #label>Allowed Sender Emails <span class="text-muted">(optional)</span></template>
      <textarea
        v-model="allowedEmailsText"
        class="form-textarea"
        rows="3"
        placeholder="user@example.com&#10;*@example.com"
      ></textarea>
    </FormField>
    <template #footer>
      <button type="button" class="btn btn-secondary" @click="emit('close')">Cancel</button>
      <button type="submit" class="btn btn-primary" :disabled="saving">
        {{ saving ? 'Saving...' : server ? 'Update' : 'Create' }}
      </button>
    </template>
  </BaseModal>
</template>
