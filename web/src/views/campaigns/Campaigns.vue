<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { apiErrorMessage } from '../../api/client'
import { campaignsApi } from '../../api/campaigns'
import type { Campaign, CampaignStatus, CampaignVariant } from '../../api/types'
import Pagination from '../../components/Pagination.vue'
import { useClientPager } from '../../composables/usePagination'
import { useNotificationStore } from '../../stores/notification'
import { useCampaignActions } from '../../composables/campaignActions'
import { useProjectStore } from '../../stores/project'
import { formatDate } from '../../composables/formatDate'
import { useAutoRefresh } from '../../composables/useAutoRefresh'
import RefreshControl from '../../components/RefreshControl.vue'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import PageHeader from '../../components/PageHeader.vue'
import BaseModal from '../../components/BaseModal.vue'
import CampaignFields from './CampaignFields.vue'
import CampaignSchedule from './CampaignSchedule.vue'
import { blankDraft, toPayload } from './campaignDraft'
import { useFieldErrors } from '../../composables/fieldErrors'

const router = useRouter()
const notify = useNotificationStore()
const projStore = useProjectStore()

const campaigns = ref<Campaign[]>([])
const loading = ref(true)
const statusFilter = ref<CampaignStatus | ''>('')
// Every act a campaign offers, shared with the detail page - which
// carries the same six, and used to word three of the confirmations a
// second time.
const {
  busy: actionBusy,
  send,
  schedule,
  pause,
  resume,
  cancel,
  duplicate,
  remove,
} = useCampaignActions(load)

const statusTabs: { label: string; value: CampaignStatus | '' }[] = [
  { label: 'All', value: '' },
  { label: 'Draft', value: 'draft' },
  { label: 'Scheduled', value: 'scheduled' },
  { label: 'Sending', value: 'sending' },
  { label: 'Paused', value: 'paused' },
  { label: 'Sent', value: 'sent' },
  { label: 'Cancelled', value: 'cancelled' },
]

const filtered = computed(() =>
  statusFilter.value
    ? campaigns.value.filter((c) => c.status === statusFilter.value)
    : campaigns.value,
)

const { pageable, pageItems, goToPage } = useClientPager(filtered, 20)
watch(statusFilter, () => goToPage(0))

// Create modal
const showModal = ref(false)
const saving = ref(false)
const form = ref(blankDraft())
const ready = ref(false)
const formVariants = ref<CampaignVariant[]>([])

// Pickers
const { errors: fieldErrors, capture, clear } = useFieldErrors()

const scheduleTarget = ref<Campaign | null>(null)

async function load(quiet = false) {
  if (!quiet) loading.value = true
  try {
    const res = await campaignsApi.list()
    campaigns.value = res.data.campaigns ?? []
  } catch (e) {
    if (!quiet) notify.error(apiErrorMessage(e, 'Failed to load campaigns'))
  } finally {
    if (!quiet) loading.value = false
  }
}

function openCreate() {
  clear()
  form.value = blankDraft()
  formVariants.value = []
  showModal.value = true
}

async function saveCampaign() {
  if (!ready.value) return

  clear()
  saving.value = true
  try {
    await campaignsApi.create(toPayload(form.value, formVariants.value))
    notify.success('Campaign created')
    showModal.value = false
    await load()
  } catch (e) {
    // A parse failure is this form's, not the server's, and saying
    // "failed to create" for it sends the reader looking in the wrong
    // place.
    if (e instanceof SyntaxError) notify.error('The template data is not valid JSON')
    else if (!capture(e)) notify.error(apiErrorMessage(e, 'Failed to create the campaign'))
  } finally {
    saving.value = false
  }
}

function openSchedule(campaign: Campaign) {
  scheduleTarget.value = campaign
}

async function scheduleCampaign(at: string) {
  const target = scheduleTarget.value
  if (!target) return

  scheduleTarget.value = null
  await schedule(target, at)
}

function canSend(c: Campaign): boolean {
  return c.status === 'draft' || c.status === 'scheduled'
}

function canSchedule(c: Campaign): boolean {
  return c.status === 'draft' || c.status === 'scheduled'
}

function canPause(c: Campaign): boolean {
  return c.status === 'sending'
}

function canResume(c: Campaign): boolean {
  return c.status === 'paused'
}

function canCancel(c: Campaign): boolean {
  return ['scheduled', 'sending', 'paused'].includes(c.status)
}

function canDelete(c: Campaign): boolean {
  return c.status === 'draft' || c.status === 'cancelled'
}

// A sending campaign moves without anybody touching this page, and its
// counts are what somebody watching it came to see.
const { refreshing, refresh, auto, paused, everySeconds } = useAutoRefresh(() => load(true), {
  storageKey: 'mailyard.autorefresh.campaigns',
})

