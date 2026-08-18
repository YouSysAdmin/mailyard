<script setup lang="ts">
// How close this project is to the ceiling its plan sets.
//
// Only rendered when a plan actually bounds something: a limit of 0 is
// unlimited, and a project on no plan at all has nothing to report. A
// card reading "0 / 0" would look like a project that may send nothing.
import { computed } from 'vue'
import type { UsageReport } from '../../api/plans'
import { formatNumber } from './formatNumber'

const props = defineProps<{ usage: UsageReport | null }>()

interface Window {
  label: string
  used: number
  limit: number
  pct: number
}

const windows = computed<Window[]>(() => {
  const u = props.usage
  if (!u?.plan) return []

  const rows: Window[] = []
  const add = (label: string, used: number, limit: number) => {
    if (limit > 0) {
      rows.push({ label, used, limit, pct: Math.min(100, Math.round((used / limit) * 100)) })
    }
  }
  add('This hour', u.usage.emails_last_hour, u.plan.hourly_email_limit ?? 0)
  add('Today', u.usage.emails_last_day, u.plan.daily_email_limit ?? 0)

  return rows
})

// Amber from 80%, red at the wall - the SAME thresholds the quota
// notification uses, so the colour on screen and the alert in the inbox
// cannot disagree.
function fill(pct: number): string {
  if (pct >= 100) return 'quota-bar-full'
  if (pct >= 80) return 'quota-bar-warn'

  return ''
}
</script>

<template>
  <div v-if="windows.length" class="card mb-4">
    <div class="card-header">
      <h2>Sending limit</h2>
      <span class="text-sm text-muted">{{ usage?.plan?.name }}</span>
    </div>

    <div class="card-body quota-rows">
      <div v-for="w in windows" :key="w.label">
        <div class="quota-head">
          <span class="quota-label">{{ w.label }}</span>
          <span class="quota-count">
            {{ formatNumber(w.used) }} / {{ formatNumber(w.limit) }}
            <span class="text-muted">({{ w.pct }}%)</span>
          </span>
        </div>

        <!-- Only the WIDTH is bound - a share of the window is data.
             Which colour it wears is a class, because that is design. -->
        <div class="quota-track">
          <div class="quota-bar" :class="fill(w.pct)" :style="{ width: w.pct + '%' }"></div>
        </div>

        <p v-if="w.pct >= 100" class="form-hint quota-hint">
          The window is full: further sends are refused until it rolls - 429 on the API, a temporary
          452 on SMTP submission, so a sending client retries rather than losing the message.
        </p>
      </div>
    </div>
  </div>
</template>

<style scoped>
.quota-rows {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.quota-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 6px;
}

.quota-label {
  font-size: 13px;
  color: var(--text-muted);
}

.quota-count {
  font-size: 13px;
  font-variant-numeric: tabular-nums;
}

.quota-track {
  height: 6px;
  border-radius: 3px;
  background: var(--bg-tertiary);
  overflow: hidden;
}

.quota-bar {
  height: 100%;
  background: var(--primary-500);
  transition: width 0.3s ease;
}

.quota-bar-warn {
  background: var(--warning-500);
}

.quota-bar-full {
  background: var(--danger-500);
}

.quota-hint {
  margin-top: 6px;
}
</style>
