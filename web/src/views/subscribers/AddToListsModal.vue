<script setup lang="ts">
// Put one subscriber on several static lists at once.
//
// The dialog knows which lists already hold them, so those come
// checked and locked rather than offered again: adding is idempotent
// on the server, but a box that can be ticked and changes nothing is a
// box the reader will wonder about. Taking somebody OFF a list is the
// list page's job, which is why unticking is not offered here.
//
// Dynamic lists are not listed. Their membership is a rule, and the way
// onto one is to satisfy it.
import { computed, ref } from 'vue'
import { subscriberListsApi } from '../../api/subscriberLists'
import { subscribersApi } from '../../api/subscribers'
import { apiErrorMessage } from '../../api/client'
import type { Subscriber, SubscriberList } from '../../api/types'
import { useNotificationStore } from '../../stores/notification'
import BaseModal from '../../components/BaseModal.vue'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'

const props = defineProps<{ subscriber: Subscriber }>()
const emit = defineEmits<{
  (e: 'added', lists: SubscriberList[]): void
  (e: 'close'): void
}>()

const notify = useNotificationStore()

const lists = ref<SubscriberList[]>([])
const member = ref(new Set<string>())
const chosen = ref(new Set<string>())
const loading = ref(true)
const saving = ref(false)

const selectable = computed(() => lists.value.filter((l) => !member.value.has(l.id)))

async function load() {
  try {
    const [all, mine] = await Promise.all([
      subscriberListsApi.list(),
      subscribersApi.lists(props.subscriber.id),
    ])
    lists.value = (all.data.subscriber_lists ?? []).filter((l) => l.type === 'static')
    member.value = new Set((mine.data.subscriber_lists ?? []).map((l) => l.id))
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load the lists'))
    emit('close')
  } finally {
    loading.value = false
  }
}

function toggle(id: string) {
  if (chosen.value.has(id)) chosen.value.delete(id)
  else chosen.value.add(id)
}

async function add() {
  const targets = lists.value.filter((l) => chosen.value.has(l.id))
  if (targets.length === 0) return

  saving.value = true
  try {
    await Promise.all(
      targets.map((l) =>
        subscriberListsApi.addMember(l.id, { subscriber_id: props.subscriber.id }),
      ),
    )
    notify.success(
      targets.length === 1 ? `Added to ${targets[0].name}` : `Added to ${targets.length} lists`,
    )
    emit('added', targets)
    emit('close')
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to add to the list'))
  } finally {
    saving.value = false
  }
}

void load()
</script>

<template>
  <BaseModal title="Add to lists" @close="emit('close')">
    <p class="form-hint list-subject">{{ subscriber.email }}</p>

    <LoadingBlock v-if="loading" />

    <EmptyState v-else-if="lists.length === 0">
      <p>
        There are no static lists in this project yet.
        <router-link to="/subscriber-lists">Create one</router-link> and come back.
      </p>
    </EmptyState>

    <div v-else class="list-options">
      <label v-for="l in lists" :key="l.id" class="checkbox-label">
        <input
          type="checkbox"
          :checked="member.has(l.id) || chosen.has(l.id)"
          :disabled="member.has(l.id) || saving"
          @change="toggle(l.id)"
        />
        <span>{{ l.name }}</span>
        <span v-if="member.has(l.id)" class="text-muted">already on it</span>
      </label>
      <p v-if="selectable.length === 0" class="form-hint">Already on every list.</p>
    </div>

    <template #footer>
      <button class="btn btn-secondary" @click="emit('close')">Cancel</button>
      <button class="btn btn-primary" :disabled="saving || chosen.size === 0" @click="add">
        {{ saving ? 'Adding...' : chosen.size > 1 ? `Add to ${chosen.size} lists` : 'Add' }}
      </button>
    </template>
  </BaseModal>
</template>

<style scoped>
.list-subject {
  margin: 0 0 12px;
}

.list-options {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
</style>
