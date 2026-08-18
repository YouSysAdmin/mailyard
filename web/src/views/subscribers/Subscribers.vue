<script setup lang="ts">
// Everyone this project can send a campaign to.
//
// The whole list is fetched and filtered in the browser. That is a
// deliberate limit rather than an oversight: search here matches an
// address or a name against what is already on screen, which is instant
// and needs no endpoint. A project large enough for it to hurt wants a
// server-side search, and that is a change to the API before it is a
// change to this page.
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { subscribersApi } from '../../api/subscribers'
import { apiErrorMessage } from '../../api/client'
import type { Subscriber, SubscriberStatus } from '../../api/types'
import { SUBSCRIBER_STATUSES } from './statuses'
import { useClientPager } from '../../composables/usePagination'
import { useConfirm } from '../../composables/useConfirm'
import { useFieldErrors } from '../../composables/fieldErrors'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import { formatDate } from '../../composables/formatDate'
import Pagination from '../../components/Pagination.vue'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import PageHeader from '../../components/PageHeader.vue'
import BaseModal from '../../components/BaseModal.vue'
import FormField from '../../components/FormField.vue'
import SubscriberImport from './SubscriberImport.vue'

const route = useRoute()
const router = useRouter()
const notify = useNotificationStore()
const projects = useProjectStore()
const { confirm } = useConfirm()
const { errors, capture, clear } = useFieldErrors()

const all = ref<Subscriber[]>([])
const loading = ref(true)

const term = ref('')
const status = ref<SubscriberStatus | ''>('')

const matching = computed(() => {
  const q = term.value.trim().toLowerCase()

  return all.value.filter((s) => {
    if (status.value && s.status !== status.value) return false
    if (!q) return true

    return s.email.toLowerCase().includes(q) || (s.name ?? '').toLowerCase().includes(q)
  })
})

const { pageable, pageItems, goToPage } = useClientPager(matching, 20)

// Back to the first page when the set under the pager changes, or a
// filter that leaves three rows shows page four of nothing.
watch([term, status], () => goToPage(0))

const importing = ref(false)

const draft = ref<{
  email: string
  name: string
  timezone: string
  language: string
  custom: string
} | null>(null)
const saving = ref(false)

async function load() {
  loading.value = true
  try {
    all.value = (await subscribersApi.list()).data.subscribers ?? []
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load the subscribers'))
  } finally {
    loading.value = false
  }
}

function openDraft(email = '', name = '') {
  clear()
  draft.value = { email, name, timezone: '', language: '', custom: '' }
}

/**
 * Arriving from Contacts with an address to add.
 *
 * The query is CONSUMED - replaced away once read - so a refresh, or
 * coming back to this page later, does not reopen a form somebody
 * already dealt with. Same reason the send page does it with ?to=.
 *
 * Gated on the permission as well as the parameter: a member who cannot
 * write subscribers should get the list, not a form the server will
 * refuse. The button that sends them here is hidden for them too, but a
 * pasted link is not.
 */
function openFromQuery() {
  const email = typeof route.query.email === 'string' ? route.query.email : ''
  if (!email || !projects.can('subscribers:write')) return

  openDraft(email, typeof route.query.name === 'string' ? route.query.name : '')
  router.replace({ name: 'subscribers' })
}

async function create() {
  const form = draft.value
  if (!form || !form.email.trim()) return

  clear()
  saving.value = true
  try {
    await subscribersApi.create({
      email: form.email.trim(),
      name: form.name.trim() || undefined,
      timezone: form.timezone.trim() || undefined,
      language: form.language.trim() || undefined,
      custom_fields: form.custom.trim() ? JSON.parse(form.custom) : undefined,
    })
    draft.value = null
    notify.success('Subscriber added')
    await load()
  } catch (e) {
    if (e instanceof SyntaxError) notify.error('The custom fields are not valid JSON')
    else if (!capture(e)) notify.error(apiErrorMessage(e, 'Failed to add the subscriber'))
  } finally {
    saving.value = false
  }
}

async function remove(s: Subscriber) {
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
    await load()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to delete the subscriber'))
  }
}

/** Custom fields as one short line, since a cell has no room for more. */
function summarise(fields?: Record<string, unknown>): string {
  if (!fields || Object.keys(fields).length === 0) return '-'

  const text = JSON.stringify(fields)

  return text.length > 60 ? text.slice(0, 60) + '...' : text
}

// The send page already knows about templates, attachments, server
// groups and scheduling, so composing is a route rather than a dialog.
function composeTo(email: string) {
  router.push({ name: 'email-send', query: { to: email } })
}

