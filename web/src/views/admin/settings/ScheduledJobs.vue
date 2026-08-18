<script setup lang="ts">
// The cron jobs this binary registers, and what happened last time each
// ran.
//
// Its own card and its own file. It shared a page with the settings
// editor and nothing else - not a value, not a request, not a save - and
// the two only ever appeared together because both are things an
// operator looks at. One 507-line file for two unrelated screens.
//
// It owns its own fetch for the same reason. Running a job answers with
// the whole list, so this component never needs the page to reload it.
import { onMounted, ref } from 'vue'
import { settingsApi, type ScheduledJob } from '../../../api/settings'
import { apiErrorMessage } from '../../../api/client'
import { useNotificationStore } from '../../../stores/notification'
import { formatDate } from '../../../composables/formatDate'
import LoadingBlock from '../../../components/LoadingBlock.vue'
import EmptyState from '../../../components/EmptyState.vue'
import Notice from '../../../components/Notice.vue'

const notify = useNotificationStore()

const jobs = ref<ScheduledJob[]>([])
const loading = ref(true)
const running = ref('')

async function load() {
  loading.value = true
  try {
    jobs.value = (await settingsApi.jobs()).data.jobs ?? []
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load the scheduled jobs'))
  } finally {
    loading.value = false
  }
}

async function run(name: string) {
  running.value = name
  try {
    jobs.value = (await settingsApi.runJob(name)).data.jobs ?? []
    notify.success(`${name} finished`)
  } catch (e) {
    notify.error(apiErrorMessage(e, `${name} failed`))
    // The run failed, so the rows on screen are stale - but the notice
    // above is the useful signal, and a refresh that also fails should
    // not stack a second error on it.
    try {
      jobs.value = (await settingsApi.jobs()).data.jobs ?? []
    } catch {
      // Nothing to add.
    }
  } finally {
    running.value = ''
  }
}

onMounted(load)
</script>

<template>
  <div class="card">
    <div class="card-header">
      <h2>Scheduled Jobs</h2>
    </div>

    <LoadingBlock v-if="loading" />

    <EmptyState v-else-if="jobs.length === 0" text="No scheduled jobs are registered." />

    <template v-else>
      <div class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th>Job</th>
              <th>Schedule</th>
              <th>Last Run</th>
              <th>Next Run</th>
              <th>Status</th>
              <th class="text-right">Actions</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="j in jobs" :key="j.name">
              <td class="cell-title">{{ j.name }}</td>
              <td>
                <code>{{ j.schedule }}</code>
              </td>
              <td>
                {{ formatDate(j.last_run_at, 'Never') }}
                <span v-if="j.last_run_at" class="text-muted"> ({{ j.last_duration_ms }} ms)</span>
              </td>
              <td>{{ formatDate(j.next_run_at) }}</td>
              <td>
                <span v-if="j.running" class="badge badge-dot badge-info">Running</span>
                <!-- The message itself is in the banner below, because a
                     stack trace does not fit in a cell and a title
                     attribute is not something anybody finds. -->
                <span v-else-if="j.last_error" class="badge badge-dot badge-danger">Failed</span>
                <span v-else-if="j.last_run_at" class="badge badge-dot badge-success">OK</span>
                <span v-else class="badge badge-neutral">Not yet run</span>
              </td>
              <td class="text-right">
                <button
                  class="btn btn-secondary btn-sm"
                  :disabled="j.running || running === j.name"
                  @click="run(j.name)"
                >
                  {{ running === j.name ? 'Running...' : 'Run now' }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <!-- Every job that failed, with what it said. A job whose last run
           failed goes on being scheduled, so this is the only place the
           reason appears. -->
      <div v-if="jobs.some((j) => j.last_error)" class="card-body">
        <Notice
          v-for="j in jobs.filter((x) => x.last_error)"
          :key="j.name"
          kind="danger"
          :title="`${j.name} last failed`"
        >
          <p>{{ j.last_error }}</p>
        </Notice>
      </div>
    </template>
  </div>
</template>
