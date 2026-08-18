<script setup lang="ts">
// The plans an installation sells, and who is on which.
//
// A plan is a set of CEILINGS, not a price - nothing here bills. Zero
// means unlimited everywhere, which is why a fresh install with no plans
// at all is a working install rather than a locked one.
import { onMounted, ref, useTemplateRef } from 'vue'
import { plansApi, type Plan } from '../../api/plans'
import { apiErrorMessage } from '../../api/client'
import { useNotificationStore } from '../../stores/notification'
import { useConfirm } from '../../composables/useConfirm'
import { formatDate } from '../../composables/formatDate'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import PageHeader from '../../components/PageHeader.vue'
import PlanForm from './PlanForm.vue'
import PlanAssignments from './PlanAssignments.vue'

const notify = useNotificationStore()
const { confirm } = useConfirm()

const plans = ref<Plan[]>([])
const loading = ref(true)

const showForm = ref(false)
const editing = ref<Plan | null>(null)

const assignments = useTemplateRef<InstanceType<typeof PlanAssignments>>('assignments')

async function load() {
  loading.value = true
  try {
    plans.value = (await plansApi.list()).data.plans ?? []
  } catch (err) {
    notify.error(apiErrorMessage(err, 'Failed to load plans'))
  } finally {
    loading.value = false
  }
}

function open(plan: Plan | null) {
  editing.value = plan
  showForm.value = true
}

async function saved() {
  showForm.value = false
  await load()
}

async function remove(p: Plan) {
  const ok = await confirm({
    title: 'Delete Plan',
    message: `Delete plan "${p.name}"? Projects using it will fall back to the default plan.`,
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!ok) return

  try {
    await plansApi.remove(p.id)
    notify.success('Plan deleted')
    // Both lists moved: every project that was on it is now on the
    // default, and the card below is showing the plan that just went.
    await Promise.all([load(), assignments.value?.reload()])
  } catch (err) {
    notify.error(apiErrorMessage(err, 'Failed to delete plan'))
  }
}

/** Zero is unlimited on every limit, so it is never shown as a number. */
function ceiling(n: number): string {
  return n > 0 ? String(n) : 'unlimited'
}

function summary(p: Plan): string {
  return (
    `${ceiling(p.hourly_email_limit)}/hr, ${ceiling(p.daily_email_limit)}/day emails - ` +
    `${ceiling(p.max_api_keys)} keys, ${ceiling(p.max_smtp_servers)} smtp, ` +
    `${ceiling(p.max_domains)} domains, ${ceiling(p.max_subscribers)} subscribers`
  )
}

onMounted(load)
</script>

<template>
  <div>
    <PageHeader title="Plans">
      <button class="btn btn-primary" @click="open(null)">Create Plan</button>
    </PageHeader>

    <div class="card">
      <div class="card-header">
        <h2>Usage Plans</h2>
      </div>

      <LoadingBlock v-if="loading" />

      <EmptyState
        v-else-if="plans.length === 0"
        title="No plans yet"
        text="Without plans every project is unlimited. Create one to set limits."
      />

      <div v-else class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th>Name</th>
              <th>Limits</th>
              <th>Created</th>
              <th class="text-right">Actions</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="p in plans" :key="p.id">
              <td>
                <div class="fw-medium">
                  {{ p.name }}
                  <span v-if="p.is_default" class="badge badge-info">default</span>
                </div>
                <div v-if="p.description" class="plan-description">{{ p.description }}</div>
              </td>
              <td class="plan-limits">{{ summary(p) }}</td>
              <td>{{ formatDate(p.created_at) }}</td>
              <td>
                <div class="table-actions justify-end">
                  <button class="btn btn-secondary btn-sm" @click="open(p)">Edit</button>
                  <button class="btn btn-danger btn-sm" @click="remove(p)">Delete</button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <PlanAssignments ref="assignments" :plans="plans" />

    <PlanForm v-if="showForm" :plan="editing" @saved="saved" @close="showForm = false" />
  </div>
</template>

<style scoped>
.plan-description {
  font-size: 12px;
  color: var(--text-muted);
  margin-top: 2px;
}

.plan-limits {
  font-size: 13px;
  color: var(--text-secondary);
}
</style>
