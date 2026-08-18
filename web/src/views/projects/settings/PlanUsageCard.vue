<script setup lang="ts">
// What this project has used, against what its plan allows.
//
// Read-only, but NOT open to any member: the figures come from GET
// /usage, which is gated on analytics:read like every other reporting
// number. A comment here once said otherwise and the card answered 403
// with a toast.
//
// The usage endpoint reports on the ACTIVE project - the header the api
// client injects - so on a settings page for some other project it would
// answer about the wrong one. It says to switch instead of showing
// somebody else's numbers under this project's name.
import { computed } from 'vue'
import type { UsageReport } from '../../../api/plans'
import FormField from '../../../components/FormField.vue'
import LoadingBlock from '../../../components/LoadingBlock.vue'
import FigureRows, { type Figure } from './FigureRows.vue'

const props = defineProps<{
  usage: UsageReport | null
  loading: boolean
  /** False when the page is showing a project that is not the active one. */
  isActiveProject: boolean
}>()

// A limit of 0 is "no limit", so the row shows the count alone rather
// than "12 / 0", which reads as a ceiling of nothing.
const rows = computed<Figure[]>(() => {
  const u = props.usage
  if (!u) return []

  const p = u.plan
  const pairs: [string, number, number][] = [
    ['Emails last hour', u.usage.emails_last_hour, p?.hourly_email_limit ?? 0],
    ['Emails last day', u.usage.emails_last_day, p?.daily_email_limit ?? 0],
    ['API keys', u.usage.api_keys, p?.max_api_keys ?? 0],
    ['SMTP servers', u.usage.smtp_servers, p?.max_smtp_servers ?? 0],
    ['Domains', u.usage.domains, p?.max_domains ?? 0],
    ['Subscribers', u.usage.subscribers, p?.max_subscribers ?? 0],
  ]

  return pairs.map(([label, current, limit]) => ({
    label,
    value: limit > 0 ? `${current} / ${limit}` : String(current),
  }))
})
</script>

<template>
  <div class="card">
    <div class="card-header">
      <h2>Plan and Usage</h2>
    </div>

    <div class="card-body">
      <p v-if="!isActiveProject" class="form-hint">
        Switch to this project to see its plan and current usage.
      </p>

      <LoadingBlock v-else-if="loading" />

      <template v-else-if="usage">
        <FormField label="Plan" :hint="usage.plan?.description">
          <div>
            <span class="fw-medium">{{ usage.plan?.name || 'Unlimited' }}</span>
            <span v-if="usage.plan?.is_default" class="badge badge-info">default</span>
          </div>
        </FormField>

        <FigureRows :rows="rows" />

        <p v-if="!usage.plan" class="form-hint mt-3">
          No plan is assigned and no default plan exists, so this project has no limits.
        </p>
      </template>
    </div>
  </div>
</template>
