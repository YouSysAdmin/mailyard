<script setup lang="ts">
// Picking when a campaign starts sending.
//
// One dialog, two callers: the list, where a row's own Schedule button
// opens it, and the detail page.
//
// It holds the value and hands back an ISO string, so neither caller
// carries a `scheduleAt` ref of its own to leave behind after the dialog
// closes.
import { ref } from 'vue'
import BaseModal from '../../components/BaseModal.vue'
import FormField from '../../components/FormField.vue'

const emit = defineEmits<{
  /** The chosen moment, as the datetime-local control gives it. */
  (e: 'schedule', at: string): void
  (e: 'close'): void
}>()

const at = ref('')
</script>

<template>
  <BaseModal title="Schedule Campaign" @close="emit('close')">
    <FormField label="Send At" hint="The campaign starts sending at this time.">
      <input v-model="at" type="datetime-local" class="form-input" />
    </FormField>

    <template #footer>
      <button class="btn btn-secondary" @click="emit('close')">Cancel</button>
      <button class="btn btn-primary" :disabled="!at" @click="emit('schedule', at)">
        Schedule
      </button>
    </template>
  </BaseModal>
</template>
