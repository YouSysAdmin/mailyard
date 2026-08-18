<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { apiErrorMessage } from '../../api/client'
import { campaignsApi, type TrackedLink } from '../../api/campaigns'
import type { Campaign, CampaignMessage } from '../../api/types'
import { useNotificationStore } from '../../stores/notification'
import { useCampaignActions } from '../../composables/campaignActions'
import { useProjectStore } from '../../stores/project'
import { formatDate } from '../../composables/formatDate'
import { useAutoRefresh } from '../../composables/useAutoRefresh'
import RefreshControl from '../../components/RefreshControl.vue'
import LoadingBlock from '../../components/LoadingBlock.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import PageHeader from '../../components/PageHeader.vue'
import CampaignEdit from './CampaignEdit.vue'
import CampaignMessages from './CampaignMessages.vue'
import CampaignStats from './CampaignStats.vue'
import CampaignSchedule from './CampaignSchedule.vue'
import { useFieldErrors } from '../../composables/fieldErrors'
import { formatMailbox } from '../../composables/mailbox'

const route = useRoute()
const router = useRouter()
const notify = useNotificationStore()
const projStore = useProjectStore()

const campaignId = String(route.params.id)
const campaign = ref<Campaign | null>(null)
const stats = ref<Record<string, number>>({})
const statsByVariant = ref<Record<string, Record<string, number>>>({})
const engagement = ref({ opened: 0, clicked: 0, sent: 0, open_rate: 0, click_rate: 0 })

const { errors: fieldErrors, capture, clear } = useFieldErrors()

const loading = ref(true)

// Messages
const messages = ref<CampaignMessage[]>([])
const messagesLoading = ref(false)

// Action availability
const canSend = computed(
  () => campaign.value?.status === 'draft' || campaign.value?.status === 'scheduled',
)
const canPause = computed(() => campaign.value?.status === 'sending')
const canResume = computed(() => campaign.value?.status === 'paused')
const canCancel = computed(() =>
  ['scheduled', 'sending', 'paused'].includes(campaign.value?.status ?? ''),
)
const canDelete = computed(
  () => campaign.value?.status === 'draft' || campaign.value?.status === 'cancelled',
)
const isDraft = computed(() => campaign.value?.status === 'draft')

// Schedule modal
const showScheduleModal = ref(false)

// Only a draft is editable, so the form is a card the page reveals.
const showEdit = ref(false)

function onEdited() {
  showEdit.value = false
  void loadCampaign()
}

async function loadCampaign(quiet = false) {
  try {
    const res = await campaignsApi.get(campaignId)
    campaign.value = res.data.campaign
    stats.value = res.data.stats ?? {}
    statsByVariant.value = res.data.stats_by_variant ?? {}
    engagement.value = res.data.engagement ?? {
      opened: 0,
      clicked: 0,
      sent: 0,
      open_rate: 0,
      click_rate: 0,
    }
  } catch (e) {
    if (!quiet) {
      notify.error(apiErrorMessage(e, 'Failed to load campaign'))
      router.push('/campaigns')
    }
  }
  // Per-link tallies, loaded separately: informational, and the page
  // must not fail if analytics does.
  try {
    const res = await campaignsApi.analytics(campaignId)
    trackedLinks.value = res.data.links ?? []
  } catch {
    trackedLinks.value = []
  }
}

const trackedLinks = ref<TrackedLink[]>([])

async function loadMessages(quiet = false) {
  if (!quiet) messagesLoading.value = true
  try {
    const res = await campaignsApi.messages(campaignId)
    messages.value = res.data.messages ?? []
  } catch (e) {
    if (!quiet) notify.error(apiErrorMessage(e, 'Failed to load messages'))
  } finally {
    if (!quiet) messagesLoading.value = false
  }
}

// A campaign that is working moves on its own, and the counts are what
// somebody watching it came for. Paused when it is not: a draft or a
// finished campaign answers the same thing every ten seconds.
//
// loadCampaign carries its own toast and a redirect on failure, which is
// right on arrival and wrong on a refresh - a blip would throw the reader
// back to the list - so the automatic path calls a quiet version.
const isMoving = computed(
  () => campaign.value?.status === 'sending' || campaign.value?.status === 'paused',
)
const { refreshing, refresh, auto, paused, everySeconds } = useAutoRefresh(
  async () => {
    await loadCampaign(true)
    await loadMessages(true)
  },
  {
    storageKey: 'mailyard.autorefresh.campaign',
    pauseWhen: () => !isMoving.value,
  },
)

// Every act a campaign offers, shared with the list page. Duplicate and
// delete answer rather than reload, because from here they change which
// page the reader should be on.
const {
  busy: actionBusy,
  send,
  schedule,
  pause,
  resume,
  cancel,
  duplicate,
  remove,
} = useCampaignActions(async () => {
  await loadCampaign()
  await loadMessages()
})

/** The campaign as the actions need it, once it has arrived. */
function target() {
  return { id: campaignId, name: campaign.value?.name ?? '' }
}

async function sendCampaign() {
  await send(target())
}

async function scheduleCampaign(at: string) {
  showScheduleModal.value = false
  await schedule(target(), at)
}

async function pauseCampaign() {
  await pause(target())
}

async function resumeCampaign() {
  await resume(target())
}

async function cancelCampaign() {
  await cancel(target())
}

// From here, the copy is what the reader wants to look at next.
async function duplicateCampaign() {
  const copy = await duplicate(target())
  if (copy) router.push(`/campaigns/${copy.id}`)
}

