<script setup lang="ts">
// Change the account password.
//
// A card and a dialog, and the one thing the page around it has to know
// afterwards: the server signs every OTHER session out on a password
// change, so the session list is stale the moment this succeeds.
import { ref } from 'vue'
import { authApi } from '../../api/auth'
import { apiErrorMessage } from '../../api/client'
import { useNotificationStore } from '../../stores/notification'
import { useFieldErrors } from '../../composables/fieldErrors'
import BaseModal from '../../components/BaseModal.vue'
import FormField from '../../components/FormField.vue'
import PasswordManagerHint from '../../components/PasswordManagerHint.vue'

/** The minimum the server enforces, stated here so the form can too. */
const MIN_LENGTH = 12

defineProps<{ email?: string }>()

const emit = defineEmits<{
  /** The password changed, so every other session is now gone. */
  (e: 'changed'): void
}>()

const notify = useNotificationStore()
const { errors, capture, clear } = useFieldErrors()

const form = ref<{ current: string; next: string } | null>(null)
const busy = ref(false)

function open() {
  clear()
  form.value = { current: '', next: '' }
}

async function submit() {
  const f = form.value
  if (!f || busy.value) return

  clear()
  busy.value = true
  try {
    await authApi.changePassword(f.current, f.next)
    form.value = null
    notify.success('Password changed. Every other session was signed out.')
    emit('changed')
  } catch (e) {
    if (!capture(e)) notify.error(apiErrorMessage(e, 'Failed to change the password'))
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="card">
    <div class="card-header">
      <h2>Password</h2>
    </div>

    <div class="card-body">
      <p class="text-sm text-muted mb-4">
        Changing it signs out every other session. This one stays.
      </p>
      <button class="btn btn-secondary" @click="open">Change password</button>
    </div>

    <BaseModal v-if="form" title="Change password" form @submit="submit" @close="form = null">
      <PasswordManagerHint :email="email" />

      <FormField label="Current password" for="current-password" :error="errors.current_password">
        <input
          id="current-password"
          v-model="form.current"
          type="password"
          class="form-input"
          autocomplete="current-password"
          required
        />
      </FormField>

      <FormField
        label="New password"
        for="new-password"
        :error="errors.password"
        hint="At least {{ MIN_LENGTH }} characters."
      >
        <input
          id="new-password"
          v-model="form.next"
          type="password"
          class="form-input"
          autocomplete="new-password"
          :minlength="MIN_LENGTH"
          required
        />
      </FormField>

      <template #footer>
        <button type="button" class="btn btn-secondary" @click="form = null">Cancel</button>
        <button
          type="submit"
          class="btn btn-primary"
          :disabled="busy || !form.current || form.next.length < MIN_LENGTH"
        >
          {{ busy ? 'Saving...' : 'Change password' }}
        </button>
      </template>
    </BaseModal>
  </div>
</template>
