<script setup lang="ts">
// Everyone this project can send a campaign to.
//
// Searched and paged on the SERVER. It used to fetch one request's worth
// and filter that in the browser, which read as the whole audience: the
// list endpoint answers fifty rows by default, so a project past fifty
// subscribers showed the newest fifty and searched only those, with
// nothing on the page saying so. The endpoint matches the address and
// reports the total of what matched, and this page asks it.
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { subscribersApi } from '../../api/subscribers'
import { apiErrorMessage } from '../../api/client'
import type { Subscriber, SubscriberStatus } from '../../api/types'
import { SUBSCRIBER_STATUSES } from './statuses'
import type { Pageable } from '../../composables/usePagination'
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
import AddToListsModal from './AddToListsModal.vue'

const route = useRoute()
const router = useRouter()
const notify = useNotificationStore()
const projects = useProjectStore()
const { confirm } = useConfirm()
const { capture, clear } = useFieldErrors()

const rows = ref<Subscriber[]>([])
const total = ref(0)
const loading = ref(true)

const term = ref('')
const status = ref<SubscriberStatus | ''>('')

const PAGE = 20
const page = ref(0)

const pageable = computed<Pageable>(() => ({
  current_page: page.value,
  size: PAGE,
  total_pages: Math.max(1, Math.ceil(total.value / PAGE)),
  total_elements: total.value,
  empty: total.value === 0,
}))

function goToPage(p: number) {
  page.value = Math.max(0, p)
  void load()
}

// Debounced, so typing is not a request per keystroke. Back to the
// first page when the set under the pager changes, or a filter that
// leaves three rows shows page four of nothing.
let searchTimer: ReturnType<typeof setTimeout> | undefined
watch(term, () => {
  clearTimeout(searchTimer)
  searchTimer = setTimeout(() => goToPage(0), 300)
})
watch(status, () => goToPage(0))

const importing = ref(false)

// The subscriber whose lists are being picked, while the dialog is up.
const listing = ref<Subscriber | null>(null)

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
    const res = await subscribersApi.list({
      q: term.value.trim() || undefined,
      status: status.value || undefined,
      limit: PAGE,
      offset: page.value * PAGE,
    })
    rows.value = res.data.subscribers ?? []
    total.value = res.data.total ?? 0
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
        <input v-model="term" class="form-input w-search" placeholder="Part of an address" />

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

      <EmptyState v-else-if="rows.length === 0" title="No subscribers">
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
                v-for="s in rows"
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
                    <button
                      v-if="projects.can('subscribers:write')"
                      class="btn btn-secondary btn-sm"
                      @click="listing = s"
                    >
                      Add to list
                    </button>
                    <!-- delete, not write - DELETE /subscribers/:id is
                         permDelete on the server, so a member holding
                         write without delete must not see a button that
                         can only answer 403. -->
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
      <FormField label="Email" field="email">
        <input
          v-model="draft.email"
          type="email"
          class="form-input"
          placeholder="user@example.com"
        />
      </FormField>

      <FormField label="Name" field="name">
        <input v-model="draft.name" class="form-input" placeholder="Optional" />
      </FormField>

      <FormField label="Timezone" field="timezone">
        <input v-model="draft.timezone" class="form-input" placeholder="Europe/Berlin" />
      </FormField>

      <FormField label="Language" field="language">
        <input v-model="draft.language" class="form-input" placeholder="en" />
      </FormField>

      <FormField
        label="Custom fields (JSON)"
        field="custom_fields"
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

    <AddToListsModal v-if="listing" :subscriber="listing" @close="listing = null" />
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
