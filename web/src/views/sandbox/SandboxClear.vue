<script setup lang="ts">
// What to remove: everything the sandbox holds, or only the mail of the
// inboxes ticked. A dialog rather than a confirm sentence because once
// captures can be filtered by sender, "empty" has more than one
// meaning, and the person pressing it is the one who knows which.
import { ref, computed } from 'vue'
import { sandboxApi, type SandboxInbox } from '../../api/sandbox'
import { apiErrorMessage } from '../../api/client'
import { useNotificationStore } from '../../stores/notification'
import BaseModal from '../../components/BaseModal.vue'

const props = defineProps<{
  inboxes: SandboxInbox[]
  // The count on screen, which is the filtered one when an inbox is
  // selected - so it is only quoted when it is the whole sandbox.
  total: number
  // The inbox the page is filtered to, ticked in advance. Empty means
  // All mail.
  current: string
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'cleared', inboxIds: string[]): void
}>()

const notify = useNotificationStore()

const everything = ref(!props.current)
const chosen = ref<string[]>(props.current ? [props.current] : [])
const clearing = ref(false)

// The addresses behind the ticked inboxes. An inbox saved through the
// API may hold none, and the server reads an empty list as everything,
// so a selection that names no address is not a selection.
const senders = computed(() =>
  props.inboxes.filter((box) => chosen.value.includes(box.id)).flatMap((box) => box.addresses),
)

const canDelete = computed(() => everything.value || senders.value.length > 0)

async function submit() {
  if (!canDelete.value || clearing.value) return
  clearing.value = true
  try {
    const ids = everything.value ? [] : chosen.value
    const res = await sandboxApi.clear(everything.value ? [] : senders.value)
    notify.success(`Deleted ${res.data.deleted} messages`)
    emit('cleared', ids)
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to empty the sandbox'))
  } finally {
    clearing.value = false
  }
}
</script>

<template>
  <BaseModal
    title="Empty the sandbox"
    size="modal-w480"
    form
    @close="emit('close')"
    @submit="submit"
  >
    <p class="text-muted">Nothing here was ever delivered, so this affects no recipient.</p>

    <div class="clear-options">
      <label class="checkbox-label">
        <input v-model="everything" type="checkbox" />
        <span>
          Everything in the sandbox
          <span v-if="!current" class="text-muted">({{ total }} messages)</span>
        </span>
      </label>

      <template v-if="inboxes.length > 0">
        <label v-for="box in inboxes" :key="box.id" class="checkbox-label">
          <input v-model="chosen" type="checkbox" :value="box.id" :disabled="everything" />
          <span>
            {{ box.name }}
            <span class="text-muted">({{ box.addresses.join(', ') }})</span>
          </span>
        </label>
        <p class="form-hint">Mail whose sender is in no inbox is only removed with everything.</p>
      </template>
    </div>

    <template #footer>
      <button type="button" class="btn btn-secondary" @click="emit('close')">Cancel</button>
      <button type="submit" class="btn btn-danger" :disabled="!canDelete || clearing">
        {{ clearing ? 'Deleting...' : everything ? 'Delete all' : 'Delete selected' }}
      </button>
    </template>
  </BaseModal>
</template>

<style scoped>
.clear-options {
  display: flex;
  flex-direction: column;
  gap: 10px;
  margin-top: 12px;
}
</style>
