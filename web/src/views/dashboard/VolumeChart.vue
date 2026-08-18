<script setup lang="ts">
// Sent and failed per day, for the last fortnight.
//
// A stacked bar per day rather than two lines: the question a person
// opens this for is "is anything failing", and failure beside its own
// day's volume answers that where a separate line does not.
import { computed } from 'vue'
import type { DayCount } from '../../api/analytics'
import { formatNumber } from './formatNumber'

const props = defineProps<{
  sent: DayCount[]
  failed: DayCount[]
}>()

/** One row per day with both series merged, oldest first. */
const days = computed(() => {
  const failedByDay = new Map(props.failed.map((d) => [d.date, d.count]))

  return props.sent.map((d) => ({
    date: d.date,
    sent: d.count,
    failed: failedByDay.get(d.date) ?? 0,
  }))
})

const total = computed(() => days.value.reduce((sum, d) => sum + d.sent + d.failed, 0))

// The tallest stacked bar sets the scale. Minimum 1 so an empty
// fortnight divides by one instead of zero.
const max = computed(() => Math.max(1, ...days.value.map((d) => d.sent + d.failed)))

function barHeight(value: number): string {
  const pct = (value / max.value) * 100

  // A non-zero day always gets a visible sliver: one failure in a
  // fortnight of thousands still has to be seen.
  return value > 0 ? `${Math.max(pct, 2)}%` : '0'
}

// toLocaleDateString and not the shared formatter: this is a weekday
// name with no time in it, which is the one date the console's clock
// has nothing to say about.
function weekdayOf(date: string): string {
  return new Date(date + 'T00:00:00').toLocaleDateString(undefined, { weekday: 'short' })
}
</script>

<template>
  <div class="card">
    <div class="card-header">
      <h2>
        Send Volume
        <span class="volume-subtitle">Last 14 days - {{ formatNumber(total) }} emails</span>
      </h2>
    </div>

    <div class="volume-chart">
      <!-- Only the HEIGHTS are bound: a share of the tallest day is
           data. The two colours are classes. -->
      <div class="volume-chart-bars">
        <div
          v-for="day in days"
          :key="day.date"
          class="volume-bar-group"
          :title="`${day.date}: ${day.sent} sent, ${day.failed} failed`"
        >
          <div class="volume-bar-stack">
            <div
              class="volume-bar volume-bar-failed"
              :style="{ height: barHeight(day.failed) }"
            ></div>
            <div class="volume-bar volume-bar-sent" :style="{ height: barHeight(day.sent) }"></div>
          </div>
          <div class="volume-bar-label">{{ weekdayOf(day.date) }}</div>
        </div>
      </div>

      <div class="volume-chart-legend">
        <span class="volume-legend-item">
          <span class="volume-legend-dot volume-legend-sent"></span> Sent
        </span>
        <span class="volume-legend-item">
          <span class="volume-legend-dot volume-legend-failed"></span> Failed
        </span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.volume-subtitle {
  font-weight: 400;
  font-size: 13px;
  color: var(--text-muted);
  margin-left: 8px;
}

.volume-chart {
  padding: 20px 24px 16px;
}

.volume-chart-bars {
  display: flex;
  align-items: flex-end;
  gap: 8px;
  height: 160px;
}

.volume-bar-group {
  flex: 1;
  display: flex;
  flex-direction: column;
  height: 100%;
  min-width: 0;
}

.volume-bar-stack {
  flex: 1;
  display: flex;
  flex-direction: column;
  justify-content: flex-end;
}

.volume-bar {
  width: 100%;
  border-radius: 2px 2px 0 0;
}

.volume-bar-sent {
  background: var(--success-500);
}

.volume-bar-failed {
  background: var(--danger-500);
}

.volume-bar-label {
  margin-top: 6px;
  font-size: 10px;
  color: var(--text-muted);
  text-align: center;
  white-space: nowrap;
  overflow: hidden;
}

.volume-chart-legend {
  display: flex;
  gap: 16px;
  margin-top: 12px;
  font-size: 12px;
  color: var(--text-secondary);
}

.volume-legend-item {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.volume-legend-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
}

.volume-legend-sent {
  background: var(--success-500);
}

.volume-legend-dot.volume-legend-failed {
  background: var(--danger-500);
}
</style>
