<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import {
  unsubscribeListsApi,
  type UnsubscribeList,
  type UnsubscribeListPayload,
} from '../../api/unsubscribeLists'
import { apiErrorMessage } from '../../api/client'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import { useConfirm } from '../../composables/useConfirm'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import PageHeader from '../../components/PageHeader.vue'
import BaseModal from '../../components/BaseModal.vue'
import CopyButton from '../../components/CopyButton.vue'
import FormField from '../../components/FormField.vue'
import { useFieldErrors } from '../../composables/fieldErrors'

const notify = useNotificationStore()
const projStore = useProjectStore()
const { confirm } = useConfirm()

const lists = ref<UnsubscribeList[]>([])
const loading = ref(true)
const saving = ref(false)
const showModal = ref(false)
const editing = ref<UnsubscribeList | null>(null)

const form = ref<UnsubscribeListPayload>({ name: '', public_name: '', description: '' })

const { errors: fieldErrors, capture, clear } = useFieldErrors()

async function load() {
  loading.value = true
  try {
    const res = await unsubscribeListsApi.list()
    lists.value = res.data.unsubscribe_lists ?? []
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load unsubscribe lists'))
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editing.value = null
  form.value = { name: '', public_name: '', description: '' }
  showModal.value = true
}

function openEdit(l: UnsubscribeList) {
  editing.value = l
  form.value = {
    name: l.name,
    public_name: l.public_name ?? '',
    description: l.description ?? '',
  }
  showModal.value = true
}

async function save() {
  clear()
  if (!form.value.name?.trim()) return
  saving.value = true
  try {
    if (editing.value) {
      await unsubscribeListsApi.update(editing.value.id, form.value)
      notify.success('Unsubscribe list updated')
    } else {
      await unsubscribeListsApi.create(form.value)
      notify.success('Unsubscribe list created')
    }
    showModal.value = false
    await load()
  } catch (e) {
    if (!capture(e)) notify.error(apiErrorMessage(e, 'Failed to save the unsubscribe list'))
  } finally {
    saving.value = false
  }
}

async function toggleActive(l: UnsubscribeList) {
  clear()
  try {
    await unsubscribeListsApi.update(l.id, { active: !l.active })
    notify.success(l.active ? 'List paused' : 'List activated')
    await load()
  } catch (e) {
    if (!capture(e)) notify.error(apiErrorMessage(e, 'Failed to update the list'))
  }
}

async function remove(l: UnsubscribeList) {
  const ok = await confirm({
    title: 'Delete Unsubscribe List',
    message:
      `Delete "${l.name}"? The ${l.suppressed_count} opt-out(s) recorded against it are kept, ` +
      'so nobody is silently resubscribed. Sends referencing this list will start failing.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!ok) return
  try {
    await unsubscribeListsApi.remove(l.id)
    notify.success('Unsubscribe list deleted')
    await load()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to delete the list'))
  }
}

watch(() => projStore.currentProjectId, load)
onMounted(load)
</script>

<template>
  <div>
    <PageHeader title="Unsubscribe Lists">
      <button
        v-if="projStore.can('suppressions:write')"
        class="btn btn-primary"
        @click="openCreate"
      >
        Create List
      </button>
    </PageHeader>

    <div class="card">
      <div class="card-body">
        <p class="text-sm text-muted">
          A transactional opt-out scope. Reference one from a send and
          <code>&#123;&#123; mailyard_unsubscribe_url &#125;&#125;</code> renders a one-click link
          that blocks only that category of mail - receipts and password resets keep flowing. These
          lists have no members: an address is opted out when it appears on the
          <router-link to="/suppressions">suppression list</router-link> against this scope.
        </p>
      </div>

      <LoadingBlock v-if="loading" />

      <template v-else>
        <EmptyState
          v-if="lists.length === 0"
          title="No unsubscribe lists"
          text="Create one to give recipients a way to opt out of one category of mail."
        />

        <div v-else class="table-wrapper">
          <table>
            <thead>
              <tr>
                <th>Name</th>
                <th>Shown to recipients</th>
                <th class="text-right">Opted out</th>
                <th>Status</th>
                <th class="text-right">Actions</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="l in lists" :key="l.id">
                <td class="cell-title">
                  {{ l.name }}
                  <p v-if="l.description" class="row-desc">{{ l.description }}</p>
                </td>
                <td>{{ l.public_name || l.name }}</td>
                <td class="text-right">{{ l.suppressed_count }}</td>
                <td>
                  <span
                    class="badge badge-dot"
                    :class="l.active ? 'badge-success' : 'badge-neutral'"
                  >
                    {{ l.active ? 'Active' : 'Paused' }}
                  </span>
                </td>
                <td>
                  <div class="table-actions">
                    <CopyButton :value="l.id" label="Copy ID" />
                    <template v-if="projStore.can('suppressions:write')">
                      <button class="btn btn-secondary btn-sm" @click="openEdit(l)">Edit</button>
                      <button class="btn btn-warning btn-sm" @click="toggleActive(l)">
                        {{ l.active ? 'Pause' : 'Activate' }}
                      </button>
                    </template>
                    <button
                      v-if="projStore.can('suppressions:delete')"
                      class="btn btn-danger btn-sm"
                      @click="remove(l)"
                    >
                      Delete
                    </button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </template>
    </div>

    <BaseModal
      v-if="showModal"
      :title="editing ? 'Edit Unsubscribe List' : 'Create Unsubscribe List'"
      form
      @submit="save"
      @close="showModal = false"
    >
      <FormField
        label="Name"
        :error="fieldErrors.name"
        hint="Internal label, unique in this project."
      >
        <input
          v-model="form.name"
          type="text"
          class="form-input"
          placeholder="e.g. product-updates"
          required
        />
      </FormField>
      <FormField
        :error="fieldErrors.public_name"
        hint="Shown on the unsubscribe page. Falls back to the name above."
      >
        <template #label>Public name <span class="text-muted">(optional)</span></template>
        <input
          v-model="form.public_name"
          type="text"
          class="form-input"
          placeholder="e.g. Product updates"
        />
      </FormField>
      <FormField :error="fieldErrors.description">
        <template #label>Description <span class="text-muted">(optional)</span></template>
        <textarea v-model="form.description" class="form-textarea" rows="2"></textarea>
      </FormField>
      <template #footer>
        <button type="button" class="btn btn-secondary" @click="showModal = false">Cancel</button>
        <button type="submit" class="btn btn-primary" :disabled="saving || !form.name?.trim()">
          {{ saving ? 'Saving...' : 'Save' }}
        </button>
      </template>
    </BaseModal>
  </div>
</template>

<style scoped>
.row-desc {
  margin: 2px 0 0;
  font-size: 12px;
  color: var(--text-muted);
  font-weight: normal;
}
</style>
