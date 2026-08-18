<script setup lang="ts">
// Outgoing event subscriptions for this project.
//
// Each row is a URL, the events it wants and an optional list of sender
// addresses narrowing it further. The signing secret is not here at all -
// it is minted with the row, shown once, and never returned again.
import { onMounted, ref, watch } from 'vue'
import { webhooksApi } from '../../api/webhooks'
import { apiErrorMessage } from '../../api/client'
import type { Webhook } from '../../api/types'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import { useConfirm } from '../../composables/useConfirm'
import { formatDate } from '../../composables/formatDate'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import PageHeader from '../../components/PageHeader.vue'
import BaseModal from '../../components/BaseModal.vue'
import CopyButton from '../../components/CopyButton.vue'
import WebhookForm from './WebhookForm.vue'
import WebhookDeliveries from './WebhookDeliveries.vue'
import Notice from '../../components/Notice.vue'

const notify = useNotificationStore()
const projStore = useProjectStore()
const { confirm } = useConfirm()

const webhooks = ref<Webhook[]>([])
const loading = ref(true)

const showForm = ref(false)
const secret = ref('')
const deliveriesFor = ref<Webhook | null>(null)

async function load() {
  loading.value = true
  try {
    webhooks.value = (await webhooksApi.list()).data.webhooks ?? []
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load webhooks'))
  } finally {
    loading.value = false
  }
}

async function created(minted: string) {
  showForm.value = false
  secret.value = minted
  await load()
}

async function remove(hook: Webhook) {
  const ok = await confirm({
    title: 'Delete Webhook',
    message: `Delete the webhook for "${hook.url}"? No further events will be delivered to it.`,
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!ok) return

  try {
    await webhooksApi.remove(hook.id)
    notify.success('Webhook deleted')
    await load()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to delete webhook'))
  }
}

watch(() => projStore.currentProjectId, load)
onMounted(load)
</script>

<template>
  <div>
    <PageHeader title="Webhooks">
      <button
        v-if="projStore.can('webhooks:write')"
        class="btn btn-primary"
        @click="showForm = true"
      >
        Add Webhook
      </button>
    </PageHeader>

    <LoadingBlock v-if="loading" />

    <div v-else class="card">
      <EmptyState
        v-if="webhooks.length === 0"
        title="No webhooks"
        text="Add a webhook to receive signed event notifications."
      />

      <div v-else class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th>URL</th>
              <th>Events</th>
              <th>Sender Filters</th>
              <th>Created</th>
              <th class="text-right">Actions</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="hook in webhooks" :key="hook.id">
              <td class="truncate cell-title w-search" :title="hook.url">{{ hook.url }}</td>
              <td>
                <div class="flex gap-2 flex-wrap">
                  <span v-for="event in hook.events" :key="event" class="badge badge-info">{{
                    event
                  }}</span>
                </div>
              </td>
              <td>
                <div v-if="hook.filters?.length" class="flex gap-2 flex-wrap">
                  <span v-for="filter in hook.filters" :key="filter" class="badge badge-neutral">{{
                    filter
                  }}</span>
                </div>
                <span v-else class="text-muted">All senders</span>
              </td>
              <td>{{ formatDate(hook.created_at) }}</td>
              <td>
                <div class="table-actions">
                  <button class="btn btn-secondary btn-sm" @click="deliveriesFor = hook">
                    Deliveries
                  </button>
                  <button
                    v-if="projStore.can('webhooks:delete')"
                    class="btn btn-danger btn-sm"
                    @click="remove(hook)"
                  >
                    Delete
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <WebhookForm v-if="showForm" @created="created" @close="showForm = false" />

    <!-- Persistent: this is the only time the secret exists on screen, so
         a stray Escape would lose it for good. -->
    <BaseModal v-if="secret" title="Webhook Signing Secret" persistent>
      <Notice kind="warning" title="Save this secret now" class="mb-4">
        <p>It will not be shown again.</p>
      </Notice>

      <div class="code-block">{{ secret }}</div>

      <p class="text-sm text-muted mt-3">
        Use it to verify the <code>X-Mailyard-Signature</code> header, an HMAC-SHA256 signature of
        the raw request body.
      </p>

      <CopyButton
        :value="secret"
        label="Copy Secret"
        copied-label="Copied!"
        variant="btn btn-secondary btn-sm mt-4"
      />

      <template #footer>
        <button class="btn btn-primary" @click="secret = ''">Done</button>
      </template>
    </BaseModal>

    <WebhookDeliveries
      v-if="deliveriesFor"
      :webhook="deliveriesFor"
      @close="deliveriesFor = null"
    />
  </div>
</template>
