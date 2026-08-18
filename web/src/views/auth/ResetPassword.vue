<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { authApi } from '../../api/auth'
import { apiErrorMessage } from '../../api/client'
import AuthPage from '../../components/AuthPage.vue'
import FormField from '../../components/FormField.vue'

const route = useRoute()
const router = useRouter()

const token = computed(() => (route.query.token as string | undefined) ?? '')
const password = ref('')
const confirm = ref('')
const loading = ref(false)
const error = ref('')
const done = ref(false)

// Mirrors the server rule (min=12) so the mismatch is caught before a
// round trip.
const tooShort = computed(() => password.value.length > 0 && password.value.length < 12)
const mismatch = computed(() => confirm.value.length > 0 && confirm.value !== password.value)
const canSubmit = computed(
  () => token.value !== '' && password.value.length >= 12 && confirm.value === password.value,
)

async function submit() {
  if (!canSubmit.value) return
  loading.value = true
  error.value = ''
  try {
    await authApi.passwordResetConfirm(token.value, password.value)
    done.value = true
    setTimeout(() => router.push('/login'), 2500)
  } catch (e) {
    error.value = apiErrorMessage(e, 'Could not reset the password')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <AuthPage title="Choose a new password">
    <div v-if="error" class="alert alert-danger">{{ error }}</div>

    <template v-if="done">
      <p class="auth-note">Password updated. Taking you to the sign-in page.</p>
      <router-link class="btn btn-primary btn-block" to="/login">Sign in</router-link>
    </template>

    <template v-else-if="!token">
      <p class="auth-note">
        This link is missing its token. Request a new reset link and open the most recent message.
      </p>
      <router-link class="btn btn-secondary btn-block" to="/forgot-password">
        Request a new link
      </router-link>
    </template>

    <form class="auth-form" v-else @submit.prevent="submit">
      <!-- Both of these say the same thing whether or not the value is
           acceptable, so they are the field's error while it is not and
           its hint while it is - which is what the red hint was. -->
      <FormField
        label="New password"
        for="password"
        hint="At least 12 characters."
        :error="tooShort ? 'At least 12 characters.' : ''"
      >
        <input
          id="password"
          v-model="password"
          type="password"
          class="form-input"
          autocomplete="new-password"
          required
        />
      </FormField>
      <FormField
        label="Confirm password"
        for="confirm"
        :error="mismatch ? 'The two passwords do not match.' : ''"
      >
        <input
          id="confirm"
          v-model="confirm"
          type="password"
          class="form-input"
          autocomplete="new-password"
          required
        />
      </FormField>
      <button class="btn btn-primary btn-block" type="submit" :disabled="loading || !canSubmit">
        {{ loading ? 'Saving...' : 'Set password' }}
      </button>
      <p class="auth-aside">
        <router-link to="/login">Back to sign in</router-link>
      </p>
    </form>
  </AuthPage>
</template>
