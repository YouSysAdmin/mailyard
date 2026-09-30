<script setup lang="ts">
// One list: what it is, who is in it, and the per-list opt-outs.
//
// The type decides which half of the page exists. A static list has
// MEMBERS and no rules. A dynamic one has RULES and no member table,
// because its membership is not stored anywhere to show - it is
// computed when a campaign sends.
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { subscriberListsApi } from '../../api/subscriberLists'
import { apiErrorMessage } from '../../api/client'
import type { FilterRule, Subscriber, SubscriberList, SubscriberListMember } from '../../api/types'
import { useClientPager } from '../../composables/usePagination'
import { useConfirm } from '../../composables/useConfirm'
import { useFieldErrors } from '../../composables/fieldErrors'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import { formatDate } from '../../composables/formatDate'
import Pagination from '../../components/Pagination.vue'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import PageHeader from '../../components/PageHeader.vue'
import BaseModal from '../../components/BaseModal.vue'
import FormField from '../../components/FormField.vue'
import SegmentRules from './SegmentRules.vue'
import SubscriberPicker from '../../components/SubscriberPicker.vue'

const route = useRoute()
const router = useRouter()
const notify = useNotificationStore()
const projects = useProjectStore()
const { confirm } = useConfirm()
const { errors, capture, clear } = useFieldErrors()

const id = String(route.params.id)

const list = ref<SubscriberList | null>(null)
const memberCount = ref<number | null>(null)
const optedOutCount = ref(0)
const members = ref<SubscriberListMember[]>([])
const loading = ref(true)
const membersLoading = ref(false)
const saving = ref(false)

// Who opted out of this list, for both types. A dynamic list has no
// member table, so this card is the only place its opt-outs show.
const optOuts = ref<SubscriberListMember[]>([])
const optOutsLoading = ref(false)

const dynamic = computed(() => list.value?.type === 'dynamic')
const mayWrite = computed(() => projects.can('subscribers:write'))

const form = ref({ name: '', description: '', rules: [] as FilterRule[] })

const { pageable, pageItems, goToPage } = useClientPager(members, 20)

// Adding a member. The box searches the audience as you type, and a
// row picked from it carries the id. An address typed in full works
// too, so a pasted one needs no menu.
const adding = ref<{ id: string; email: string } | null>(null)
const addBusy = ref(false)

const optOut = ref('')
const optOutBusy = ref(false)

async function loadList() {
  try {
    const res = await subscriberListsApi.get(id)
    list.value = res.data.subscriber_list
    memberCount.value = res.data.member_count ?? null
    optedOutCount.value = res.data.opted_out_count ?? 0
    form.value = {
      name: res.data.subscriber_list.name,
      description: res.data.subscriber_list.description ?? '',
      rules: res.data.subscriber_list.filter_rules ?? [],
    }
  } catch (e) {
    notify.error(apiErrorMessage(e, 'No such list'))
    router.push('/subscriber-lists')
  }
}

async function loadMembers() {
  if (dynamic.value) return

  membersLoading.value = true
  try {
    members.value = (await subscriberListsApi.listMembers(id)).data.members ?? []
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load the members'))
  } finally {
    membersLoading.value = false
  }
}

async function loadOptOuts() {
  optOutsLoading.value = true
  try {
    optOuts.value = (await subscriberListsApi.listOptOuts(id, { limit: 200 })).data.opt_outs ?? []
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load the opt-outs'))
  } finally {
    optOutsLoading.value = false
  }
}

/** Everything an opt-out changes: the count, the marker and the card. */
async function reloadOptOuts() {
  await Promise.all([loadList(), loadMembers(), loadOptOuts()])
}

async function save() {
  if (!form.value.name.trim()) return
  if (dynamic.value && form.value.rules.length === 0) {
    notify.error('A dynamic list needs at least one rule')

    return
  }

  clear()
  saving.value = true
  try {
    const res = await subscriberListsApi.update(id, {
      name: form.value.name.trim(),
      description: form.value.description.trim(),
      filter_rules: dynamic.value ? form.value.rules : undefined,
    })
    list.value = res.data.subscriber_list
    notify.success('Saved')
  } catch (e) {
    if (!capture(e)) notify.error(apiErrorMessage(e, 'Failed to save'))
  } finally {
    saving.value = false
  }
}

function openAdd() {
  clear()
  adding.value = { id: '', email: '' }
}

function onPick(s: Subscriber | null) {
  if (adding.value) adding.value.id = s?.id ?? ''
}

