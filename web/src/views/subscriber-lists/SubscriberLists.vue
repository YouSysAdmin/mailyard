<script setup lang="ts">
// Lists and segments - the two things a campaign can be addressed to.
//
// A STATIC list holds the members somebody put in it. A DYNAMIC one
// holds rules, and who it means is worked out at send time. The type is
// decided here and never again, which is why the dialog says so.
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { subscriberListsApi } from '../../api/subscriberLists'
import { apiErrorMessage } from '../../api/client'
import type { FilterRule, SubscriberList } from '../../api/types'
import { useClientPager } from '../../composables/usePagination'
import { useConfirm } from '../../composables/useConfirm'
import { useFieldErrors } from '../../composables/fieldErrors'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import { formatDate } from '../../composables/formatDate'
import Pagination from '../../components/Pagination.vue'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import PageHeader from '../../components/PageHeader.vue'
import BaseModal from '../../components/BaseModal.vue'
import FormField from '../../components/FormField.vue'
import SegmentRules from './SegmentRules.vue'

const router = useRouter()
const notify = useNotificationStore()
const projects = useProjectStore()
const { confirm } = useConfirm()
const { errors, capture, clear } = useFieldErrors()

const lists = ref<SubscriberList[]>([])
const loading = ref(true)

const { pageable, pageItems, goToPage } = useClientPager(lists, 20)

const draft = ref<{
  name: string
  description: string
  type: 'static' | 'dynamic'
  rules: FilterRule[]
} | null>(null)
const saving = ref(false)

const canSave = computed(() => {
  const d = draft.value
  if (!d || !d.name.trim()) return false

  // A dynamic list with no rules matches nothing, which the server
  // refuses - so the button says so rather than the toast.
  return d.type === 'static' || d.rules.length > 0
})

async function load() {
  loading.value = true
  try {
    lists.value = (await subscriberListsApi.list()).data.subscriber_lists ?? []
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load the lists'))
  } finally {
    loading.value = false
  }
}

function openDraft() {
  clear()
  draft.value = { name: '', description: '', type: 'static', rules: [] }
}

async function create() {
  const d = draft.value
  if (!d || !canSave.value) return

  saving.value = true
  try {
    await subscriberListsApi.create({
      name: d.name.trim(),
      description: d.description.trim(),
      type: d.type,
      filter_rules: d.type === 'dynamic' ? d.rules : undefined,
    })
    draft.value = null
    notify.success('List created')
    await load()
  } catch (e) {
    if (!capture(e)) notify.error(apiErrorMessage(e, 'Failed to create the list'))
  } finally {
    saving.value = false
  }
}

async function remove(list: SubscriberList) {
  const confirmed = await confirm({
    title: 'Delete list',
    message: `Delete "${list.name}"? This cannot be undone.`,
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return

  try {
    await subscriberListsApi.remove(list.id)
    notify.success('List deleted')
    await load()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to delete the list'))
  }
}

void load()
</script>

<template>
  <div>
    <PageHeader title="Subscriber lists">
      <button v-if="projects.can('subscribers:write')" class="btn btn-primary" @click="openDraft">
        New list
      </button>
    </PageHeader>

    <LoadingBlock v-if="loading" />

    <div v-else class="card">
      <EmptyState
        v-if="lists.length === 0"
        title="No lists yet"
        text="A list is who a campaign goes to - either a set of members you pick, or a rule that finds them."
      />

      <template v-else>
        <div class="table-wrapper">
          <table>
            <thead>
              <tr>
                <th>Name</th>
                <th>Type</th>
                <th>Rules</th>
                <th>Created</th>
                <th v-if="projects.can('subscribers:delete')" class="text-right"></th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="list in pageItems"
                :key="list.id"
                class="row-clickable"
                @click="router.push(`/subscriber-lists/${list.id}`)"
              >
                <td>
                  <strong>{{ list.name }}</strong>
                  <div v-if="list.description" class="text-muted text-sm">
                    {{ list.description }}
                  </div>
                </td>
                <td>
                  <span
                    class="badge"
                    :class="list.type === 'dynamic' ? 'badge-info' : 'badge-neutral'"
                  >
                    {{ list.type }}
                  </span>
                </td>
                <td>{{ list.type === 'dynamic' ? (list.filter_rules?.length ?? 0) : '-' }}</td>
                <td>{{ formatDate(list.created_at) }}</td>
                <td v-if="projects.can('subscribers:delete')" class="text-right">
                  <button class="btn btn-danger btn-sm" @click.stop="remove(list)">Delete</button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <Pagination :pageable="pageable" @page="goToPage" />
      </template>
    </div>

    <BaseModal v-if="draft" title="New list" size="modal-w720" @close="draft = null">
      <FormField label="Name" :error="errors.name">
        <input v-model="draft.name" class="form-input" placeholder="Active customers" />
      </FormField>

      <FormField label="Description" :error="errors.description">
        <input v-model="draft.description" class="form-input" placeholder="Optional" />
      </FormField>

      <FormField
        label="Type"
        :error="errors.type"
        hint="This cannot be changed once the list exists."
      >
        <select v-model="draft.type" class="form-select">
          <option value="static">Static - members you choose</option>
          <option value="dynamic">Dynamic - whoever matches the rules</option>
        </select>
      </FormField>

      <FormField v-if="draft.type === 'dynamic'" label="Rules (all must match)">
        <SegmentRules v-model="draft.rules" />
      </FormField>

      <template #footer>
        <button class="btn btn-secondary" @click="draft = null">Cancel</button>
        <button class="btn btn-primary" :disabled="saving || !canSave" @click="create">
          {{ saving ? 'Creating...' : 'Create' }}
        </button>
      </template>
    </BaseModal>
  </div>
</template>
