<script setup lang="ts">
// One subscriber: who they are, and everything the templates can read
// about them.
//
// The custom fields are an editor and NOT also a table. The page used to
// show both - a JSON textarea, then the same data again as key/value
// rows underneath - which is two things to read and one of them going
// stale the moment you typed.
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { subscribersApi } from '../../api/subscribers'
import { apiErrorMessage } from '../../api/client'
import type { Subscriber, SubscriberStatus } from '../../api/types'
import { SUBSCRIBER_STATUSES } from './statuses'
import { useConfirm } from '../../composables/useConfirm'
import { useFieldErrors } from '../../composables/fieldErrors'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import { formatDate } from '../../composables/formatDate'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import PageHeader from '../../components/PageHeader.vue'
import FormField from '../../components/FormField.vue'

const route = useRoute()
const router = useRouter()
const notify = useNotificationStore()
const projects = useProjectStore()
const { confirm } = useConfirm()
const { errors, capture, clear } = useFieldErrors()

const id = String(route.params.id)

const subscriber = ref<Subscriber | null>(null)
const loading = ref(true)
const saving = ref(false)

const form = ref({
  email: '',
  name: '',
  status: 'subscribed' as SubscriberStatus,
  timezone: '',
  language: '',
  custom: '',
})

/** The dates, which are recorded rather than edited. */
const recorded = computed(() => {
  const s = subscriber.value
  if (!s) return []

  return [
    { label: 'Subscribed', at: s.subscribed_at },
    { label: 'Unsubscribed', at: s.unsubscribed_at },
    { label: 'Added', at: s.created_at },
    { label: 'Last changed', at: s.updated_at },
  ]
})

function fill(s: Subscriber) {
  form.value = {
    email: s.email,
    name: s.name ?? '',
    status: s.status,
    timezone: s.timezone ?? '',
    language: s.language ?? '',
    // Pretty-printed, because this is the only view of it and a
    // one-line object is unreadable past two keys.
    custom: JSON.stringify(s.custom_fields ?? {}, null, 2),
  }
}

async function load() {
  try {
    const res = await subscribersApi.get(id)
    subscriber.value = res.data.subscriber
    fill(res.data.subscriber)
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load the subscriber'))
  } finally {
    loading.value = false
  }
}

async function save() {
  if (!subscriber.value) return

  clear()
  saving.value = true
  try {
    const res = await subscribersApi.update(subscriber.value.id, {
      email: form.value.email.trim(),
      name: form.value.name.trim(),
      status: form.value.status,
      timezone: form.value.timezone.trim(),
      language: form.value.language.trim(),
      custom_fields: form.value.custom.trim() ? JSON.parse(form.value.custom) : {},
    })
    subscriber.value = res.data.subscriber
    // Re-filled from the answer rather than left as typed: the server
    // normalises, and the box should show what is stored.
    fill(res.data.subscriber)
    notify.success('Saved')
  } catch (e) {
    if (e instanceof SyntaxError) notify.error('The custom fields are not valid JSON')
    else if (!capture(e)) notify.error(apiErrorMessage(e, 'Failed to save'))
  } finally {
    saving.value = false
  }
}

async function remove() {
  const s = subscriber.value
  if (!s) return

  const confirmed = await confirm({
    title: 'Delete subscriber',
    message: `Delete "${s.email}"? This cannot be undone.`,
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return

  try {
    await subscribersApi.remove(s.id)
    notify.success('Subscriber deleted')
    router.push('/subscribers')
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to delete the subscriber'))
  }
}

void load()
</script>

<template>
  <div>
    <PageHeader :title="subscriber?.email || 'Subscriber'">
      <button class="btn btn-secondary" @click="router.push('/subscribers')">
        All subscribers
      </button>
    </PageHeader>

    <LoadingBlock v-if="loading" />

    <template v-else-if="subscriber">
      <div class="card">
        <div class="card-header">
          <h2>Details</h2>
          <StatusBadge :status="subscriber.status" scope="subscriber" />
        </div>

        <div class="card-body">
          <FormField label="Email" :error="errors.email">
            <input v-model="form.email" type="email" class="form-input" />
          </FormField>

          <FormField label="Name" :error="errors.name">
            <input v-model="form.name" class="form-input" />
          </FormField>

          <FormField
            label="Status"
            :error="errors.status"
            hint="Only a subscribed address is sent to. The other three are records of why not."
          >
            <select v-model="form.status" class="form-select">
              <option v-for="s in SUBSCRIBER_STATUSES" :key="s.value" :value="s.value">
                {{ s.label }}
              </option>
            </select>
          </FormField>

          <FormField label="Timezone" :error="errors.timezone">
            <input v-model="form.timezone" class="form-input" placeholder="Europe/Berlin" />
          </FormField>

          <FormField label="Language" :error="errors.language">
            <input v-model="form.language" class="form-input" placeholder="en" />
          </FormField>

          <FormField
            label="Custom fields (JSON)"
            :error="errors.custom_fields"
            hint="What a template fills in, and what a dynamic list can be filtered on."
          >
            <textarea v-model="form.custom" class="form-textarea code-font" rows="8"></textarea>
          </FormField>

          <div v-if="projects.can('subscribers:write')" class="actions">
            <button class="btn btn-primary" :disabled="saving" @click="save">
              {{ saving ? 'Saving...' : 'Save' }}
            </button>
            <button
              v-if="projects.can('subscribers:delete')"
              class="btn btn-danger"
              @click="remove"
            >
              Delete
            </button>
          </div>
        </div>
      </div>

      <div class="card">
        <div class="card-header">
          <h2>History</h2>
        </div>

        <!-- A definition list rather than a table, and one class for
             every label: the four rows used to carry two different
             ones, so the first column was 160px wide and the other
             three were as wide as their text. -->
        <dl class="history">
          <template v-for="r in recorded" :key="r.label">
            <dt>{{ r.label }}</dt>
            <dd>{{ r.at ? formatDate(r.at) : 'never' }}</dd>
          </template>
        </dl>
      </div>
    </template>

    <EmptyState v-else title="No such subscriber" />
  </div>
</template>

<style scoped>
.actions {
  display: flex;
  gap: 8px;
  margin-top: 16px;
}

.history {
  display: grid;
  grid-template-columns: 160px 1fr;
  gap: 10px 16px;
  margin: 0;
  padding: 20px;
  font-size: 14px;
}

.history dt {
  color: var(--text-secondary);
  font-weight: 600;
}

.history dd {
  margin: 0;
  color: var(--text-primary);
}

@media (max-width: 640px) {
  .history {
    grid-template-columns: 1fr;
    gap: 4px 0;
  }

  .history dd {
    margin-bottom: 8px;
  }
}
</style>
