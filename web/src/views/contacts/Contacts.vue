<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { contactsApi, type Contact } from '../../api/contacts'
import { apiErrorMessage } from '../../api/client'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import { useConfirm } from '../../composables/useConfirm'
import { formatDate } from '../../composables/formatDate'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import PageHeader from '../../components/PageHeader.vue'
import BaseModal from '../../components/BaseModal.vue'
import FormField from '../../components/FormField.vue'
import ActionMenu from '../../components/ActionMenu.vue'

const router = useRouter()

const notify = useNotificationStore()
const projStore = useProjectStore()
const { confirm } = useConfirm()

// The clean-up dialog: a date, and everything idle since it goes.
const cleanupOpen = ref(false)
const cleanupBefore = ref('')
const cleaning = ref(false)

async function remove(c: Contact) {
  const ok = await confirm({
    title: 'Delete contact',
    message: `Delete ${c.email} and its tallies? The next delivery to this address creates a fresh contact - to stop sending to it, add a suppression instead.`,
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!ok) return

  try {
    await contactsApi.remove(c.id)
    notify.success('Contact deleted')
    await load()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to delete the contact'))
  }
}

async function cleanup() {
  if (!cleanupBefore.value) return

  cleaning.value = true
  try {
    const res = await contactsApi.removeInactive(new Date(cleanupBefore.value).toISOString())
    cleanupOpen.value = false
    cleanupBefore.value = ''
    notify.success(`Deleted ${res.data.deleted} inactive contacts`)
    offset.value = 0
    await load()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to clean up contacts'))
  } finally {
    cleaning.value = false
  }
}

const contacts = ref<Contact[]>([])
const total = ref(0)
const loading = ref(true)
const search = ref('')
const offset = ref(0)
const PAGE = 25

let searchTimer: ReturnType<typeof setTimeout> | undefined

const showing = computed(() => {
  if (total.value === 0) return 'No contacts'
  const from = offset.value + 1
  const to = Math.min(offset.value + contacts.value.length, total.value)
  return `${from}-${to} of ${total.value}`
})
const hasPrev = computed(() => offset.value > 0)
const hasNext = computed(() => offset.value + contacts.value.length < total.value)

async function load() {
  loading.value = true
  try {
    const res = await contactsApi.list({
      search: search.value || undefined,
      limit: PAGE,
      offset: offset.value,
    })
    contacts.value = res.data.contacts ?? []
    total.value = res.data.total ?? 0
  } catch (e) {
    contacts.value = []
    notify.error(apiErrorMessage(e, 'Failed to load contacts'))
  } finally {
    loading.value = false
  }
}

// Debounced so typing does not fire a query per keystroke.
watch(search, () => {
  clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    offset.value = 0
    load()
  }, 300)
})

watch(
  () => projStore.currentProjectId,
  () => {
    offset.value = 0
    search.value = ''
    load()
  },
)

function page(delta: number) {
  offset.value = Math.max(0, offset.value + delta * PAGE)
  load()
}

// A contact with failures and no sends is worth a second look.
function health(c: Contact) {
  if (c.suppressed) return { label: 'Suppressed', cls: 'badge-danger' }
  if (c.fail_count > 0 && c.sent_count === 0) return { label: 'Failing', cls: 'badge-warning' }
  if (c.fail_count > 0) return { label: 'Some failures', cls: 'badge-warning' }
  return { label: 'OK', cls: 'badge-success' }
}

onMounted(load)
// composeTo opens the send form with this address filled in.
//
// A route rather than a modal on purpose: the send page already knows
// about templates, attachments, server groups and scheduling, and a
// modal would either duplicate all of that or quietly offer less.
function composeTo(email: string) {
  router.push({ name: 'email-send', query: { to: email } })
}

// addAsSubscriber opens the subscriber form with this contact filled in.
//
// A route with query params, exactly like composeTo above and for the
// same reason: the subscriber form is where timezone, language and
// custom fields are, and a second form here would either duplicate all
// of that or quietly offer less. Nothing is created until somebody
// presses Create on that page - a contact is a record of delivery, and
// turning one into an audience member is a decision, not a side effect.
//
// Offered for a suppressed contact too. Being blocked is a fact about
// sending, which the subscriber list does not decide, and a suppression
// can be lifted the same afternoon.
function addAsSubscriber(c: Contact) {
  router.push({
    name: 'subscribers',
    query: { email: c.email, ...(c.name ? { name: c.name } : {}) },
  })
}
</script>

