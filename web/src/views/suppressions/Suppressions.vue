<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { suppressionsApi } from '../../api/suppressions'
import { apiErrorMessage } from '../../api/client'
import type { Suppression } from '../../api/types'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import { useConfirm } from '../../composables/useConfirm'
import { useKeysetPager } from '../../composables/usePagination'
import { formatDate } from '../../composables/formatDate'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import PageHeader from '../../components/PageHeader.vue'
import BaseModal from '../../components/BaseModal.vue'
import FormField from '../../components/FormField.vue'
import { useFieldErrors } from '../../composables/fieldErrors'

const notify = useNotificationStore()
const projStore = useProjectStore()
const { confirm } = useConfirm()

const kindFilter = ref('')
const search = ref('')

const {
  items: suppressions,
  loading,
  loadingMore,
  hasMore,
  reload,
  loadMore,
} = useKeysetPager<Suppression>(async (cursor) => {
  const res = await suppressionsApi.list({
    kind: kindFilter.value || undefined,
    search: search.value.trim() || undefined,
    cursor: cursor || undefined,
  })
  return { items: res.data.suppressions ?? [], next: res.data.next_cursor ?? '' }
})

const showAddModal = ref(false)
const adding = ref(false)
const addForm = ref({ email: '', kind: 'manual', reason: '' })

const { errors: fieldErrors, capture, clear } = useFieldErrors()

async function load() {
  try {
    await reload()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load suppressions'))
  }
}

async function more() {
  try {
    await loadMore()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load more suppressions'))
  }
}

// Debounced, because every keystroke is a query against a table that
// can hold millions of rows.
let searchTimer: ReturnType<typeof setTimeout> | undefined
function onSearch() {
  clearTimeout(searchTimer)
  searchTimer = setTimeout(load, 250)
}

function openAdd() {
  addForm.value = { email: '', kind: 'manual', reason: '' }
  showAddModal.value = true
}

async function addSuppression() {
  clear()
  if (!addForm.value.email.trim()) return
  adding.value = true
  try {
    await suppressionsApi.create({
      email: addForm.value.email.trim(),
      kind: addForm.value.kind,
      reason: addForm.value.reason.trim() || undefined,
    })
    notify.success('Suppression added')
    showAddModal.value = false
    await load()
  } catch (e) {
    if (!capture(e)) notify.error(apiErrorMessage(e, 'Failed to add suppression'))
  } finally {
    adding.value = false
  }
}

// One row, one scope. A list opt-out and a global block are separate
// rows for the same address, so the button names the row it is on
// rather than the address. Naming the address would remove the person's
// list opt-outs along with the global block.
async function removeSuppression(sup: Suppression) {
  const scoped = !!sup.unsubscribe_list_id
  const ok = await confirm({
    title: scoped ? 'Remove List Opt-out' : 'Remove Suppression',
    message: scoped
      ? `Remove "${sup.email}" from this unsubscribe list? They asked to leave it, and this puts them back on it. Any global block stays.`
      : `Remove the global block on "${sup.email}"? This address will be able to receive emails again. Its unsubscribe list opt-outs are kept.`,
    confirmText: 'Remove',
    variant: 'warning',
  })
  if (!ok) return
  try {
    await suppressionsApi.remove(sup.email, sup.unsubscribe_list_id)
    notify.success('Suppression removed')
    await load()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to remove suppression'))
  }
}

function kindBadgeClass(kind: string) {
  switch (kind) {
    case 'hard':
      return 'badge badge-danger'
    case 'bounce':
      return 'badge badge-warning'
    case 'complaint':
      return 'badge badge-info'
    default:
      return 'badge badge-neutral'
  }
}

watch(kindFilter, load)
watch(() => projStore.currentProjectId, load)
onMounted(load)
</script>

