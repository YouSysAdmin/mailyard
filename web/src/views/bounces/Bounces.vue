<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { bouncesApi } from '../../api/bounces'
import { suppressionsApi } from '../../api/suppressions'
import { apiErrorMessage } from '../../api/client'
import type { Bounce } from '../../api/types'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import { useKeysetPager } from '../../composables/usePagination'
import { useConfirm } from '../../composables/useConfirm'
import { formatDate } from '../../composables/formatDate'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import PageHeader from '../../components/PageHeader.vue'

const notify = useNotificationStore()
const projStore = useProjectStore()
const { confirm } = useConfirm()
const removing = ref('')

// Filtering happens on the server. A computed over whatever the first
// page held would make "show me hard bounces" filter the most recent few
// hundred rows and call that an answer.
const typeFilter = ref('')
const search = ref('')

const {
  items: bounces,
  loading,
  loadingMore,
  hasMore,
  reload,
  loadMore,
} = useKeysetPager<Bounce>(async (cursor) => {
  const res = await bouncesApi.list({
    type: typeFilter.value || undefined,
    search: search.value.trim() || undefined,
    cursor: cursor || undefined,
  })
  return { items: res.data.bounces ?? [], next: res.data.next_cursor ?? '' }
})

async function load() {
  try {
    await reload()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load bounces'))
  }
}

async function more() {
  try {
    await loadMore()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load more bounces'))
  }
}

let searchTimer: ReturnType<typeof setTimeout> | undefined
function onSearch() {
  clearTimeout(searchTimer)
  searchTimer = setTimeout(load, 250)
}

// Clear an address that has started working.
//
// The case: a customer hands over a list, some of the mailboxes have not
// been created on their side yet, those bounce, and once the mailboxes
// exist the addresses are still blocked.
//
// There are two records behind one address. The bounce reports are
// history, and they are what keeps the verifier calling the address
// previously bounced. The block itself is a suppression row, which is
// what FilterSuppressed consults on every send. Deleting only the
// reports looks like it worked and still delivers nothing, so this
// button does both - two calls, because they are two resources with two
// permissions.
async function remove(email: string) {
  const alsoUnblock = projStore.can('suppressions:delete')
  const ok = await confirm({
    title: 'Remove bounces',
    message: alsoUnblock
      ? `Delete every bounce report for "${email}" and remove it from the suppression list, so mail is sent to it again?`
      : `Delete every bounce report for "${email}"? This clears the history only - the address stays blocked, ` +
        `which needs the Suppressions page and permission to delete there.`,
    confirmText: 'Remove',
    variant: 'warning',
  })
  if (!ok) return

  removing.value = email
  try {
    const res = await bouncesApi.remove(email)
    let unblocked = false
    if (alsoUnblock) {
      try {
        await suppressionsApi.remove(email)
        unblocked = true
      } catch (e) {
        // 404 is the ordinary answer for a soft bounce, which never
        // suppressed anything. Anything else is worth saying out loud,
        // because the address is still blocked.
        if ((e as { response?: { status?: number } })?.response?.status !== 404) {
          notify.error(apiErrorMessage(e, 'Removed the reports, but the address is still blocked'))
        }
      }
    }
    const n = res.data?.deleted ?? 0
    notify.success(
      unblocked
        ? `Removed ${n} report(s) for ${email} and unblocked the address`
        : `Removed ${n} report(s) for ${email}`,
    )
    // Dropped from the list here rather than by reloading. This list is
    // one of the reads that may be served by a replica, so re-fetching
    // it straight after a write can hand back the rows just deleted.
    bounces.value = bounces.value.filter((b) => b.recipient.toLowerCase() !== email.toLowerCase())
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to remove the bounce reports'))
  } finally {
    removing.value = ''
  }
}

function typeBadgeClass(type: string) {
  switch (type) {
    case 'hard':
      return 'badge badge-danger'
    case 'soft':
      return 'badge badge-warning'
    case 'complaint':
      return 'badge badge-info'
    default:
      return 'badge badge-neutral'
  }
}

watch(typeFilter, load)
watch(() => projStore.currentProjectId, load)
onMounted(load)
</script>

<template>
  <div>
    <PageHeader title="Bounces" />

    <div class="filter-bar">
      <input
        v-model="search"
        type="search"
        class="form-input w-search"
        placeholder="Search by recipient..."
        @input="onSearch"
      />
      <select v-model="typeFilter" class="form-select w-filter">
        <option value="">All types</option>
        <option value="hard">Hard</option>
        <option value="soft">Soft</option>
        <option value="complaint">Complaint</option>
      </select>
    </div>

    <LoadingBlock v-if="loading" />

    <div v-else class="card">
      <EmptyState v-if="bounces.length === 0">
        <h3>{{ search.trim() || typeFilter ? 'No match' : 'No bounces' }}</h3>
        <p v-if="search.trim()">No bounce for a recipient starting with "{{ search.trim() }}".</p>
        <p v-else-if="typeFilter">No {{ typeFilter }} bounces recorded.</p>
        <p v-else>Delivery failures will appear here.</p>
      </EmptyState>

      <template v-else>
        <div class="table-wrapper">
          <table>
            <thead>
              <tr>
                <th>Recipient</th>
                <th>Type</th>
                <th>Reason</th>
                <th>Email</th>
                <th>Date</th>
                <th v-if="projStore.can('bounces:delete')" class="text-right">Actions</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="bounce in bounces" :key="bounce.id">
                <td class="cell-title">{{ bounce.recipient }}</td>
                <td>
                  <span :class="typeBadgeClass(bounce.type)">{{ bounce.type }}</span>
                </td>
                <td class="truncate w-search">{{ bounce.reason || '-' }}</td>
                <td>
                  <router-link v-if="bounce.email_id" :to="`/emails/${bounce.email_id}`"
                    >View email</router-link
                  >
                  <span v-else class="text-muted">-</span>
                </td>
                <td>{{ formatDate(bounce.created_at) }}</td>
                <td v-if="projStore.can('bounces:delete')" class="text-right">
                  <!-- Named for what it does to the RECIPIENT, not to
                       this row: it removes every report for the address,
                       and the confirmation says whether the block goes
                       with them. -->
                  <button
                    class="btn btn-sm btn-danger"
                    :disabled="removing === bounce.recipient"
                    @click="remove(bounce.recipient)"
                  >
                    {{ removing === bounce.recipient ? 'Removing...' : 'Remove' }}
                  </button>
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
  </div>
</template>

<style scoped></style>
