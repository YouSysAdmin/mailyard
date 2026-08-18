<script setup lang="ts">
// The Refresh button and the Auto switch, for every page whose rows are
// written by something other than the person reading them.
//
// One component because it is one control: six pages had no way to see
// current data short of reloading the console, and six hand-written
// copies of a button plus a checkbox would differ in wording within a
// week. The behaviour lives in composables/useAutoRefresh - this is only
// what it looks like.
const props = defineProps<{
  refreshing?: boolean
  // Omit to render the button alone. A page polling one thing until it
  // settles has nothing for a person to switch off - the poll stops by
  // itself when the thing has settled.
  auto?: boolean
  paused?: boolean
  // Seconds between automatic refreshes, for the tooltip.
  //
  // Comes from useAutoRefresh, which is the one place the cadence is
  // decided - `everySeconds` is in its return value for that reason.
  // It was optional here with a `?? 10` fallback and NOTHING passed it,
  // so the tooltip claimed ten seconds on every page whatever the
  // interval, and one of them polls every three.
  everySeconds: number
}>()

const emit = defineEmits<{ refresh: []; 'update:auto': [value: boolean] }>()

const hint = () =>
  `Refreshes every ${props.everySeconds} seconds while this page is open and in front. ` +
  `A background tab does not poll.`
</script>

<template>
  <div class="refresh-control">
    <span
      v-if="auto && paused"
      class="text-sm text-muted"
      title="Older pages are left alone so a refresh cannot pull you back to the newest one."
    >
      Auto paused
    </span>
    <label v-if="auto !== undefined" class="refresh-auto" :title="hint()">
      <input
        type="checkbox"
        :checked="auto"
        @change="emit('update:auto', ($event.target as HTMLInputElement).checked)"
      />
      <span>Auto</span>
    </label>
    <button class="btn btn-secondary btn-sm" :disabled="refreshing" @click="emit('refresh')">
      {{ refreshing ? 'Refreshing...' : 'Refresh' }}
    </button>
  </div>
</template>

<style scoped>
.refresh-control {
  display: flex;
  align-items: center;
  gap: 10px;
}
.refresh-auto {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  cursor: pointer;
  white-space: nowrap;
}
</style>
