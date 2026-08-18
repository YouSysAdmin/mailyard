<script setup lang="ts">
// The role a member carries when nobody assigns them one.
//
// Gated on members:write rather than settings:write, and that is not an
// oversight: it decides ACCESS, which is the same question the Roles page
// answers. Somebody who may rename the project is not thereby somebody
// who may hand out permissions.
import { ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { projectApi } from '../../../api/projects'
import { apiErrorMessage } from '../../../api/client'
import type { ProjectRole } from '../../../api/types'
import { useNotificationStore } from '../../../stores/notification'
import FormField from '../../../components/FormField.vue'

const props = defineProps<{
  projectId: string
  roles: ProjectRole[]
  /** The role currently named as the default, or ''. */
  modelValue: string
}>()

const emit = defineEmits<{ (e: 'saved', roleID: string): void }>()

const router = useRouter()
const notify = useNotificationStore()

const selected = ref(props.modelValue)
const saving = ref(false)

watch(
  () => props.modelValue,
  (v) => (selected.value = v),
)

async function save() {
  saving.value = true
  try {
    await projectApi.setDefaultRole(props.projectId, selected.value)
    notify.success(
      selected.value
        ? 'Default role saved'
        : 'Default role cleared - members with no role of their own now reach nothing',
    )
    emit('saved', selected.value)
  } catch (err) {
    notify.error(apiErrorMessage(err, 'Failed to save the default role'))
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="card">
    <div class="card-header">
      <h2>Default Role</h2>
      <button class="btn btn-secondary btn-sm" @click="router.push(`/projects/${projectId}/roles`)">
        Manage roles
      </button>
    </div>

    <div class="card-body">
      <FormField
        label="Members with no role of their own carry"
        :hint="
          roles.length === 0
            ? 'This project has no roles yet. Until it has one and names it here, every member except an owner can reach nothing.'
            : 'Applied to everybody an invitation, a single sign-on provision or a plain add creates without naming a role. A role assigned to somebody directly always wins over this.'
        "
      >
        <select v-model="selected" class="form-select">
          <option value="">Nothing - they can reach no resource here</option>
          <option v-for="r in roles" :key="r.id" :value="r.id">{{ r.name }}</option>
        </select>
      </FormField>

      <button class="btn btn-primary" :disabled="saving" @click="save">
        {{ saving ? 'Saving...' : 'Save' }}
      </button>
    </div>
  </div>
</template>