async function addMember() {
  const a = adding.value
  if (!a || (!a.id && !a.email.trim())) return

  clear()
  addBusy.value = true
  try {
    await subscriberListsApi.addMember(
      id,
      a.id ? { subscriber_id: a.id } : { email: a.email.trim() },
    )
    adding.value = null
    notify.success('Member added')
    await Promise.all([loadMembers(), loadList()])
  } catch (e) {
    if (!capture(e)) notify.error(apiErrorMessage(e, 'Failed to add the member'))
  } finally {
    addBusy.value = false
  }
}

async function removeMember(s: Subscriber) {
  const confirmed = await confirm({
    title: 'Remove member',
    message: `Take "${s.email}" out of this list? The subscriber itself is kept.`,
    confirmText: 'Remove',
    variant: 'danger',
  })
  if (!confirmed) return

  try {
    await subscriberListsApi.removeMember(id, s.id)
    notify.success('Member removed')
    await Promise.all([loadMembers(), loadList()])
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to remove the member'))
  }
}

/** Record or clear a per-list opt-out, which is scoped to this list. */
async function setOptOut(out: boolean) {
  const email = optOut.value.trim()
  if (!email) return

  optOutBusy.value = true
  try {
    await (out
      ? subscriberListsApi.unsubscribe(id, email)
      : subscriberListsApi.resubscribe(id, email))
    optOut.value = ''
    notify.success(out ? 'Unsubscribed from this list' : 'Resubscribed to this list')
    await reloadOptOuts()
  } catch (e) {
    notify.error(apiErrorMessage(e, out ? 'Failed to unsubscribe' : 'Failed to resubscribe'))
  } finally {
    optOutBusy.value = false
  }
}

/** Lift one opt-out from the card. Not a membership change. */
async function resubscribe(m: SubscriberListMember) {
  try {
    await subscriberListsApi.resubscribe(id, m.email)
    notify.success(`${m.email} resubscribed to this list`)
    await reloadOptOuts()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to resubscribe'))
  }
}

async function start() {
  await loadList()
  await Promise.all([loadMembers(), loadOptOuts()])
  loading.value = false
}

void start()
</script>

