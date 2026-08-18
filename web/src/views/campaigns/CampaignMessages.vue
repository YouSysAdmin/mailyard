<script setup lang="ts">
// One row per recipient the campaign addressed.
//
// The table is a record of what happened to each message, and the
// interesting question about any one of them is what it actually said -
// so a row opens the email it produced.
//
// That body is fetched ON OPEN, not with the list. A campaign to twenty
// thousand people would otherwise pull twenty thousand rendered bodies
// to show one, and the row already carries everything the table needs.
import { computed, ref } from 'vue'
import type { CampaignMessage, CampaignMessageStatus, Email } from '../../api/types'
import { emailsApi } from '../../api/emails'
import { apiErrorMessage } from '../../api/client'
import { useNotificationStore } from '../../stores/notification'
import { useClientPager } from '../../composables/usePagination'
import { formatDate } from '../../composables/formatDate'
import Pagination from '../../components/Pagination.vue'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import BaseModal from '../../components/BaseModal.vue'
import HtmlPreview from '../../components/HtmlPreview.vue'

const props = defineProps<{
  messages: CampaignMessage[]
  loading: boolean
}>()

const notify = useNotificationStore()

const TABS: { label: string; value: CampaignMessageStatus | '' }[] = [
  { label: 'All', value: '' },
  { label: 'Pending', value: 'pending' },
  { label: 'Queued', value: 'queued' },
  { label: 'Sent', value: 'sent' },
  { label: 'Failed', value: 'failed' },
  { label: 'Skipped', value: 'skipped' },
]

const filter = ref<CampaignMessageStatus | ''>('')

const matching = computed(() =>
  filter.value ? props.messages.filter((m) => m.status === filter.value) : props.messages,
)

const { pageable, pageItems, goToPage } = useClientPager(matching, 20)

// The row that was opened, and the email it produced. Two refs because
// the row arrives first and the body follows.
const opened = ref<CampaignMessage | null>(null)
const email = ref<Email | null>(null)
const loadingEmail = ref(false)

async function open(msg: CampaignMessage) {
  if (!msg.email_id) return

  opened.value = msg
  email.value = null
  loadingEmail.value = true
  try {
    email.value = (await emailsApi.get(msg.email_id)).data.email
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load the sent email'))
    opened.value = null
  } finally {
    loadingEmail.value = false
  }
}
</script>

<template>
  <div class="card">
    <div class="card-header">
      <h2>Messages</h2>
    </div>

    <div class="card-body pb-0">
      <!-- The stylesheet's tab strip. These were buttons carrying two
           variant classes at once - btn-secondary from the static class
           and btn-primary from the bound one - so which won came down to
           the order the rules appear in. -->
      <div class="tabs">
        <button
          v-for="t in TABS"
          :key="t.value"
          class="tab"
          :class="{ active: filter === t.value }"
          @click="filter = t.value"
        >
          {{ t.label }}
        </button>
      </div>
    </div>

    <LoadingBlock v-if="loading" />

    <EmptyState
      v-else-if="matching.length === 0"
      :text="filter ? `Nothing ${filter} here.` : 'No messages yet.'"
    />

    <template v-else>
      <div class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th>Recipient</th>
              <th>Status</th>
              <th>Variant</th>
              <th>Why not</th>
              <th>Sent</th>
              <th>Opened</th>
              <th>Clicked</th>
            </tr>
          </thead>
          <tbody>
            <!-- Only a message that produced an email can be opened. A
                 pending or skipped one has no body to show, so its row
                 is not offered as clickable. -->
            <tr
              v-for="m in pageItems"
              :key="m.id"
              :class="{ 'row-clickable': !!m.email_id }"
              @click="open(m)"
            >
              <td>
                <strong v-if="m.email">{{ m.email }}</strong>
                <!-- The address comes from a LEFT JOIN on subscribers,
                     so it is empty exactly when that row has gone -
                     campaign_messages carries no foreign key to it, on
                     purpose: what a campaign did is history and must
                     survive the audience being edited. Falling back to
                     the raw subscriber_id put a uuid in a column headed
                     Recipient, which says nothing and reads as a
                     rendering fault. -->
                <span v-else class="text-muted" :title="`subscriber ${m.subscriber_id}`">
                  deleted subscriber
                </span>
              </td>
              <td><StatusBadge :status="m.status" scope="campaignMessage" /></td>
              <td>{{ m.variant || '-' }}</td>
              <td class="link-cell">{{ m.error_message || '-' }}</td>
              <td>{{ formatDate(m.sent_at) }}</td>
              <td>{{ formatDate(m.opened_at) }}</td>
              <td>{{ formatDate(m.clicked_at) }}</td>
            </tr>
          </tbody>
        </table>
      </div>

      <Pagination :pageable="pageable" @page="goToPage" />
    </template>

    <BaseModal
      v-if="opened"
      :title="opened.email || 'Deleted subscriber'"
      size="modal-w760"
      @close="opened = null"
    >
      <LoadingBlock v-if="loadingEmail" />

      <template v-else-if="email">
        <dl class="field-grid">
          <dt>Subject</dt>
          <dd>{{ email.subject }}</dd>

          <dt>From</dt>
          <dd>{{ email.sender }}</dd>

          <dt>To</dt>
          <dd>{{ email.recipients.join(', ') }}</dd>

          <dt>Status</dt>
          <dd><StatusBadge :status="email.status" scope="email" /></dd>

          <template v-if="email.error_message">
            <dt>Error</dt>
            <dd>{{ email.error_message }}</dd>
          </template>
        </dl>

        <!-- Sandboxed. It is operator-authored and already went out, but
             it is still markup from a template with subscriber data
             merged in - rendering it into this document would let it
             reach the console. -->
        <HtmlPreview
          v-if="email.html_body"
          :html="email.html_body"
          min-height="320px"
          title="Sent message"
        />
        <pre v-else-if="email.text_body" class="sent-text">{{ email.text_body }}</pre>
        <p v-else class="text-sm text-muted">
          The body was not kept - see the email body retention setting.
        </p>
      </template>

      <template #footer>
        <router-link v-if="email" class="btn btn-secondary" :to="`/emails/${email.id}`">
          Open the full view
        </router-link>
        <button class="btn btn-primary" @click="opened = null">Close</button>
      </template>
    </BaseModal>
  </div>
</template>

<style scoped>
.link-cell {
  max-width: 300px;
  overflow: hidden;
  text-overflow: ellipsis;
}
.field-grid {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: 6px 16px;
  margin: 0 0 16px;
}
.field-grid dt {
  font-size: 13px;
  color: var(--text-muted);
}
.field-grid dd {
  margin: 0;
  font-size: 13px;
  word-break: break-word;
}
.sent-text {
  white-space: pre-wrap;
  word-wrap: break-word;
  font-size: 13px;
  color: var(--text-secondary);
  line-height: 1.6;
  margin: 0;
}
</style>
