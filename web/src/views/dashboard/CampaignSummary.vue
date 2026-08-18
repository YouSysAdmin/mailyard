<script setup lang="ts">
// How many campaigns this project has, and what state they are in.
//
// Counted from the list the dashboard already loads rather than by a
// query of its own - and the statuses are a FIXED list, so a state with
// none of them still shows a zero. Deriving the columns from the data
// would make the row change shape as campaigns move.
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import type { Campaign } from '../../api/types'
import EmptyState from '../../components/EmptyState.vue'
import { formatNumber } from './formatNumber'

const props = defineProps<{ campaigns: Campaign[] }>()

const router = useRouter()

const STATUSES = ['draft', 'scheduled', 'sending', 'paused', 'sent', 'cancelled']

const counts = computed(() => {
  const acc: Record<string, number> = {}
  for (const c of props.campaigns) {
    acc[c.status] = (acc[c.status] ?? 0) + 1
  }

  return acc
})
</script>

<template>
  <div class="card">
    <div class="card-header">
      <h2>Campaigns</h2>
      <button class="btn btn-secondary btn-sm" @click="router.push('/campaigns')">View All</button>
    </div>

    <EmptyState
      v-if="campaigns.length === 0"
      title="No campaigns yet"
      text="Create a campaign to send templated emails to a subscriber list."
    />

    <div v-else class="campaign-counts">
      <div class="campaign-count">
        <div class="campaign-count-value">{{ formatNumber(campaigns.length) }}</div>
        <div class="campaign-count-label">Total</div>
      </div>
      <div v-for="s in STATUSES" :key="s" class="campaign-count">
        <div class="campaign-count-value">{{ formatNumber(counts[s] ?? 0) }}</div>
        <div class="campaign-count-label">{{ s }}</div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.campaign-counts {
  display: flex;
  flex-wrap: wrap;
  gap: 24px;
  padding: 20px 24px;
}

.campaign-count {
  min-width: 80px;
  text-align: center;
}

.campaign-count-value {
  font-size: 22px;
  font-weight: 700;
  color: var(--text-primary);
}

/* Capitalised in CSS rather than in six written-out labels, so the list
   above stays the status vocabulary the server uses. */
.campaign-count-label {
  font-size: 12px;
  color: var(--text-muted);
  margin-top: 2px;
  text-transform: capitalize;
}
</style>
