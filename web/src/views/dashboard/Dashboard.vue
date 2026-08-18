<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { emailsApi } from '../../api/emails'
import { campaignsApi } from '../../api/campaigns'
import { analyticsApi, type DayCount, type Engagement } from '../../api/analytics'
import { useProjectStore } from '../../stores/project'
import type { Email, Campaign } from '../../api/types'
import StatCard from '../../components/StatCard.vue'
import SendingLimitCard from './SendingLimitCard.vue'
import VolumeChart from './VolumeChart.vue'
import CampaignSummary from './CampaignSummary.vue'
import RecentEmails from './RecentEmails.vue'
import { formatNumber } from './formatNumber'
import { useAutoRefresh } from '../../composables/useAutoRefresh'
import RefreshControl from '../../components/RefreshControl.vue'
import { plansApi, type UsageReport } from '../../api/plans'
import { apiErrorMessage } from '../../api/client'
import { useNotificationStore } from '../../stores/notification'
import LoadingBlock from '../../components/LoadingBlock.vue'
import PageHeader from '../../components/PageHeader.vue'

const router = useRouter()
const projStore = useProjectStore()
const notify = useNotificationStore()
const loading = ref(true)
const counts = ref<Record<string, number>>({})
const recentEmails = ref<Email[]>([])
const campaigns = ref<Campaign[]>([])

// Send volume, last 14 days. Two aligned series - the backend fills
// empty days, so both arrays cover every day in the range.
const sentSeries = ref<DayCount[]>([])
const failedSeries = ref<DayCount[]>([])

function daysAgo(n: number): string {
  const d = new Date()
  d.setDate(d.getDate() - n)
  return d.toISOString().slice(0, 10)
}

// Opens and clicks were recorded from the day tracking shipped and
// displayed nowhere: a project with tracking on had a pixel in every
// message and no page that admitted it had ever fired.
function blankEngagement(): Engagement {
  return { tracked_sent: 0, opened: 0, clicked: 0, open_rate: 0, click_rate: 0 }
}
const engagement = ref<Engagement>(blankEngagement())
const usage = ref<UsageReport | null>(null)

// THE REST OF A PAYLOAD THIS PAGE WAS ALREADY PAYING FOR.
//
// /dashboard/stats has always answered with failure_rate, the inbound
// counts and fifteen resource counts, and this page read `engagement`
// and dropped the rest on the floor. So the three cards below cost no
// query at all - they are the difference between using the response
// and using a field of it.
//
// Not gated on bounces:read or domains:read, deliberately. The server
// put every one of these behind analytics:read in one endpoint, so
// that IS the rule, and a console that hid what the API hands it would
// be inventing a second one.
const failureRate = ref(0)
const resources = ref<Record<string, number>>({})

function resource(key: string): number {
  return resources.value[key] ?? 0
}

// This page is reachable with analytics:read alone (see landingFor), and
// two of its calls need a different resource: the recent list is
// emails:read, the campaign table is campaigns:read. Asking for them
// regardless meant one 403 rejected the whole Promise.all and the page
// rendered zeroes everywhere - reading as "this project has never sent
// anything" rather than "you cannot see this part".
//
// So each section is asked for only when the caller may have it, and the
// template already hides what it has no data for.
//
// The status counts are not one of those calls. /emails/stats and
// /dashboard/stats run the same `SELECT status, COUNT(*) ... GROUP BY
// status`, so asking for both would aggregate the table that grows per
// message twice on every load of the page people leave open. We read
// them from the payload that arrives anyway.
//
// That also settles what a caller with analytics:read but not
// emails:read sees. The server sends these counts under analytics:read,
// so they are shown under analytics:read rather than coming back empty
// and rendering a row of zeroes.
async function load(quiet = false) {
  if (!quiet) loading.value = true
  try {
    const from = daysAgo(13)
    const canEmails = projStore.can('emails:read')
    const canCampaigns = projStore.can('campaigns:read')
    const [emailsRes, campaignsRes, sentRes, failedRes, dashRes, usageRes] = await Promise.all([
      canEmails ? emailsApi.list({ limit: 10 }) : null,
      canCampaigns ? campaignsApi.list() : null,
      analyticsApi.trend({ from, status: 'sent' }),
      analyticsApi.trend({ from, status: 'failed' }),
      // The engagement figures. Server-side, because the same two rates
      // appear on a campaign and two places doing the division is two
      // places to disagree about the denominator.
      analyticsApi.dashboard(),
      // The plan and what is left of it. Same permission as everything
      // else here (analytics:read), and the reason it belongs on this
      // page rather than only in project settings: hitting a limit
      // refuses sends, and the number that says how close you are was
      // two clicks away on a page nobody opens daily.
      plansApi.usage(),
    ])
    usage.value = usageRes.data
    const stats = dashRes.data.stats
    counts.value = stats?.emails ?? {}
    engagement.value = stats?.engagement ?? blankEngagement()
    failureRate.value = stats?.failure_rate ?? 0
    resources.value = stats?.resources ?? {}
    recentEmails.value = emailsRes?.data.emails ?? []
    campaigns.value = campaignsRes?.data.campaigns ?? []
    sentSeries.value = sentRes.data.daily_counts ?? []
    failedSeries.value = failedRes.data.daily_counts ?? []
  } catch (e) {
    // A failed manual load says so, like every other page. Left to the
    // browser console, a dashboard of zeroes is indistinguishable from
    // an empty project. The automatic path stays quiet on purpose: the
    // numbers on screen are still the last good answer, and a toast
    // every tick is worse than stale data.
    if (!quiet) notify.error(apiErrorMessage(e, 'Failed to load the dashboard'))
  } finally {
    if (!quiet) loading.value = false
  }
}