<template>
  <div>
    <PageHeader title="Contacts">
      <button
        v-if="projStore.can('contacts:delete')"
        class="btn btn-secondary"
        @click="cleanupOpen = true"
      >
        Clean up inactive
      </button>
    </PageHeader>

    <div class="card">
      <div class="card-body">
        <p class="text-sm text-muted mb-3">
          Every address this project has delivered to, tallied automatically as mail is sent. The
          tallies cannot be edited - to build an audience you can target, use
          <router-link to="/subscriber-lists">Subscriber Lists</router-link>.
        </p>
        <input
          v-model="search"
          type="search"
          class="form-input w-search"
          placeholder="Search by address or name"
        />
      </div>

      <LoadingBlock v-if="loading" />

      <template v-else>
        <EmptyState v-if="contacts.length === 0">
          <h3>{{ search ? 'No matching contacts' : 'No contacts yet' }}</h3>
          <p v-if="!search">
            Contacts appear here once a message to an address reaches a final state.
          </p>
        </EmptyState>

        <div v-else class="table-wrapper">
          <table>
            <thead>
              <tr>
                <th>Address</th>
                <th>Name</th>
                <th class="text-right">Sent</th>
                <th class="text-right">Failed</th>
                <th>Status</th>
                <th>Last sent</th>
                <th class="text-right">Actions</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="c in contacts" :key="c.id">
                <!-- Both truncate with the full value on hover: a long
                     address used to squeeze the name into a one-word
                     column, and a long name did the same to everything
                     after it. -->
                <td class="cell-title truncate w-search" :title="c.email">{{ c.email }}</td>
                <td class="truncate w-filter" :title="c.name">{{ c.name || '-' }}</td>
                <td class="text-right">{{ c.sent_count }}</td>
                <td class="text-right">{{ c.fail_count }}</td>
                <td>
                  <span class="badge badge-dot" :class="health(c).cls">{{ health(c).label }}</span>
                </td>
                <td>{{ formatDate(c.last_sent_at, 'Never') }}</td>
                <td class="text-right">
                  <div class="table-actions">
                    <ActionMenu>
                      <!-- Suppressed means we will not send to this
                           address, so offering to compose one is offering
                           a message that gets dropped at accept time. -->
                      <button
                        v-if="!c.suppressed"
                        type="button"
                        class="action-menu-item"
                        role="menuitem"
                        @click="composeTo(c.email)"
                      >
                        Send email
                      </button>
                      <button
                        v-if="projStore.can('subscribers:write')"
                        type="button"
                        class="action-menu-item"
                        role="menuitem"
                        @click="addAsSubscriber(c)"
                      >
                        Add to subscribers
                      </button>
                      <button
                        v-if="projStore.can('contacts:delete')"
                        type="button"
                        class="action-menu-item action-menu-item-danger"
                        role="menuitem"
                        @click="remove(c)"
                      >
                        Delete
                      </button>
                    </ActionMenu>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <div v-if="hasPrev || hasNext" class="pager">
          <button class="btn btn-secondary btn-sm" :disabled="!hasPrev" @click="page(-1)">
            Previous
          </button>
          <span class="text-sm text-muted">{{ showing }}</span>
          <button class="btn btn-secondary btn-sm" :disabled="!hasNext" @click="page(1)">
            Next
          </button>
        </div>
      </template>
    </div>

    <BaseModal
      v-if="cleanupOpen"
      title="Clean up inactive contacts"
      form
      @submit="cleanup"
      @close="cleanupOpen = false"
    >
      <p class="text-sm text-muted mb-3">
        Deletes every contact with no delivery - sent or failed - since the date you pick. A contact
        comes back with a fresh tally the next time mail reaches its address, so this forgets
        history and blocks nothing.
      </p>
      <FormField label="No activity since" for="cleanup-before">
        <input
          id="cleanup-before"
          v-model="cleanupBefore"
          type="datetime-local"
          class="form-input"
          required
        />
      </FormField>
      <template #footer>
        <button type="button" class="btn btn-secondary" @click="cleanupOpen = false">Cancel</button>
        <button type="submit" class="btn btn-danger" :disabled="cleaning || !cleanupBefore">
          {{ cleaning ? 'Deleting...' : 'Delete inactive contacts' }}
        </button>
      </template>
    </BaseModal>
  </div>
</template>

<style scoped></style>
