<script setup lang="ts">
import { ref } from 'vue'
import { authApi } from '../../api/auth'
import { apiErrorMessage } from '../../api/client'
import AuthPage from '../../components/AuthPage.vue'
import FormField from '../../components/FormField.vue'

const email = ref('')
const loading = ref(false)
const error = ref('')
// The server answers the same way whether or not the address has an
// account, so the view shows that answer rather than a success state
// that would confirm the address exists.
const sent = ref(false)

async function submit() {
  if (!email.value) return
  loading.value = true
  error.value = ''
  try {
    await authApi.passwordResetRequest(email.value)
    sent.value = true
  } catch (e) {
    error.value = apiErrorMessage(e, 'Could not start the reset')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <AuthPage title="Reset your password">
    <div v-if="error" class="alert alert-danger">{{ error }}</div>

    <template v-if="sent">
      <p class="auth-note">
        If that address has an account, a reset link is on its way. The link expires in 30 minutes
        and can be used once.
      </p>
      <router-link class="btn btn-secondary btn-block" to="/login">Back to sign in</router-link>
    </template>

    <form class="auth-form" v-else @submit.prevent="submit">
      <p class="auth-note">
        Enter the address you sign in with and we will email you a link to choose a new password.
      </p>
      <FormField label="Email" for="email">
        <input
          id="email"
          v-model="email"
          type="email"
          class="form-input"
          autocomplete="username"
          required
        />
      </FormField>
      <button class="btn btn-primary btn-block" type="submit" :disabled="loading">
        {{ loading ? 'Sending...' : 'Send reset link' }}
      </button>
      <p class="auth-aside">
        <router-link to="/login">Back to sign in</router-link>
      </p>
    </form>
  </AuthPage>
</template>
