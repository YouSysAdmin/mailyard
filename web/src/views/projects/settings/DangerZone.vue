<script setup lang="ts">
// Deleting the project.
//
// OWNERSHIP, not a permission. Deleting is the one act the permission
// catalogue cannot name, and the server decides it the same way - so
// this card is not gated on settings:write however wide that is.
//
// Two confirmations, and the second is not theatre: the first names the
// project, the second names what goes with it. Everything a project
// holds is removed and nothing here can put it back.
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { projectApi } from '../../../api/projects'
import { apiErrorMessage } from '../../../api/client'
import { useNotificationStore } from '../../../stores/notification'
import { useProjectStore } from '../../../stores/project'
import { useConfirm } from '../../../composables/useConfirm'

const props = defineProps<{ projectId: string; name: string }>()

const router = useRouter()
const notify = useNotificationStore()
const projStore = useProjectStore()
const { confirm } = useConfirm()

const deleting = ref(false)

async function remove() {
  const first = await confirm({
    title: 'Delete Project',
    message: `Delete project "${props.name}" and everything in it? This cannot be undone.`,
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!first) return

  const second = await confirm({
    title: 'Confirm Deletion',
    message: `This permanently removes all emails, templates, keys, and members of "${props.name}". Are you absolutely sure?`,
    confirmText: 'Delete Forever',
    variant: 'danger',
  })
  if (!second) return

  deleting.value = true
  try {
    await projectApi.remove(props.projectId)
    notify.success('Project deleted')
    // The console stores the active project id and sends it on every
    // request. Left pointing at a project that no longer exists, every
    // page that follows asks about nothing.
    if (projStore.currentProjectId === props.projectId) {
      await projStore.setProject(null)
    }
    await projStore.fetchProjects(true)
    router.push('/projects')
  } catch (err) {
    notify.error(apiErrorMessage(err, 'Failed to delete project'))
  } finally {
    deleting.value = false
  }
}
</script>

<template>
  <div class="card">
    <div class="card-header">
      <h2>Danger Zone</h2>
    </div>

    <div class="card-body">
      <p class="danger-note">
        Deleting this project permanently removes all of its resources. This cannot be undone.
      </p>
      <button class="btn btn-danger" :disabled="deleting" @click="remove">
        {{ deleting ? 'Deleting...' : 'Delete Project' }}
      </button>
    </div>
  </div>
</template>

<style scoped>
.danger-note {
  font-size: 13px;
  color: var(--text-muted);
  margin-bottom: 12px;
}
</style>
