<script setup lang="ts">
// The recovery codes, shown once.
//
// The server keeps hashes, so this is the only time anybody can read
// them - which is why the dialog is PERSISTENT, like the API key token:
// Escape or a click outside would cost the reader the only copy.
import BaseModal from '../../components/BaseModal.vue'
import CopyButton from '../../components/CopyButton.vue'
import Notice from '../../components/Notice.vue'

const props = defineProps<{ codes: string[] }>()

defineEmits<{ (e: 'close'): void }>()

const joined = () => props.codes.join('\n')
</script>

<template>
  <BaseModal title="Your recovery codes" persistent>
    <Notice kind="warning" title="Save these now" class="mb-4">
      <p>
        Each code signs you in once in place of your authenticator app. They are stored hashed, so
        this is the only time they can be read. Keep them somewhere that is not the phone.
      </p>
    </Notice>

    <div class="code-block recovery-grid">
      <span v-for="code in codes" :key="code">{{ code }}</span>
    </div>

    <CopyButton
      :value="joined()"
      label="Copy the codes"
      copied-label="Copied"
      variant="btn btn-secondary btn-sm mt-4"
    />

    <template #footer>
      <button class="btn btn-primary" @click="$emit('close')">I saved them</button>
    </template>
  </BaseModal>
</template>

<style scoped>
/* Two columns of monospace codes: ten in one column is a list to scroll,
   five and five is a card to photograph. */
.recovery-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 6px 24px;
  font-variant-numeric: tabular-nums;
  user-select: all;
}
</style>
