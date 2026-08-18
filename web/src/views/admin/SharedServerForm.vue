<script setup lang="ts">
// The dialog that adds or edits a server in the platform pool.
//
// Everything it asks for follows from the chosen PROVIDER: a row that
// dials wants a host, a port and an encryption mode, a row that talks to
// an API wants whatever that API declares. So there is no second list
// here saying which fields SES needs - the descriptor the server sent is
// the only source.
import { computed, ref } from 'vue'
import { sharedSmtpApi, type SharedSMTPPayload } from '../../api/sharedSmtp'
import { apiErrorMessage } from '../../api/client'
import type { MailProvider, SharedSMTPServer } from '../../api/types'
import { useNotificationStore } from '../../stores/notification'
import { useFieldErrors } from '../../composables/fieldErrors'
import BaseModal from '../../components/BaseModal.vue'
import FormField from '../../components/FormField.vue'

const props = defineProps<{
  providers: MailProvider[]
  /** The row being edited, or null to add one. */
  server: SharedSMTPServer | null
}>()

const emit = defineEmits<{ (e: 'saved'): void; (e: 'close'): void }>()

const notify = useNotificationStore()
const { errors, capture, clear } = useFieldErrors()

const saving = ref(false)

/**
 * The form state, which is the payload plus two fields the wire does not
 * carry: the domain list as typed, and the per-provider options before
 * the empty ones are dropped.
 */
function fromServer(srv: SharedSMTPServer | null) {
  return {
    name: srv?.name ?? '',
    provider: srv?.provider || 'smtp',
    providerConfig: { ...(srv?.provider_config ?? {}) } as Record<string, string>,
    host: srv?.host ?? '',
    port: srv?.port ?? 587,
    username: srv?.username ?? '',
    // Never prefilled: the API does not return it, and an empty value on
    // PATCH means "leave the stored password alone".
    password: '',
    encryption: srv?.encryption ?? 'starttls',
    skip_dkim: srv?.skip_dkim ?? false,
    ses_topic_arn: srv?.ses_topic_arn ?? '',
    security_mode: srv?.security_mode ?? 'permissive',
    platform_only: srv?.platform_only ?? false,
    priority: srv?.priority ?? 0,
    allowed_domains_text: (srv?.allowed_domains ?? []).join(', '),
  }
}

const form = ref(fromServer(props.server))

const provider = computed(() => props.providers.find((p) => p.id === form.value.provider) ?? null)
const dials = computed(() => provider.value?.dial ?? true)

function payload(): SharedSMTPPayload {
  const f = form.value
  const out: SharedSMTPPayload = {
    name: f.name,
    username: f.username,
    skip_dkim: f.skip_dkim,
    ses_topic_arn: f.ses_topic_arn.trim(),
    allowed_domains: f.allowed_domains_text
      .split(',')
      .map((d) => d.trim())
      .filter(Boolean),
    security_mode: f.security_mode,
    platform_only: f.platform_only,
    priority: Number(f.priority),
  }
  if (f.password) out.password = f.password

  // Only what this provider uses - a row that never dials would carry a
  // host nothing reads, and a reader of the table could not tell it from
  // one that does.
  if (dials.value) {
    out.host = f.host
    out.port = Number(f.port)
    out.encryption = f.encryption
  } else {
    const opts: Record<string, string> = {}
    for (const opt of provider.value?.options ?? []) {
      const v = (f.providerConfig[opt.key] ?? '').trim()
      if (v !== '') opts[opt.key] = v
    }
    out.provider_config = opts
  }

  // Create-only: the credentials mean something different to each
  // provider, so changing it would leave a row whose password belongs to
  // somebody else.
  if (!props.server) out.provider = f.provider

  return out
}

