<script setup lang="ts">
// The dialog that writes a plan.
//
// Every limit is the same control asked fourteen times, so the fields are
// a LIST rather than fourteen blocks of markup - which is also what keeps
// the field-error key matching the payload key without anybody checking.
import { ref } from 'vue'
import { plansApi, type Plan, type PlanPayload } from '../../api/plans'
import { apiErrorMessage } from '../../api/client'
import { useNotificationStore } from '../../stores/notification'
import { useFieldErrors } from '../../composables/fieldErrors'
import BaseModal from '../../components/BaseModal.vue'
import FormField from '../../components/FormField.vue'

const props = defineProps<{
  /** The plan being edited, or null to create one. */
  plan: Plan | null
}>()

const emit = defineEmits<{ (e: 'saved'): void; (e: 'close'): void }>()

const notify = useNotificationStore()
const { errors, capture, clear } = useFieldErrors()

/** The numeric limits, in the order they are asked for. */
type LimitKey = Exclude<keyof PlanPayload, 'name' | 'description' | 'is_default'>

const limits: { key: LimitKey; label: string }[] = [
  { key: 'hourly_email_limit', label: 'Hourly email limit' },
  { key: 'daily_email_limit', label: 'Daily email limit' },
  { key: 'max_api_keys', label: 'Max API keys' },
  { key: 'max_smtp_servers', label: 'Max SMTP servers' },
  { key: 'max_domains', label: 'Max domains' },
  { key: 'max_subscribers', label: 'Max subscribers' },
  // The sandbox belongs to a project, so what bounds it is sold here.
  // The second is a CEILING - the project chooses its own window under
  // it, which is why the label says so.
  { key: 'max_sandbox_messages', label: 'Max sandbox messages kept' },
  { key: 'max_sandbox_retention_days', label: 'Max sandbox retention (days a project may choose)' },
]

const form = ref<PlanPayload>({
  name: props.plan?.name ?? '',
  description: props.plan?.description ?? '',
  is_default: props.plan?.is_default ?? false,
  hourly_email_limit: props.plan?.hourly_email_limit ?? 0,
  daily_email_limit: props.plan?.daily_email_limit ?? 0,
  max_api_keys: props.plan?.max_api_keys ?? 0,
  max_smtp_servers: props.plan?.max_smtp_servers ?? 0,
  max_domains: props.plan?.max_domains ?? 0,
  max_subscribers: props.plan?.max_subscribers ?? 0,
  max_sandbox_messages: props.plan?.max_sandbox_messages ?? 0,
  max_sandbox_retention_days: props.plan?.max_sandbox_retention_days ?? 0,
})

const saving = ref(false)

async function save() {
  const name = form.value.name.trim()
  if (!name) return

  clear()
  saving.value = true
  try {
    const payload: PlanPayload = { ...form.value, name }
    if (props.plan) {
      await plansApi.update(props.plan.id, payload)
      notify.success('Plan updated')
    } else {
      await plansApi.create(payload)
      notify.success('Plan created')
    }
    emit('saved')
  } catch (err) {
    // The limits are what get refused here, and there are fourteen of
    // them in one dialog - a summary line at the top right would leave
    // the reader counting fields.
    if (!capture(err)) notify.error(apiErrorMessage(err, 'Failed to save the plan'))
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <BaseModal :title="plan ? 'Edit Plan' : 'Create Plan'" form @submit="save" @close="emit('close')">
    <FormField label="Name" :error="errors.name">
      <input
        v-model="form.name"
        class="form-input"
        placeholder="Tiny, Max, Full..."
        maxlength="100"
        required
      />
    </FormField>

    <FormField label="Description" :error="errors.description">
      <input
        v-model="form.description"
        class="form-input"
        placeholder="Optional description"
        maxlength="500"
      />
    </FormField>

    <FormField>
      <label class="checkbox-label">
        <input v-model="form.is_default" type="checkbox" />
        <span>Default plan (applies to projects without an explicit plan)</span>
      </label>
    </FormField>

    <div class="form-row">
      <FormField v-for="f in limits" :key="f.key" :error="errors[f.key]">
        <template #label>{{ f.label }}</template>
        <input v-model.number="form[f.key]" type="number" min="0" class="form-input" />
      </FormField>
    </div>

    <p class="form-hint">Set a limit to 0 for unlimited.</p>

    <template #footer>
      <button type="button" class="btn btn-secondary" @click="emit('close')">Cancel</button>
      <button type="submit" class="btn btn-primary" :disabled="saving || !form.name.trim()">
        {{ saving ? 'Saving...' : plan ? 'Save' : 'Create Plan' }}
      </button>
    </template>
  </BaseModal>
</template>