<template>
  <div>
    <PageHeader>
      <template #title>
        <!-- A bare router-link, deliberately: the stylesheet underlines
             anchors that carry no class, which is the only cue for a
             link when the accent is ink. A ramp colour as text does
             not reach 4.5:1. -->
        <p class="trail"><router-link to="/subscriber-lists">Lists</router-link> /</p>
        <h1>{{ list?.name || 'List' }}</h1>
      </template>

      <button v-if="list && !dynamic && mayWrite" class="btn btn-primary" @click="openAdd">
        Add member
      </button>
    </PageHeader>

    <LoadingBlock v-if="loading" />

    <template v-else-if="list">
      <div class="card">
        <div class="card-header">
          <h2>Details</h2>
          <span class="badge" :class="dynamic ? 'badge-info' : 'badge-neutral'">
            {{ list.type }}
          </span>
        </div>

        <div class="card-body">
          <FormField label="Name" :error="errors.name">
            <input v-model="form.name" class="form-input" :disabled="!mayWrite" />
          </FormField>

          <FormField label="Description" :error="errors.description">
            <input v-model="form.description" class="form-input" :disabled="!mayWrite" />
          </FormField>

          <FormField v-if="dynamic" label="Rules (all must match)">
            <SegmentRules v-model="form.rules" :readonly="!mayWrite" />
          </FormField>

          <div v-if="mayWrite" class="actions">
            <button class="btn btn-primary" :disabled="saving || !form.name.trim()" @click="save">
              {{ saving ? 'Saving...' : 'Save' }}
            </button>
          </div>
        </div>
      </div>

      <div v-if="!dynamic" class="card">
        <div class="card-header">
          <h2>Members</h2>
          <span v-if="memberCount !== null" class="text-muted">
            {{ memberCount }} total{{ optedOutCount ? `, ${optedOutCount} opted out` : '' }}
          </span>
        </div>

        <LoadingBlock v-if="membersLoading" />

        <EmptyState
          v-else-if="members.length === 0"
          title="Nobody yet"
          text="Add subscribers to this list, and a campaign sent to it reaches them."
        />

        <template v-else>
          <div class="table-wrapper">
            <table>
              <thead>
                <tr>
                  <th>Email</th>
                  <th>Name</th>
                  <th>Status</th>
                  <th>Opted out</th>
                  <th>Added</th>
                  <!-- delete, matching the cell below it. Gated on write,
                       a member holding write without delete got a header
                       column with no body cells under it and every row's
                       data shifted one column left. -->
                  <th v-if="projects.can('subscribers:delete')" class="text-right"></th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="m in pageItems" :key="m.id">
                  <td>{{ m.email }}</td>
                  <td>{{ m.name || '-' }}</td>
                  <td><StatusBadge :status="m.status" scope="subscriber" /></td>
                  <!-- The list's own opt-out, beside the global status:
                       a subscribed person who asked this list to stop. -->
                  <td>
                    <span
                      v-if="m.opted_out_at"
                      class="badge badge-warning"
                      :title="m.opt_out_reason"
                    >
                      {{ formatDate(m.opted_out_at) }}
                    </span>
                    <span v-else>-</span>
                  </td>
                  <td>{{ formatDate(m.created_at) }}</td>
                  <td v-if="projects.can('subscribers:delete')" class="text-right">
                    <button class="btn btn-danger btn-sm" @click="removeMember(m)">Remove</button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>

          <Pagination :pageable="pageable" @page="goToPage" />
        </template>
      </div>

      <div class="card">
        <div class="card-header">
          <h2>Opted out</h2>
          <span v-if="optedOutCount" class="text-muted">{{ optedOutCount }} total</span>
        </div>

        <LoadingBlock v-if="optOutsLoading" />

        <EmptyState
          v-else-if="optOuts.length === 0"
          title="Nobody has opted out"
          text="A recipient who unsubscribes from a campaign sent to this list lands here, whether they are a member or matched by rules."
        />

        <div v-else class="table-wrapper">
          <table>
            <thead>
              <tr>
                <th>Email</th>
                <th>Name</th>
                <th>Reason</th>
                <th>Opted out</th>
                <th v-if="mayWrite" class="text-right"></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="m in optOuts" :key="m.id">
                <td>
                  <router-link :to="`/subscribers/${m.id}`">{{ m.email }}</router-link>
                </td>
                <td>{{ m.name || '-' }}</td>
                <td>{{ m.opt_out_reason || '-' }}</td>
                <td>{{ formatDate(m.opted_out_at) }}</td>
                <td v-if="mayWrite" class="text-right">
                  <button class="btn btn-secondary btn-sm" @click="resubscribe(m)">
                    Resubscribe
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <div v-if="mayWrite" class="card">
        <div class="card-header">
          <h2>Opt-out</h2>
        </div>

        <div class="card-body">
          <p class="form-hint">
            Scoped to this list. A global suppression is a different thing, on the Suppressions
            page.
          </p>

          <div class="optout">
            <input
              v-model="optOut"
              type="email"
              class="form-input w-search"
              placeholder="user@example.com"
            />
            <button
              class="btn btn-secondary"
              :disabled="optOutBusy || !optOut.trim()"
              @click="setOptOut(true)"
            >
              Unsubscribe
            </button>
            <button
              class="btn btn-secondary"
              :disabled="optOutBusy || !optOut.trim()"
              @click="setOptOut(false)"
            >
              Resubscribe
            </button>
          </div>
        </div>
      </div>
    </template>

    <BaseModal v-if="adding" title="Add member" @close="adding = null">
      <!-- Room under the box for the picker's menu: it is absolute, and
           the dialog scrolls its own box, so without this the menu opens
           into a scrollbar. -->
      <FormField
        label="Subscriber"
        for="add-member-email"
        class="member-field"
        :error="errors.email"
        hint="Type part of an address to search. The address has to belong to a subscriber already."
      >
        <SubscriberPicker id="add-member-email" v-model="adding.email" @pick="onPick" />
      </FormField>

      <template #footer>
        <button class="btn btn-secondary" @click="adding = null">Cancel</button>
        <button
          class="btn btn-primary"
          :disabled="addBusy || (!adding.id && !adding.email.trim())"
          @click="addMember"
        >
          {{ addBusy ? 'Adding...' : 'Add' }}
        </button>
      </template>
    </BaseModal>
  </div>
</template>

<style scoped>
.trail {
  margin: 0 0 2px;
  color: var(--text-secondary);
  font-size: 13px;
}

/* Underlined here rather than by the stylesheet's `a:not([class])`.
   That rule is what marks a prose link when the accent is ink and a
   link cannot be told by its colour - but a router-link whose target is
   a PREFIX of the current path is given router-link-active, so this
   anchor carries a class and the rule skips it. */
.trail a {
  color: var(--accent-fg);
  text-decoration: underline;
  text-decoration-thickness: 1px;
  text-underline-offset: 2px;
  text-decoration-color: var(--border-strong);
  transition: text-decoration-color var(--transition);
}

.trail a:hover {
  text-decoration-color: currentColor;
}

.actions {
  margin-top: 16px;
}

.member-field {
  min-height: 300px;
}

/* Wraps, because an address field and two buttons do not fit a phone. */
.optout {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  margin-top: 12px;
}
</style>