// And deleting leaves nothing on this page to be looking at.
async function deleteCampaign() {
  if (await remove(target())) router.push('/campaigns')
}

/**
 * Read the campaign and its messages, once, on arrival.
 *
 * This was missing: `onMounted` was imported and never called, and
 * `loading` was set true and never cleared - so the page rendered its
 * spinner and nothing else, from the root commit onwards. Every path to
 * a campaign went through it.
 */
async function start() {
  await loadCampaign()
  await loadMessages()
  loading.value = false
}

void start()
</script>

<template>
  <div>
    <PageHeader>
      <template #title>
        <button class="btn btn-secondary btn-sm mb-2" @click="router.push('/campaigns')">
          Back to Campaigns
        </button>
        <h1 v-if="campaign">{{ campaign.name }}</h1>
      </template>

      <RefreshControl
        :every-seconds="everySeconds"
        :refreshing="refreshing"
        :auto="auto"
        :paused="paused"
        @refresh="refresh"
        @update:auto="auto = $event"
      />
      <template v-if="campaign && projStore.can('campaigns:write')">
        <button v-if="isDraft && !showEdit" class="btn btn-secondary" @click="showEdit = true">
          Edit
        </button>
        <button v-if="canSend" class="btn btn-primary" :disabled="actionBusy" @click="sendCampaign">
          Send Now
        </button>
        <button
          v-if="canSend"
          class="btn btn-secondary"
          :disabled="actionBusy"
          @click="showScheduleModal = true"
        >
          Schedule
        </button>
        <button
          v-if="canPause"
          class="btn btn-warning"
          :disabled="actionBusy"
          @click="pauseCampaign"
        >
          Pause
        </button>
        <button
          v-if="canResume"
          class="btn btn-primary"
          :disabled="actionBusy"
          @click="resumeCampaign"
        >
          Resume
        </button>
        <button
          v-if="canCancel"
          class="btn btn-danger"
          :disabled="actionBusy"
          @click="cancelCampaign"
        >
          Cancel
        </button>
        <button class="btn btn-secondary" :disabled="actionBusy" @click="duplicateCampaign">
          Duplicate
        </button>
        <button
          v-if="projStore.can('campaigns:delete') && canDelete"
          class="btn btn-danger"
          :disabled="actionBusy"
          @click="deleteCampaign"
        >
          Delete
        </button>
      </template>
    </PageHeader>

    <LoadingBlock v-if="loading" />

    <template v-else-if="campaign">
      <CampaignEdit
        v-if="showEdit && isDraft"
        :campaign="campaign"
        @saved="onEdited"
        @close="showEdit = false"
      />

      <!-- Config summary -->
      <div class="card">
        <div class="card-body">
          <div class="metric-grid">
            <div>
              <div class="summary-label">Status</div>
              <StatusBadge :status="campaign.status" scope="campaign" />
            </div>
            <div>
              <div class="summary-label">Subject</div>
              <div class="fw-medium">{{ campaign.subject || '-' }}</div>
            </div>
            <div>
              <div class="summary-label">From</div>
              <div>{{ formatMailbox(campaign.from_email, campaign.from_name) }}</div>
            </div>
            <div>
              <div class="summary-label">Language</div>
              <div>{{ campaign.language || '-' }}</div>
            </div>
            <div>
              <div class="summary-label">Send Rate</div>
              <div>
                {{ campaign.send_rate > 0 ? `${campaign.send_rate} emails/min` : 'Unthrottled' }}
              </div>
            </div>
            <div>
              <div class="summary-label">Local Time Delivery</div>
              <div>{{ campaign.send_at_local_time ? 'Yes' : 'No' }}</div>
            </div>
            <div>
              <div class="summary-label">A/B Testing</div>
              <div>
                {{
                  campaign.ab_test_enabled ? `${campaign.ab_variants?.length ?? 0} variants` : 'Off'
                }}
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- Status timeline -->
      <div class="card">
        <div class="card-header">
          <h2>Timeline</h2>
        </div>
        <div class="card-body">
          <div class="metric-grid">
            <div>
              <div class="summary-label">Created</div>
              <div>{{ formatDate(campaign.created_at) }}</div>
            </div>
            <div>
              <div class="summary-label">Scheduled</div>
              <div>{{ formatDate(campaign.scheduled_at) }}</div>
            </div>
            <div>
              <div class="summary-label">Started</div>
              <div>{{ formatDate(campaign.started_at) }}</div>
            </div>
            <div>
              <div class="summary-label">Completed</div>
              <div>{{ formatDate(campaign.completed_at) }}</div>
            </div>
          </div>
        </div>
      </div>

      <CampaignStats
        :stats="stats"
        :by-variant="statsByVariant"
        :engagement="engagement"
        :links="trackedLinks"
      />

      <CampaignMessages :messages="messages" :loading="messagesLoading" />
    </template>

    <CampaignSchedule
      v-if="showScheduleModal"
      @schedule="scheduleCampaign"
      @close="showScheduleModal = false"
    />
  </div>
</template>

<style scoped>
.metric-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  gap: 16px;
}

.summary-label {
  font-size: 0.85em;
  color: var(--text-secondary);
  margin-bottom: 2px;
}

/* Field list for the sent-message modal. A local copy rather than a
   shared class: scoped styles do not cross components, and promoting
   it to the global sheet for one more caller is not worth another
   entry in the design system. */

/* Wider than the 520px default: this holds a rendered email, and a
   message laid out for a mail client does not survive being folded
   into a narrow column. */
.sent-modal {
  max-width: 760px;
}
</style>