async function save() {
  clear()
  saving.value = true
  try {
    const editing = props.server
    if (editing) {
      await sharedSmtpApi.update(editing.id, payload())
      notify.success('Server updated')
    } else {
      await sharedSmtpApi.create(payload())
      notify.success('Server added to the pool')
    }
    emit('saved')
  } catch (e) {
    if (!capture(e)) notify.error(apiErrorMessage(e, 'Failed to save the server'))
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <BaseModal
    :title="server ? 'Edit Shared Server' : 'Add Shared Server'"
    form
    @submit="save"
    @close="emit('close')"
  >
    <FormField label="Name" :error="errors.name">
      <input v-model="form.name" class="form-input" placeholder="Company Outbound" />
    </FormField>

    <FormField
      label="Provider"
      :error="errors.provider"
      :hint="
        server
          ? 'Not changeable on an existing server - the credentials mean something different to each provider. Delete and recreate.'
          : ''
      "
    >
      <select v-model="form.provider" class="form-select" :disabled="!!server">
        <option v-for="p in providers" :key="p.id" :value="p.id">{{ p.label }}</option>
      </select>
    </FormField>

    <template v-if="dials">
      <FormField label="Host" :error="errors.host">
        <input v-model="form.host" class="form-input" placeholder="smtp.example.com" />
      </FormField>
      <FormField label="Port" :error="errors.port">
        <input v-model.number="form.port" type="number" class="form-input" min="1" max="65535" />
      </FormField>
    </template>

    <FormField v-for="opt in provider?.options ?? []" :key="opt.key" :hint="opt.hint">
      <template #label>
        {{ opt.label }} <span v-if="!opt.required" class="text-muted">(optional)</span>
      </template>
      <input v-model="form.providerConfig[opt.key]" class="form-input" :required="opt.required" />
    </FormField>

    <FormField :error="errors.username" :hint="provider?.credential_hint">
      <template #label>Username <span class="text-muted">(optional)</span></template>
      <input v-model="form.username" class="form-input" autocomplete="off" />
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

    <FormField v-if="dials" label="Encryption" :error="errors.encryption">
      <select v-model="form.encryption" class="form-select">
        <option value="none">None</option>
        <option value="starttls">STARTTLS</option>
        <option value="ssl">SSL</option>
      </select>
    </FormField>

    <FormField label="Priority" :error="errors.priority" hint="Lowest first. Ties broken by age.">
      <input v-model.number="form.priority" type="number" class="form-input" min="0" />
    </FormField>

    <FormField
      label="Allowed sender domains"
      hint="Comma separated. Empty allows any sender domain."
    >
      <input
        v-model="form.allowed_domains_text"
        class="form-input"
        placeholder="example.com, example.org"
      />
    </FormField>

    <FormField
      label="Security mode"
      :error="errors.security_mode"
      hint="Strict also requires the sending project to have verified the sender's domain, so one project cannot send as another's through these credentials."
    >
      <select v-model="form.security_mode" class="form-select">
        <option value="permissive">Permissive</option>
        <option value="strict">Strict</option>
      </select>
    </FormField>

    <!-- Offered only where it is a real choice. -->
    <FormField v-if="!provider?.re_signs">
      <label class="checkbox-label">
        <input v-model="form.skip_dkim" type="checkbox" />
        Skip Mailyard's DKIM signature (provider re-signs)
      </label>
    </FormField>
    <p v-else class="form-hint">
      {{ provider?.label }} signs with its own key, so Mailyard's signature is always omitted for
      this provider - there is nothing to choose.
    </p>

    <FormField
      hint="Invitations, password resets and signup confirmations leave through this server and no project ever does. Platform mail picks a reserved server over any other - leave this off and one shared server carries both, which is fine on a small install."
    >
      <label class="checkbox-label">
        <input v-model="form.platform_only" type="checkbox" />
        Reserve for platform mail only
      </label>
    </FormField>

    <FormField
      label="SES topic ARN"
      :error="errors.ses_topic_arn"
      hint="Only for Amazon SES. SES replaces the envelope sender, so its bounces can never come back as a delivery report - they arrive over SNS instead. Paste the topic here and Mailyard will accept notifications from it about mail this server sent. Leave empty for anything else."
    >
      <input
        v-model="form.ses_topic_arn"
        class="form-input"
        type="text"
        placeholder="arn:aws:sns:eu-west-1:123456789012:ses-feedback"
      />
    </FormField>

    <template #footer>
      <button type="button" class="btn btn-secondary" :disabled="saving" @click="emit('close')">
        Cancel
      </button>
      <button type="submit" class="btn btn-primary" :disabled="saving">
        {{ saving ? 'Saving...' : 'Save' }}
      </button>
    </template>
  </BaseModal>
</template>
