<script setup lang="ts">
// Which plan each project is on.
//
// The select writes immediately - there is no Save, because one row is
// one decision and a page of thirty selects with a single button at the
// bottom hides which of them changed.
import { onMounted, ref } from 'vue'
import { plansApi, type Plan } from '../../api/plans'
import { projectApi } from '../../api/projects'
import { apiErrorMessage } from '../../api/client'
import type { Project } from '../../api/types'
import { useNotificationStore } from '../../stores/notification'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'

defineProps<{ plans: Plan[] }>()

const notify = useNotificationStore()

const projects = ref<Project[]>([])
const loading = ref(true)
const assigning = ref('')

async function load() {
  loading.value = true
  try {
    projects.value = (await projectApi.list()).data.projects ?? []
  } catch (err) {
    notify.error(apiErrorMessage(err, 'Failed to load projects'))
  } finally {
    loading.value = false
  }
}

async function assign(proj: Project, event: Event) {
  const planID = (event.target as HTMLSelectElement).value
  assigning.value = proj.id
  try {
    const updated = (await plansApi.assign(proj.id, planID)).data.project
    const i = projects.value.findIndex((p) => p.id === updated.id)
    // Merged rather than replaced: the list endpoint attaches the
    // caller's role to each row and the assign response does not.
    if (i !== -1) projects.value[i] = { ...projects.value[i], ...updated }
    notify.success(planID ? 'Plan assigned' : 'Plan cleared')
  } catch (err) {
    // Told plainly, not placed in a field: there is no form here, so a
    // refusal attributed to `plan_id` would land nowhere and the select
    // would simply spring back with no explanation.
    notify.error(apiErrorMessage(err, 'Failed to assign the plan'))
    await load()
  } finally {
    assigning.value = ''
  }
}

onMounted(load)

// The page reloads this after deleting a plan, since every project that
// was on it has just fallen back to the default.
defineExpose({ reload: load })
</script>

<template>
  <div class="card">
    <div class="card-header">
      <h2>Project Assignments</h2>
    </div>

    <LoadingBlock v-if="loading" />

    <EmptyState
      v-else-if="projects.length === 0"
      title="No projects"
      text="There are no projects to assign plans to."
    />

    <div v-else class="table-wrapper">
      <table>
        <thead>
          <tr>
            <th>Project</th>
            <th class="col-plan">Plan</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="proj in projects" :key="proj.id">
            <td class="fw-medium">{{ proj.name }}</td>
            <td>
              <select
                class="form-select"
                :value="proj.plan_id || ''"
                :disabled="assigning === proj.id"
                @change="assign(proj, $event)"
              >
                <option value="">Default plan</option>
                <option v-for="p in plans" :key="p.id" :value="p.id">{{ p.name }}</option>
              </select>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <div class="card-body pt-0">
      <p class="form-hint">
        Projects without an explicit plan use the default plan. If no default plan exists they are
        unlimited.
      </p>
    </div>
  </div>
</template>

<style scoped>
.col-plan {
  width: 260px;
}
</style>
