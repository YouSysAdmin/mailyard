<script setup lang="ts">
// What happened to the messages a campaign produced.
//
// Three answers to one question, at three grains: the counts per status,
// then the same thing split by variant when the list was split, then
// which links people actually clicked. They belong together because a
// reader compares them - a variant that sent fewer and got more clicks
// is the whole point of running a split.
//
// The RATES are the server's. Dividing here would be a second answer to
// the same question, and the dashboard already reports the project-wide
// pair - two roundings and two opinions about the denominator is how
// those come to disagree.
import { computed } from 'vue'
import type { TrackedLink } from '../../api/campaigns'

const props = defineProps<{
  /** Message counts keyed by status. */
  stats: Record<string, number>
  /** The same, per variant name, empty when the list was not split. */
  byVariant: Record<string, Record<string, number>>
  engagement: {
    opened: number
    clicked: number
    sent: number
    open_rate: number
    click_rate: number
  }
  links: TrackedLink[]
}>()

const total = computed(() => Object.values(props.stats).reduce((sum, n) => sum + n, 0))

function stat(key: string): number {
  return props.stats[key] ?? 0
}

/** One status's share of the whole, as a CSS width. */
function share(key: string): string {
  if (total.value === 0) return '0%'

  return `${(stat(key) / total.value) * 100}%`
}

// One decimal, because a campaign to 1200 people moves the rate in
// tenths and a rounded integer would sit still for hours.
function pct(v: number): string {
  return `${v.toFixed(1)}%`
}

const variantNames = computed(() => Object.keys(props.byVariant).sort())

const VARIANT_COLUMNS = ['pending', 'queued', 'sent', 'failed', 'skipped']
</script>

<template>
  <div v-if="total > 0" class="card">
    <div class="card-header">
      <h2>Delivery Stats</h2>
    </div>

    <div class="card-body">
      <div class="flex gap-6 flex-wrap">
        <div class="stat-block">
          <div class="stat-value">{{ total }}</div>
          <div class="summary-label">Total</div>
        </div>
        <div class="stat-block">
          <div class="stat-value">{{ stat('pending') }}</div>
          <div class="summary-label">Pending</div>
        </div>
        <div class="stat-block">
          <div class="stat-value">{{ stat('queued') }}</div>
          <div class="summary-label">Queued</div>
        </div>
        <div class="stat-block">
          <div class="stat-value text-success">{{ stat('sent') }}</div>
          <div class="summary-label">Sent</div>
        </div>
        <div class="stat-block">
          <div class="stat-value text-danger">{{ stat('failed') }}</div>
          <div class="summary-label">Failed</div>
        </div>
        <div class="stat-block">
          <div class="stat-value">{{ stat('skipped') }}</div>
          <div class="summary-label">Skipped</div>
        </div>

        <!-- Out of SENT, not out of the audience: a recipient the
             campaign never delivered to was in no position to open
             anything, and counting them reports a delivery problem as an
             engagement problem. -->
        <div class="stat-block">
          <div class="stat-value text-accent">
            {{ engagement.opened }}
            <span v-if="engagement.sent" class="stat-rate">{{ pct(engagement.open_rate) }}</span>
          </div>
          <div class="summary-label">Opened</div>
        </div>
        <div class="stat-block">
          <div class="stat-value text-accent">
            {{ engagement.clicked }}
            <span v-if="engagement.sent" class="stat-rate">{{ pct(engagement.click_rate) }}</span>
          </div>
          <div class="summary-label">Clicked</div>
        </div>
      </div>

      <!-- Only the WIDTHS are bound. A colour in a style attribute is
           design in the markup, which is the one thing the stylesheet
           rule forbids - a share of the total is data, and which colour a
           status wears is not. -->
      <div class="mt-4">
        <div class="meter">
          <div class="meter-sent" :style="{ width: share('sent') }"></div>
          <div class="meter-failed" :style="{ width: share('failed') }"></div>
          <div class="meter-queued" :style="{ width: share('queued') }"></div>
        </div>
      </div>

      <div v-if="links.length > 0" class="mt-5">
        <h3 class="mb-2">Link Clicks</h3>
        <div class="table-wrapper">
          <table>
            <thead>
              <tr>
                <th>URL</th>
                <th class="text-right">Clicks</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="l in links" :key="l.id">
                <td class="link-url" :title="l.original_url">{{ l.original_url }}</td>
                <td class="text-right">{{ l.click_count }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <div v-if="variantNames.length > 0" class="mt-5">
        <h3 class="mb-2">By Variant</h3>
        <div class="table-wrapper">
          <table>
            <thead>
              <tr>
                <th>Variant</th>
                <th v-for="c in VARIANT_COLUMNS" :key="c" class="col-status">{{ c }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="name in variantNames" :key="name">
                <td class="fw-semibold">{{ name }}</td>
                <td v-for="c in VARIANT_COLUMNS" :key="c">{{ byVariant[name]?.[c] ?? 0 }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* A plain labelled number, NOT the .stat-card family: those are the
   dashboard's cards with a glyph and a border, and eight of them across
   one row is a wall. The global sheet sizes .stat-value only under a
   .stat-card, so these carry their own. */
.stat-block {
  text-align: center;
}

.stat-value {
  font-size: 1.5em;
  font-weight: 600;
}

.meter {
  width: 100%;
  height: 8px;
  background: var(--bg-tertiary);
  border-radius: 4px;
  overflow: hidden;
  display: flex;
}

/* The same three colours the counts above the bar wear, so a glance at
   the bar and a glance at the numbers agree. */
.meter-sent {
  background: var(--success-600);
}

.meter-failed {
  background: var(--danger-600);
}

.meter-queued {
  background: var(--primary-600);
}

/* A campaign link is a full URL with tracking on it. Truncated, with
   the whole thing in the title - the number beside it is the answer,
   and a wrapped URL would give one row six lines. */
.link-url {
  max-width: 480px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: var(--font-mono, monospace);
  font-size: 12px;
}

/* The five status columns hold counts, so the name above them is what
   sets their width. Capitalised in CSS rather than in five written-out
   headings, which is what they were. */
.col-status {
  text-transform: capitalize;
}

.summary-label {
  font-size: 0.85em;
  color: var(--text-secondary);
}

.stat-rate {
  margin-left: 6px;
  font-size: 0.7em;
  color: var(--text-secondary);
}
</style>