// Offered but off by default here, alone among these pages: one refresh
// is six requests, one of them an aggregation over the table that grows
// per message. It was two of those until the status counts stopped being
// fetched twice - the conclusion is unchanged, since a dashboard is read
// for a minute and left where a log is watched, so the cost is still real
// and the benefit still is not.
const { refreshing, refresh, auto, paused, everySeconds } = useAutoRefresh(() => load(true), {
  storageKey: 'mailyard.autorefresh.dashboard',
  autoDefault: false,
})

onMounted(() => load())
// Reload when the active project changes.
watch(
  () => projStore.currentProjectId,
  () => load(),
)

const count = (s: string) => counts.value[s] ?? 0
const totalEmails = computed(() => Object.values(counts.value).reduce((sum, n) => sum + n, 0))
const inQueue = computed(
  () => count('pending') + count('queued') + count('processing') + count('scheduled'),
)
// The denominator of failure_rate, derived from the same map the server
// divided - so the percentage and the sentence under it cannot disagree.
// Queued and scheduled mail is excluded on purpose: counting mail that
// has not been attempted as a success flatters the number and as a
// failure slanders it.
const finalized = computed(() => count('sent') + count('failed'))
const deliveryRate = computed(() => {
  if (totalEmails.value === 0) return 0
  return (count('sent') / totalEmails.value) * 100
})

/**
 * The row of numbers, as data.
 *
 * Several are RATES, and a rate with no denominator is not zero - it is
 * unmeasured. Those pass '-' and say why in the subtitle, which is the
 * same reasoning throughout: no tracked sends is not a 0% open rate, no
 * finalized mail is not a 0% failure rate, and no domains is not
 * "0 of 0 verified".
 */
const statCards = computed(() => [
  { label: 'Total emails', icon: 'total', value: formatNumber(totalEmails.value) },
  {
    label: 'Sent',
    icon: 'sent',
    value: formatNumber(count('sent')),
    sub: `${deliveryRate.value.toFixed(1)}% of all emails`,
  },
  {
    label: 'In queue',
    icon: 'queued',
    value: formatNumber(inQueue.value),
    sub: 'pending, queued, processing and scheduled',
  },
  { label: 'Failed', icon: 'failed', value: formatNumber(count('failed')) },
  { label: 'Suppressed', icon: 'suppressed', value: formatNumber(count('suppressed')) },
  {
    label: 'Open rate',
    icon: 'opened',
    value: engagement.value.tracked_sent ? `${engagement.value.open_rate.toFixed(1)}%` : '-',
    sub: engagement.value.tracked_sent
      ? `${formatNumber(engagement.value.opened)} of ${formatNumber(engagement.value.tracked_sent)} tracked sends`
      : 'no tracked sends yet',
  },
  {
    label: 'Click rate',
    icon: 'clicked',
    value: engagement.value.tracked_sent ? `${engagement.value.click_rate.toFixed(1)}%` : '-',
    sub: engagement.value.tracked_sent
      ? `${formatNumber(engagement.value.clicked)} of ${formatNumber(engagement.value.tracked_sent)} tracked sends`
      : 'no tracked sends yet',
  },
  {
    label: 'Failure rate',
    icon: 'failure',
    value: finalized.value ? `${failureRate.value.toFixed(1)}%` : '-',
    sub: finalized.value
      ? `of ${formatNumber(finalized.value)} sent and failed`
      : 'nothing finalized yet',
  },
  {
    label: 'Bounces',
    icon: 'bounced',
    value: formatNumber(resource('bounces')),
    // History, not the block. A bounced address is two records and only
    // the suppression stops mail, so this going up does not mean
    // anything is being refused.
    sub: 'reports recorded',
  },
  {
    label: 'Domains',
    icon: 'domains',
    value: resource('domains') ? `${resource('verified_domains')} / ${resource('domains')}` : '-',
    // The gate that refuses a send, which is why it is here and not only
    // on the domains page: an unverified From domain is a 400 at
    // submission time.
    sub: resource('domains') ? 'verified for sending' : 'no domains added yet',
  },
])
</script>

<template>
  <div>
    <PageHeader title="Dashboard">
      <RefreshControl
        :every-seconds="everySeconds"
        :refreshing="refreshing"
        :auto="auto"
        :paused="paused"
        @refresh="refresh"
        @update:auto="auto = $event"
      />
      <template v-if="projStore.can('emails:write')">
        <button class="btn btn-primary" @click="router.push('/emails/send')">Send email</button>
        <button class="btn btn-secondary" @click="router.push('/campaigns')">New campaign</button>
      </template>
    </PageHeader>

    <LoadingBlock v-if="loading" />

    <template v-else>
      <SendingLimitCard :usage="usage" />

      <!-- Every one of these is already in the /dashboard/stats payload
           this page was fetching anyway, so the row costs no query. -->
      <div class="stats-grid">
        <StatCard v-for="s in statCards" :key="s.label" v-bind="s" />
      </div>

      <VolumeChart :sent="sentSeries" :failed="failedSeries" />

      <CampaignSummary :campaigns="campaigns" />

      <RecentEmails :emails="recentEmails" />
    </template>
  </div>
</template>