<template>
  <div>
    <PageHeader title="Suppressions">
      <button v-if="projStore.can('suppressions:write')" class="btn btn-primary" @click="openAdd">
        Add Suppression
      </button>
    </PageHeader>

    <div class="filter-bar">
      <!-- Search first, because it is what the page is for. "Is this
           customer blocked" is not a question paging answers on a
           list with millions of rows. -->
      <input
        v-model="search"
        type="search"
        class="form-input w-search"
        placeholder="Search by address..."
        @input="onSearch"
      />
      <select v-model="kindFilter" class="form-select w-filter">
        <option value="">All kinds</option>
        <option value="hard">Hard</option>
        <option value="bounce">Bounce</option>
        <option value="complaint">Complaint</option>
        <option value="manual">Manual</option>
      </select>
    </div>

    <LoadingBlock v-if="loading" />

    <div v-else class="card">
      <EmptyState v-if="suppressions.length === 0">
        <h3>{{ search.trim() ? 'No match' : 'No suppressions' }}</h3>
        <p v-if="search.trim()">
          No suppressed address starts with "{{ search.trim() }}". Search matches the beginning of
          an address.
        </p>
        <p v-else>
          Suppressed addresses are never sent to. Add one manually or let bounces populate this
          list.
        </p>
      </EmptyState>

      <template v-else>
        <div class="table-wrapper">
          <table>
            <thead>
              <tr>
                <th>Email</th>
                <th>Kind</th>
                <!-- Scope, because Remove now acts on the row and not on
                     the address, and the two mean different things. -->
                <th>Scope</th>
                <th>Reason</th>
                <th>Created</th>
                <th class="text-right">Actions</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="sup in suppressions" :key="sup.id">
                <td class="cell-title">{{ sup.email }}</td>
                <td>
                  <span :class="kindBadgeClass(sup.kind)">{{ sup.kind }}</span>
                </td>
                <td>
                  <span
                    :class="sup.unsubscribe_list_id ? 'badge badge-info' : 'badge badge-neutral'"
                  >
                    {{ sup.unsubscribe_list_id ? 'List' : 'Global' }}
                  </span>
                </td>
                <td class="truncate w-search">{{ sup.reason || '-' }}</td>
                <td>{{ formatDate(sup.created_at) }}</td>
                <td>
                  <div class="table-actions">
                    <button
                      v-if="projStore.can('suppressions:delete')"
                      class="btn btn-danger btn-sm"
                      @click="removeSuppression(sup)"
                    >
                      Remove
                    </button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-if="hasMore" class="load-more">
          <button class="btn btn-secondary" :disabled="loadingMore" @click="more">
            {{ loadingMore ? 'Loading...' : 'Load more' }}
          </button>
        </div>
      </template>
    </div>

    <!-- Add Suppression Modal -->
    <BaseModal
      v-if="showAddModal"
      title="Add Suppression"
      form
      @submit="addSuppression"
      @close="showAddModal = false"
    >
      <FormField label="Email" :error="fieldErrors.email">
        <input
          v-model="addForm.email"
          type="email"
          class="form-input"
          placeholder="user@example.com"
          required
        />
      </FormField>
      <FormField label="Kind" :error="fieldErrors.kind">
        <select v-model="addForm.kind" class="form-select">
          <option value="manual">Manual</option>
          <option value="hard">Hard</option>
          <option value="bounce">Bounce</option>
          <option value="complaint">Complaint</option>
        </select>
      </FormField>
      <FormField :error="fieldErrors.reason">
        <template #label>Reason <span class="text-muted">(optional)</span></template>
        <input
          v-model="addForm.reason"
          type="text"
          class="form-input"
          placeholder="Why this address is blocked"
        />
      </FormField>
      <template #footer>
        <button type="button" class="btn btn-secondary" @click="showAddModal = false">
          Cancel
        </button>
        <button type="submit" class="btn btn-primary" :disabled="adding || !addForm.email.trim()">
          {{ adding ? 'Adding...' : 'Add' }}
        </button>
      </template>
    </BaseModal>
  </div>
</template>

<style scoped></style>
