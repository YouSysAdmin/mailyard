<script setup lang="ts">
// Who may reach one project, in two tabs.
//
// Members and invitations are two resources with two sets of routes and
// two sets of writes, and they shared one file with the project fetch,
// the permission resolution and the tab state. Each is its own card now;
// this holds what they both depend on.
//
// PERMISSIONS COME FROM THIS PAGE'S OWN REQUEST, not the project store.
// The page addresses a project by path id, which is not necessarily the
// store's current one - so the store's can() would answer for the wrong
// project. GET /projects/:id returns the set that applies here, and it
// covers platform admins too, since the server hands them the wildcard.
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { projectApi } from '../../api/projects'
import { apiErrorMessage } from '../../api/client'
import type { Project, ProjectInvitation, ProjectMember, ProjectRole } from '../../api/types'
import { useNotificationStore } from '../../stores/notification'
import LoadingBlock from '../../components/LoadingBlock.vue'
import PageHeader from '../../components/PageHeader.vue'
import MembersCard from './members/MembersCard.vue'
import InvitationsCard from './members/InvitationsCard.vue'

const route = useRoute()
const router = useRouter()
const notify = useNotificationStore()

const projId = route.params.id as string
const proj = ref<Project | null>(null)
const iAmOwner = ref(false)
const perms = ref<string[]>([])
const loading = ref(true)

const members = ref<ProjectMember[]>([])
const invitations = ref<ProjectInvitation[]>([])
const roles = ref<ProjectRole[]>([])
const defaultRole = computed(() => roles.value.find((r) => r.default) ?? null)

const activeTab = ref<'members' | 'invitations'>('members')

function can(p: string): boolean {
  return perms.value.includes('*') || perms.value.includes(p)
}

// members:write is what the server checks on every management route
// here, so it is what the buttons check too. Removing is members:delete,
// which is a narrower thing than handing out roles.
const canManage = computed(() => can('members:write'))
const canRemove = computed(() => can('members:delete'))

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

    return
  } finally {
    loading.value = false
  }

  await refresh()
}

async function refresh() {
  const loads = [fetchMembers(), fetchRoles()]
  // Listing invitations needs members:write on the server, so a reader
  // who lacks it is not asked to.
  if (canManage.value) loads.push(fetchInvitations())
  await Promise.all(loads)
}

async function fetchMembers() {
  try {
    members.value = (await projectApi.listMembers(projId)).data.members ?? []
  } catch (err) {
    notify.error(apiErrorMessage(err, 'Failed to load members'))
  }
}

async function fetchInvitations() {
  try {
    invitations.value = (await projectApi.listInvitations(projId)).data.invitations ?? []
  } catch (err) {
    notify.error(apiErrorMessage(err, 'Failed to load invitations'))
  }
}

async function fetchRoles() {
  try {
    roles.value = (await projectApi.listRoles(projId)).data.roles ?? []
  } catch {
    // The selectors fall back to the empty option, which still says what
    // no role means. Blocking the page over this would be worse.
    roles.value = []
  }
}

onMounted(fetchProject)
</script>

<template>
  <div>
    <PageHeader>
      <template #title>
        <h1>{{ proj?.name || 'Project' }} - Members</h1>
        <p v-if="proj" class="header-sub">
          <code>{{ proj.slug }}</code>
        </p>
      </template>
      <button class="btn btn-secondary" @click="router.push(`/projects/${projId}`)">
        Project Settings
      </button>
      <button class="btn btn-secondary" @click="router.push('/projects')">Back</button>
    </PageHeader>

    <LoadingBlock v-if="loading" />

    <template v-else>
      <!-- Only offered where there is a second tab to reach. -->
      <div v-if="canManage" class="tabs">
        <button
          :class="['tab', { active: activeTab === 'members' }]"
          @click="activeTab = 'members'"
        >
          Members ({{ members.length }})
        </button>
        <button
          :class="['tab', { active: activeTab === 'invitations' }]"
          @click="activeTab = 'invitations'"
        >
          Invitations ({{ invitations.length }})
        </button>
      </div>

      <MembersCard
        v-if="activeTab === 'members'"
        :project-id="projId"
        :members="members"
        :roles="roles"
        :default-role="defaultRole"
        :can-manage="canManage"
        :can-remove="canRemove"
        :i-am-owner="iAmOwner"
        @changed="fetchMembers"
        @owner-changed="fetchProject"
      />

      <InvitationsCard
        v-else-if="canManage"
        :project-id="projId"
        :invitations="invitations"
        :roles="roles"
        :default-role="defaultRole"
        :can-remove="canRemove"
        @changed="fetchInvitations"
      />
    </template>
  </div>
</template>

<style scoped></style>
