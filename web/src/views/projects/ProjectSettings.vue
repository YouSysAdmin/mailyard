<script setup lang="ts">
// One project's settings, as five cards that share almost nothing.
//
// Each answers a different question and each is gated differently - the
// form on settings:write, the default role on members:write, the usage
// figures on analytics:read, the export on nothing at all, and deleting
// on OWNERSHIP, which is not a permission. They were one file, and the
// only thing that file really shared between them was the project.
//
// PERMISSIONS COME FROM THIS PAGE'S OWN REQUEST, not the project store:
// the path id may name a project other than the store's current one, so
// the store's can() would answer for the wrong one.
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { projectApi } from '../../api/projects'
import { plansApi, type UsageReport } from '../../api/plans'
import { apiErrorMessage } from '../../api/client'
import type { Project, ProjectRole } from '../../api/types'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import LoadingBlock from '../../components/LoadingBlock.vue'
import PageHeader from '../../components/PageHeader.vue'
import SettingsForm from './settings/SettingsForm.vue'
import DefaultRoleCard from './settings/DefaultRoleCard.vue'
import PlanUsageCard from './settings/PlanUsageCard.vue'
import ExportCard from './settings/ExportCard.vue'
import DangerZone from './settings/DangerZone.vue'

const route = useRoute()
const router = useRouter()
const notify = useNotificationStore()
const projStore = useProjectStore()

const projId = route.params.id as string
const proj = ref<Project | null>(null)
const iAmOwner = ref(false)
const perms = ref<string[]>([])
const roles = ref<ProjectRole[]>([])
const loading = ref(true)

const usage = ref<UsageReport | null>(null)
const usageLoading = ref(false)

function can(p: string): boolean {
  return perms.value.includes('*') || perms.value.includes(p)
}

const canEditSettings = computed(() => can('settings:write'))
// Choosing the default role is member policy, so it follows the members
// routes rather than settings.
const canManageMembers = computed(() => can('members:write'))

// The usage endpoint reports on the ACTIVE project, through the header
// the api client injects - so two of the cards below can only speak for
// the project the console is currently on.
const isActiveProject = computed(() => projStore.currentProjectId === projId)

// The plan's ceiling on the sandbox window, which the settings form
// needs for its own field. Read from the usage report this page already
// loads rather than by a request of its own.
const sandboxCeiling = computed(() => usage.value?.plan?.max_sandbox_retention_days ?? 0)

async function fetchProject() {
  loading.value = true
  try {
    const res = await projectApi.get(projId)
    proj.value = res.data.project
    iAmOwner.value = res.data.owner === true
    perms.value = res.data.permissions ?? []
  } catch (err) {
    notify.error(apiErrorMessage(err, 'Failed to load project'))
    router.push('/projects')
  } finally {
    loading.value = false
  }
}

async function fetchRoles() {
  try {
    roles.value = (await projectApi.listRoles(projId)).data.roles ?? []
  } catch {
    // The selector falls back to the empty option, which still says what
    // no default means.
    roles.value = []
  }
}

async function fetchUsage() {
  if (!isActiveProject.value) return

  usageLoading.value = true
  try {
    usage.value = (await plansApi.usage()).data
  } catch (err) {
    notify.error(apiErrorMessage(err, 'Failed to load usage'))
  } finally {
    usageLoading.value = false
  }
}

onMounted(async () => {
  // The project FIRST, because it is what answers "what may this person
  // do here" - the two below are gated server-side and asking before the
  // answer arrives is asking to be refused. A member without
  // analytics:read would get a toast from the usage card, and one
  // without members:read a silent 403 from the roles fetch.
  await fetchProject()
  if (can('members:read')) fetchRoles()
  if (can('analytics:read')) fetchUsage()
})
</script>

<template>
  <div>
    <PageHeader>
      <template #title>
        <h1>{{ proj?.name || 'Project' }} - Settings</h1>
        <p v-if="proj" class="header-sub">
          <code>{{ proj.slug }}</code>
          <span v-if="iAmOwner" class="badge badge-info">owner</span>
        </p>
      </template>
      <button class="btn btn-secondary" @click="router.push(`/projects/${projId}/members`)">
        Members
      </button>
      <button class="btn btn-secondary" @click="router.push('/projects')">Back</button>
    </PageHeader>

    <LoadingBlock v-if="loading" />

    <template v-else-if="proj">
      <SettingsForm
        :project="proj"
        :editable="canEditSettings"
        :sandbox-ceiling="sandboxCeiling"
        @saved="proj = $event"
      />

      <DefaultRoleCard
        v-if="canManageMembers"
        :project-id="projId"
        :roles="roles"
        :model-value="proj.default_role_id ?? ''"
        @saved="proj.default_role_id = $event"
      />

      <PlanUsageCard
        v-if="can('analytics:read')"
        :usage="usage"
        :loading="usageLoading"
        :is-active-project="isActiveProject"
      />

      <ExportCard :slug="proj.slug" :is-active-project="isActiveProject" />

      <DangerZone v-if="iAmOwner" :project-id="projId" :name="proj.name" />
    </template>
  </div>
</template>

<style scoped>
.header-sub {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--text-muted);
  font-size: 13px;
  margin-top: 4px;
}
</style>
