<script setup lang="ts">
// SMTP server groups: the named pools a send is routed to.
//
// Two jobs, both visible here. A group is what an integration names
// instead of a server uuid, so the servers behind it can be replaced
// without touching the caller. And it is the unit failover happens
// within - a transient failure moves to the next server in the same
// group, in priority order, never to another group.
import { computed, onMounted, ref } from 'vue'
import { smtpGroupApi, type SMTPGroupPayload } from '../../api/smtpGroups'
import { apiErrorMessage } from '../../api/client'
import type { SMTPServerGroup } from '../../api/types'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import { useConfirm } from '../../composables/useConfirm'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import PageHeader from '../../components/PageHeader.vue'
import BaseModal from '../../components/BaseModal.vue'
import FormField from '../../components/FormField.vue'
import { useFieldErrors } from '../../composables/fieldErrors'

const notify = useNotificationStore()
const { confirm } = useConfirm()
const projStore = useProjectStore()

const groups = ref<SMTPServerGroup[]>([])
const loading = ref(true)
const saving = ref(false)
const showForm = ref(false)
const editing = ref<SMTPServerGroup | null>(null)
const form = ref<SMTPGroupPayload>({ name: '', slug: '', description: '' })

// smtp:write, matching the server gate on these routes. Not a
// store-level "is project admin" flag - that shadows the local canEdit
// with a wider tier, and the same name then means two different things
// depending on the file.
const canEdit = computed(() => projStore.can('smtp:write'))
// Its own gate: the DELETE route asks smtp:delete, and a role that
// may configure a pool without dismantling it is a real one.
const canDelete = computed(() => projStore.can('smtp:delete'))

const { errors: fieldErrors, capture, clear } = useFieldErrors()

async function load() {
  loading.value = true
  try {
    const res = await smtpGroupApi.list()
    groups.value = res.data.smtp_server_groups ?? []
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load server groups'))
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editing.value = null
  form.value = { name: '', slug: '', description: '' }
  showForm.value = true
}

function openEdit(g: SMTPServerGroup) {
  editing.value = g
  form.value = { name: g.name, slug: g.slug, description: g.description ?? '' }
  showForm.value = true
}

async function save() {
  clear()
  saving.value = true
  try {
    if (editing.value) {
      await smtpGroupApi.update(editing.value.id, form.value)
      notify.success('Group updated')
    } else {
      await smtpGroupApi.create(form.value)
      notify.success('Group created')
    }
    showForm.value = false
    await load()
  } catch (e) {
    if (!capture(e)) notify.error(apiErrorMessage(e, 'Failed to save the group'))
  } finally {
    saving.value = false
  }
}

async function makeDefault(g: SMTPServerGroup) {
  clear()
  try {
    await smtpGroupApi.update(g.id, { make_default: true })
    notify.success(`${g.name} is now the default`)
    await load()
  } catch (e) {
    if (!capture(e)) notify.error(apiErrorMessage(e, 'Failed to change the default'))
  }
}

async function remove(g: SMTPServerGroup) {
  const n = g.servers?.length ?? 0
  const moved = n > 0 ? ` Its ${n} server${n === 1 ? '' : 's'} will move to the default group.` : ''
  const ok = await confirm({
    title: 'Delete group',
    message: `Delete ${g.name}?${moved}`,
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!ok) return
  try {
    await smtpGroupApi.remove(g.id)
    notify.success('Group deleted')
    await load()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to delete the group'))
  }
}

// The overlay closes on a click that BEGAN on it. A plain
// @click.self also fires when a drag-select inside the form ends
// out here, throwing away what was typed.

onMounted(load)
</script>

<template>
  <div>
    <PageHeader title="Server Groups">
      <button v-if="canEdit" class="btn btn-primary" @click="openCreate">New Group</button>
    </PageHeader>

    <LoadingBlock v-if="loading" />

    <template v-else>
      <div v-for="g in groups" :key="g.id" class="card group-card">
        <div class="card-header">
          <div>
            <h2>
              {{ g.name }}
              <span v-if="g.is_default" class="badge badge-info">Default</span>
            </h2>
            <p class="text-sm text-muted">
              <code>{{ g.slug }}</code>
              <template v-if="g.description"> - {{ g.description }}</template>
            </p>
          </div>
          <div v-if="canEdit" class="table-actions">
            <button v-if="!g.is_default" class="btn btn-secondary btn-sm" @click="makeDefault(g)">
              Make default
            </button>
            <button class="btn btn-secondary btn-sm" @click="openEdit(g)">Edit</button>
            <button
              v-if="!g.is_default && canDelete"
              class="btn btn-danger btn-sm"
              @click="remove(g)"
            >
              Delete
            </button>
          </div>
        </div>

        <EmptyState
          v-if="!g.servers?.length"
          title="No servers"
          text="A send routed here falls through to whatever the default group holds."
        />

        <div v-else class="table-wrapper">
          <table>
            <thead>
              <tr>
                <th>Priority</th>
                <th>Server</th>
                <th>Host</th>
                <th>Status</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="srv in g.servers" :key="srv.id">
                <td>{{ srv.priority }}</td>
                <td>
                  <router-link :to="`/smtp-servers/${srv.id}`">{{ srv.name }}</router-link>
                </td>
                <td>{{ srv.host }}:{{ srv.port }}</td>
                <td>
                  <span v-if="srv.status === 'enabled'" class="badge badge-success badge-dot"
                    >Enabled</span
                  >
                  <span v-else-if="srv.status === 'invalid'" class="badge badge-danger badge-dot"
                    >Invalid</span
                  >
                  <span v-else class="badge badge-neutral badge-dot">Disabled</span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </template>

    <BaseModal
      v-if="showForm"
      :title="editing ? 'Edit Group' : 'New Group'"
      form
      @submit="save"
      @close="showForm = false"
    >
      <FormField label="Name" :error="fieldErrors.name">
        <input v-model="form.name" class="form-input" placeholder="Bulk" />
      </FormField>
      <FormField
        label="Slug"
        :error="fieldErrors.slug"
        hint="What a send names. Derived from the name when left empty. Changing it breaks any integration already using the old one."
      >
        <input v-model="form.slug" class="form-input" placeholder="bulk" />
      </FormField>
      <FormField :error="fieldErrors.description">
        <template #label>Description <span class="text-muted">(optional)</span></template>
        <input v-model="form.description" class="form-input" />
      </FormField>
      <template #footer>
        <button
          type="button"
          class="btn btn-secondary"
          :disabled="saving"
          @click="showForm = false"
        >
          Cancel
        </button>
        <button type="submit" class="btn btn-primary" :disabled="saving || !form.name">
          {{ saving ? 'Saving...' : 'Save' }}
        </button>
      </template>
    </BaseModal>
  </div>
</template>

<style scoped>
/* One card per group, so they need the gap a single-card page gets
   for free. Everything else is the shared card, table and badge
   styling. */
.group-card + .group-card {
  margin-top: 16px;
}
.card-header h2 .badge {
  margin-left: 6px;
  vertical-align: middle;
}
</style>
