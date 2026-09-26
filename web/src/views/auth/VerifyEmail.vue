<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { authApi } from '../../api/auth'
import { apiErrorMessage } from '../../api/client'
import AuthPage from '../../components/AuthPage.vue'

const route = useRoute()

const token = (route.query.token as string | undefined) ?? ''
const verifying = ref(true)
const confirmed = ref(false)
const error = ref('')

// The link is the action: redeem it immediately rather than making
// the reader click a second confirm button. A token is single use, so
// a prefetching mail client is the risk - but clients prefetch GETs,
// and the redemption here is a POST fired by the page's script.
onMounted(async () => {
  if (!token) {
    verifying.value = false
    error.value = 'This link is missing its token. Open the most recent message from your mailbox.'
    return
  }
  // Confirming proves the mailbox and nothing else: the password is
  // proved at sign-in, so the link never starts a session.
  try {
    await authApi.verifyEmail(token)
    confirmed.value = true
  } catch (e) {
    error.value = apiErrorMessage(e, 'This verification link is invalid or has expired')
  }
  verifying.value = false
})
</script>

<template>
  <AuthPage title="Confirm your email">
    <template v-if="verifying">
      <p class="auth-note">Checking your link...</p>
    </template>

    <template v-else-if="confirmed">
      <div class="alert alert-success">Your email address is confirmed.</div>
      <p class="auth-note">Sign in with the password you chose to continue.</p>
      <router-link class="btn btn-primary btn-block" to="/login">Go to sign in</router-link>
    </template>

    <template v-else>
      <div class="alert alert-danger">{{ error }}</div>
      <p class="auth-note">
        You can request a fresh link from the sign-in page - enter your email and password, and the
        form will offer to send one.
      </p>
      <router-link class="btn btn-primary btn-block" to="/login">Go to sign in</router-link>
    </template>
  </AuthPage>
</template>
