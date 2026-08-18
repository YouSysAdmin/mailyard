<script setup lang="ts">
// The dialog that registers a webhook.
//
// Create only - there is no edit, because the signing secret is minted
// with the row and shown once. Changing the URL of a live subscription
// silently redirects signed events at a host the secret was never given
// to, so a new destination is a new webhook.
import { ref } from 'vue'
import { webhooksApi } from '../../api/webhooks'
import { apiErrorMessage } from '../../api/client'
import { WEBHOOK_EVENTS } from '../../api/types'
import { useNotificationStore } from '../../stores/notification'
import { useFieldErrors } from '../../composables/fieldErrors'
import BaseModal from '../../components/BaseModal.vue'
import FormField from '../../components/FormField.vue'
import Notice from '../../components/Notice.vue'

const emit = defineEmits<{
  /** Carries the signing secret, which is returned exactly once. */
  (e: 'created', secret: string): void
  (e: 'close'): void
}>()

const notify = useNotificationStore()
const { errors, capture, clear } = useFieldErrors()

const url = ref('')
const events = ref<string[]>([])
const filtersText = ref('')
const creating = ref(false)

function toggle(event: string) {
  const i = events.value.indexOf(event)
  if (i === -1) events.value.push(event)
  else events.value.splice(i, 1)
}

async function create() {
  const target = url.value.trim()
  if (!target || events.value.length === 0) return

  clear()
  creating.value = true
  try {
    // One filter per line, lowercased here because an address is matched
    // case-insensitively and a stored `User@Example.com` would read as a
    // different rule from the one beside it.
    const filters = filtersText.value
      .split(/\r?\n/)
      .map((f) => f.trim().toLowerCase())
      .filter(Boolean)

    const res = await webhooksApi.create({
      url: target,
      events: [...events.value],
      filters: filters.length > 0 ? filters : undefined,
    })
    notify.success('Webhook created')
    emit('created', res.data.secret)
  } catch (e) {
    if (!capture(e)) notify.error(apiErrorMessage(e, 'Failed to create webhook'))
  } finally {
    creating.value = false
  }
}
</script>

<template>
  <BaseModal title="Add Webhook" form @submit="create" @close="emit('close')">
    <FormField label="URL" :error="errors.url">
      <input
        v-model="url"
        type="url"
        class="form-input"
        placeholder="https://example.com/hooks/mailyard"
        required
      />
    </FormField>

    <FormField label="Events">
      <div class="event-list">
        <label v-for="event in WEBHOOK_EVENTS" :key="event" class="checkbox-label">
          <input type="checkbox" :checked="events.includes(event)" @change="toggle(event)" />
          {{ event }}
        </label>
      </div>
    </FormField>

    <FormField
      hint="One per line. Exact addresses or *@domain wildcards. Leave empty to fire for all senders."
    >
      <template #label>Sender Filters <span class="text-muted">(optional)</span></template>
      <textarea
        v-model="filtersText"
        class="form-textarea"
        rows="3"
        placeholder="user@example.com&#10;*@example.com"
      ></textarea>
    </FormField>

    <Notice>
      <p>
        A signing secret is generated on creation and shown once. Deliveries carry an
        <code>X-Mailyard-Signature</code> HMAC-SHA256 header.
      </p>
    </Notice>

    <template #footer>
      <button type="button" class="btn btn-secondary" @click="emit('close')">Cancel</button>
      <button
        type="submit"
        class="btn btn-primary"
        :disabled="creating || !url.trim() || events.length === 0"
      >
        {{ creating ? 'Creating...' : 'Create Webhook' }}
      </button>
    </template>
  </BaseModal>
</template>

<style scoped>
.event-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-top: 4px;
}
</style>
