<script setup lang="ts">
// Every attempt made against one webhook.
//
// Paged with a CURSOR rather than a page number: a project subscribed to
// email.sent writes a delivery row per message, so this list grows with
// traffic and is walked from the newest end, never scrolled to the last
// page.
import { onMounted, ref } from 'vue'
import { webhooksApi } from '../../api/webhooks'
import { apiErrorMessage } from '../../api/client'
import type { Webhook, WebhookDelivery } from '../../api/types'
import { useNotificationStore } from '../../stores/notification'
import { formatDate } from '../../composables/formatDate'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import BaseModal from '../../components/BaseModal.vue'

const props = defineProps<{ webhook: Webhook }>()

const emit = defineEmits<{ (e: 'close'): void }>()

const notify = useNotificationStore()

const deliveries = ref<WebhookDelivery[]>([])
const loading = ref(true)
const loadingMore = ref(false)
const cursor = ref('')

async function load(more = false) {
  if (more && (!cursor.value || loadingMore.value)) return

  if (more) loadingMore.value = true
  else loading.value = true

  try {
    const res = await webhooksApi.deliveries(
      props.webhook.id,
      more ? { cursor: cursor.value } : undefined,
    )
    const page = res.data.deliveries ?? []
    deliveries.value = more ? deliveries.value.concat(page) : page
    cursor.value = res.data.next_cursor ?? ''
  } catch (e) {
    notify.error(
      apiErrorMessage(e, more ? 'Failed to load more deliveries' : 'Failed to load deliveries'),
    )
  } finally {
    loadingMore.value = false
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <BaseModal size="modal-w860" @close="emit('close')">
    <template #header>
      <h3 class="truncate">Deliveries - {{ webhook.url }}</h3>
    </template>

    <LoadingBlock v-if="loading" class="deliveries-pane" />

    <EmptyState
      v-else-if="deliveries.length === 0"
      class="deliveries-empty"
      title="No deliveries yet"
      text="Delivery attempts for this webhook will appear here."
    />

    <div v-else class="table-wrapper">
      <table>
        <thead>
          <tr>
            <th>Event</th>
            <th>Status</th>
            <th>HTTP</th>
            <th>Attempt</th>
            <th>Error</th>
            <th>Date</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="d in deliveries" :key="d.id">
            <td>
              <span class="badge badge-neutral">{{ d.event }}</span>
            </td>
            <td>
              <span
                class="badge badge-dot"
                :class="d.status === 'success' ? 'badge-success' : 'badge-danger'"
                >{{ d.status }}</span
              >
            </td>
            <td>{{ d.http_status || '-' }}</td>
            <td>{{ d.attempt }}</td>
            <td class="truncate w-filter" :title="d.error_message">
              {{ d.error_message || '-' }}
            </td>
            <td>{{ formatDate(d.created_at) }}</td>
          </tr>
        </tbody>
      </table>
    </div>

    <template #footer>
      <button v-if="cursor" class="btn btn-secondary" :disabled="loadingMore" @click="load(true)">
        {{ loadingMore ? 'Loading...' : 'Load more' }}
      </button>
      <button class="btn btn-secondary" @click="emit('close')">Close</button>
    </template>
  </BaseModal>
</template>

<style scoped>
.deliveries-pane {
  min-height: 120px;
}

.deliveries-empty {
  padding: 32px 16px;
}
</style>