// Either control, or the header and the cells disagree and every row
// shifts one column against its heading.
const anyRowActions = computed(
  () => projects.can('subscribers:write') || projects.can('subscribers:delete'),
)

void load().then(openFromQuery)
</script>

<template>
  <div>
    <PageHeader title="Subscribers" />

    <div class="card">
      <div class="card-header filters">
        <input v-model="term" class="form-input w-search" placeholder="Email or name" />

        <select v-model="status" class="form-select w-filter">
          <option value="">All statuses</option>
          <option v-for="s in SUBSCRIBER_STATUSES" :key="s.value" :value="s.value">
            {{ s.label }}
          </option>
        </select>

        <div v-if="projects.can('subscribers:write')" class="flex gap-2 ml-auto">
          <button class="btn btn-secondary" @click="importing = true">Import</button>
          <button class="btn btn-primary" @click="openDraft()">Add subscriber</button>
        </div>
      </div>

      <LoadingBlock v-if="loading" />

      <EmptyState v-else-if="matching.length === 0" title="No subscribers">
        <p v-if="term || status">Nothing matches those filters.</p>
        <p v-else>Add them one at a time, or import a file.</p>
      </EmptyState>

      <template v-else>
        <div class="table-wrapper">
          <table>
            <thead>
              <tr>
                <th>Email</th>
                <th>Name</th>
                <th>Status</th>
                <th>Custom fields</th>
                <th>Added</th>
                <th v-if="anyRowActions" class="text-right"></th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="s in pageItems"
                :key="s.id"
                class="row-clickable"
                @click="router.push(`/subscribers/${s.id}`)"
              >
                <td>{{ s.email }}</td>
                <td>{{ s.name || '-' }}</td>
                <td><StatusBadge :status="s.status" scope="subscriber" /></td>
                <td>{{ summarise(s.custom_fields) }}</td>
                <td>{{ formatDate(s.created_at) }}</td>
                <td v-if="anyRowActions" class="text-right">
                  <div class="table-actions" @click.stop>
                    <!-- Only an active subscriber. Composing to an
                         unsubscribed or bounced one offers a message
                         that is refused at accept time. -->
                    <button
                      v-if="s.status === 'subscribed' && projects.can('subscribers:write')"
                      class="btn btn-secondary btn-sm"
                      @click="composeTo(s.email)"
                    >
                      Send email
                    </button>
                    <!-- delete, not write - DELETE /subscribers/:id is
                         permDelete on the server, so a member holding
                         write without delete was offered a button that
                         could only ever answer 403. -->
                    <button
                      v-if="projects.can('subscribers:delete')"
                      class="btn btn-danger btn-sm"
                      @click="remove(s)"
                    >
                      Delete
                    </button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <Pagination :pageable="pageable" @page="goToPage" />
      </template>
    </div>

    <BaseModal v-if="draft" title="Add subscriber" @close="draft = null">
      <FormField label="Email" :error="errors.email">
        <input
          v-model="draft.email"
          type="email"
          class="form-input"
          placeholder="user@example.com"
        />
      </FormField>

      <FormField label="Name" :error="errors.name">
        <input v-model="draft.name" class="form-input" placeholder="Optional" />
      </FormField>

      <FormField label="Timezone" :error="errors.timezone">
        <input v-model="draft.timezone" class="form-input" placeholder="Europe/Berlin" />
      </FormField>

      <FormField label="Language" :error="errors.language">
        <input v-model="draft.language" class="form-input" placeholder="en" />
      </FormField>

      <FormField
        label="Custom fields (JSON)"
        :error="errors.custom_fields"
        hint="Anything a template or a segment rule should be able to read."
      >
        <textarea
          v-model="draft.custom"
          class="form-textarea code-font"
          rows="4"
          placeholder='{"company": "Acme", "plan": "pro"}'
        ></textarea>
      </FormField>

      <template #footer>
        <button class="btn btn-secondary" @click="draft = null">Cancel</button>
        <button class="btn btn-primary" :disabled="saving || !draft.email.trim()" @click="create">
          {{ saving ? 'Adding...' : 'Add' }}
        </button>
      </template>
    </BaseModal>

    <SubscriberImport v-if="importing" @imported="load" @close="importing = false" />
  </div>
</template>

<style scoped>
/* The filter row: two controls left, the buttons pushed right by their
   own ml-auto. Wraps rather than overflowing, because a search box and
   a select and two buttons do not fit a phone. */
.filters {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px;
}
</style>
