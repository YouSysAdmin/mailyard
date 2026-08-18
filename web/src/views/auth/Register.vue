<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { authApi } from '../../api/auth'
import { apiErrorMessage } from '../../api/client'
import { useAuthStore } from '../../stores/auth'
import AuthPage from '../../components/AuthPage.vue'
import { enterConsole } from '../../composables/session'
import FormField from '../../components/FormField.vue'

const router = useRouter()
const auth = useAuthStore()

const email = ref('')
const password = ref('')
const confirm = ref('')
const loading = ref(false)
const error = ref('')
// Set when the install verifies signups by mail: the account exists
// but cannot sign in until the emailed link is clicked.
const awaitingVerification = ref(false)

onMounted(async () => {
  // The route exists in the SPA whatever the server config says, so
  // check the flag and bounce to login when signup is off - otherwise
  // the form would render and every submit would 404.
  try {
    const res = await authApi.info()
    if (!res.data.registration_enabled) router.replace('/login')
  } catch {
    router.replace('/login')
  }
})

async function submit() {
  if (!email.value || !password.value) return
  if (password.value !== confirm.value) {
    error.value = 'Passwords do not match'
    return
  }
  loading.value = true
  error.value = ''
  try {
    const res = await authApi.register(email.value, password.value)
    if (res.data.verification_required) {
      // The account is created but gated on the emailed link - show
      // the check-your-mailbox state instead of navigating.
      awaitingVerification.value = true
      return
    }
    // No verification on this install: register signs the account in,
    // so this lands straight on the dashboard.
    if (res.data.user) auth.setUser(res.data.user)
    enterConsole()
  } catch (e) {
    error.value = apiErrorMessage(e, 'Registration failed')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <AuthPage title="Create your account">
    <div v-if="error" class="alert alert-danger">{{ error }}</div>

    <template v-if="awaitingVerification">
      <p class="auth-note">
        Almost there. We sent a confirmation link to <strong>{{ email }}</strong> - open it to
        finish signing up. The link works once and expires in 24 hours.
      </p>
      <p class="auth-aside">
        Nothing arrived? Check spam, or
        <router-link to="/login">request a new link from the sign-in page</router-link>.
      </p>
    </template>

    <form class="auth-form" v-else @submit.prevent="submit">
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
      <FormField label="Password" for="password">
        <input
          id="password"
          v-model="password"
          type="password"
          class="form-input"
          autocomplete="new-password"
          minlength="8"
          placeholder="Minimum 8 characters"
          required
        />
      </FormField>
      <FormField label="Repeat password" for="confirm">
        <input
          id="confirm"
          v-model="confirm"
          type="password"
          class="form-input"
          autocomplete="new-password"
          minlength="8"
          required
        />
      </FormField>
      <button class="btn btn-primary btn-block" type="submit" :disabled="loading">
        {{ loading ? 'Creating account...' : 'Create account' }}
      </button>
      <p class="auth-aside">
        Already have an account?
        <router-link to="/login">Sign in</router-link>
      </p>
    </form>
  </AuthPage>
</template>
