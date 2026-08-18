<script setup lang="ts">
// The token, shown once.
//
// The server hashes it and keeps only a prefix, so this is genuinely
// the last time anybody can read it - which is why the dialog is
// PERSISTENT. Every other dialog in the console closes on Escape or a
// click outside, and here that gesture costs the reader the only copy.
import type { APIKey } from '../../api/types'
import BaseModal from '../../components/BaseModal.vue'
import CopyButton from '../../components/CopyButton.vue'
import { formatDate } from '../../composables/formatDate'
import Notice from '../../components/Notice.vue'

defineProps<{ token: string; apiKey: APIKey | null }>()

defineEmits<{ (e: 'close'): void }>()
</script>

<template>
  <BaseModal title="Your new API key" persistent>
    <Notice kind="warning" title="Copy this now" class="mb-4">
      <p>
        It is stored hashed, so this is the only time it can be read. Put it in a secret manager.
      </p>
    </Notice>

    <div class="code-block">{{ token }}</div>

    <p class="text-sm text-muted mt-2">
      {{
        apiKey?.expires_at ? `Expires ${formatDate(apiKey.expires_at)}.` : 'This key never expires.'
      }}
    </p>

    <CopyButton
      :value="token"
      label="Copy the key"
      copied-label="Copied"
      variant="btn btn-secondary btn-sm mt-4"
    />

    <template #footer>
      <button class="btn btn-primary" @click="$emit('close')">Done</button>
    </template>
  </BaseModal>
</template>