onMounted(() => {
  load()
})
</script>

<template>
  <div>
    <PageHeader title="Campaigns">
      <RefreshControl
        :every-seconds="everySeconds"
        :refreshing="refreshing"
        :auto="auto"
        :paused="paused"
        @refresh="refresh"
        @update:auto="auto = $event"
      />
      <button v-if="projStore.can('campaigns:write')" class="btn btn-primary" @click="openCreate">
        Create Campaign
      </button>
    </PageHeader>

    <!-- Status filter tabs -->
    <div class="tabs">
      <button
        v-for="tab in statusTabs"
        :key="tab.value"
        class="tab"
        :class="{ active: statusFilter === tab.value }"
        @click="statusFilter = tab.value"
      >
        {{ tab.label }}
      </button>
    </div>

    <LoadingBlock v-if="loading" />

    <div v-else class="card">
      <EmptyState v-if="filtered.length === 0" title="No Campaigns">
        <p v-if="statusFilter">No campaigns with this status.</p>
        <p v-else>Create a campaign to start sending emails to your subscribers.</p>
      </EmptyState>

      <template v-else>
        <div class="table-wrapper">
          <table>
            <thead>
              <tr>
                <th>Name</th>
                <th>Status</th>
                <th>Scheduled</th>
                <th>Created</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="campaign in pageItems" :key="campaign.id">
                <td>
                  <a class="link" @click="router.push(`/campaigns/${campaign.id}`)">
                    {{ campaign.name }}
                  </a>
                  <div v-if="campaign.subject" class="text-muted text-sm">
                    {{ campaign.subject }}
                  </div>
                </td>
                <td>
                  <StatusBadge :status="campaign.status" scope="campaign" />
                </td>
                <td>{{ formatDate(campaign.scheduled_at) }}</td>
                <td>{{ formatDate(campaign.created_at) }}</td>
                <td>
                  <div class="flex gap-1 flex-wrap">
                    <button
                      class="btn btn-secondary btn-sm"
                      @click="router.push(`/campaigns/${campaign.id}`)"
                    >
                      View
                    </button>
                    <template v-if="projStore.can('campaigns:write')">
                      <button
                        v-if="canSend(campaign)"
                        class="btn btn-primary btn-sm"
                        :disabled="actionBusy"
                        @click="send(campaign)"
                      >
                        Send Now
                      </button>
                      <button
                        v-if="canSchedule(campaign)"
                        class="btn btn-secondary btn-sm"
                        :disabled="actionBusy"
                        @click="openSchedule(campaign)"
                      >
                        Schedule
                      </button>
                      <button
                        v-if="canPause(campaign)"
                        class="btn btn-warning btn-sm"
                        :disabled="actionBusy"
                        @click="pause(campaign)"
                      >
                        Pause
                      </button>
                      <button
                        v-if="canResume(campaign)"
                        class="btn btn-primary btn-sm"
                        :disabled="actionBusy"
                        @click="resume(campaign)"
                      >
                        Resume
                      </button>
                      <button
                        v-if="canCancel(campaign)"
                        class="btn btn-danger btn-sm"
                        :disabled="actionBusy"
                        @click="cancel(campaign)"
                      >
                        Cancel
                      </button>
                      <button
                        class="btn btn-secondary btn-sm"
                        :disabled="actionBusy"
                        @click="duplicate(campaign)"
                      >
                        Duplicate
                      </button>
                    </template>
                    <button
                      v-if="projStore.can('campaigns:delete') && canDelete(campaign)"
                      class="btn btn-danger btn-sm"
                      :disabled="actionBusy"
                      @click="remove(campaign)"
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

    <!-- Create Modal -->
    <BaseModal
      v-if="showModal"
      title="Create Campaign"
      size="modal-w720"
      @close="showModal = false"
    >
      <CampaignFields
        v-model="form"
        v-model:variants="formVariants"
        :errors="fieldErrors"
        @update:ready="ready = $event"
      />

      <template #footer>
        <button class="btn btn-secondary" @click="showModal = false">Cancel</button>
        <button class="btn btn-primary" :disabled="saving || !ready" @click="saveCampaign">
          {{ saving ? 'Creating...' : 'Create' }}
        </button>
      </template>
    </BaseModal>

    <CampaignSchedule
      v-if="scheduleTarget"
      @schedule="scheduleCampaign"
      @close="scheduleTarget = null"
    />
  </div>
</template>

<style scoped>
.col-count {
  width: 80px;
}

.link {
  color: var(--primary-600);
  cursor: pointer;
  font-weight: 500;
}

.link:hover {
  text-decoration: underline;
}
</style>
