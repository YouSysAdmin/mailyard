<script setup lang="ts">
import { nextTick, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { authApi, type AuthInfo, type LoginProvider } from '../../api/auth'
import * as webauthn from '../../api/webauthn'
import { apiErrorMessage } from '../../api/client'
import { useAuthStore } from '../../stores/auth'
import { safeReturnPath } from '../../composables/useReturnPath'
import AuthPage from '../../components/AuthPage.vue'
import { enterConsole, enterInvitation } from '../../composables/session'
import FormField from '../../components/FormField.vue'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const email = ref('')
const password = ref('')
const totpCode = ref('')
const requires2fa = ref(false)
const totpInput = ref<HTMLInputElement | null>(null)
const loading = ref(false)
const error = ref('')
// The account exists but has not confirmed its signup link yet -
// offer a resend instead of a bare failure.
const needsVerification = ref(false)
const resendLoading = ref(false)
const resendMessage = ref('')
const info = ref<AuthInfo | null>(null)
const passkeyLoading = ref(false)
// Both have to be true: the install offers passkeys and this browser
// can run the ceremony. Resolved once on mount so the button does not
// flicker in.
const passkeyReady = ref(false)

const ssoErrors: Record<string, string> = {
  sso_idp_error: 'The identity provider reported an error',
  sso_bad_callback: 'The SSO callback was malformed',
  sso_state_missing: 'The SSO state expired, please try again',
  sso_token_invalid: 'The SSO token could not be verified',
  sso_access_denied: 'Your account is not allowed to sign in',
  sso_unknown_provider: 'That sign-in provider is not available',
  sso_provider_error: 'The sign-in provider is misconfigured, contact an administrator',
  sso_state_mismatch: 'The sign-in attempt did not match, please try again',
  sso_no_account: 'No account exists for that identity, ask an administrator for access',
}

onMounted(async () => {
  const qErr = (route.query.error || route.query.err) as string | undefined
  if (qErr) error.value = ssoErrors[qErr] || qErr
  try {
    const res = await authApi.info()
    info.value = res.data
    if (res.data.auth_disabled) {
      auth.authDisabled = true
      router.replace('/')
      return
    }
    passkeyReady.value = res.data.passkeys_enabled === true && (await webauthn.available())
  } catch {
    info.value = { local_enabled: true }
  }
})

// afterSignIn is the shared tail of both sign-in routes: honour the
// ?next= a gated page outside the SPA sent the reader here with, and
// otherwise land on the dashboard.

function afterSignIn() {
  // An invitation wins over ?next=, because it is the reason this person
  // is signing in at all. Built here rather than trusted from the query,
  // for the same reason the server validates the token: the destination
  // is ours, only the token varies.
  const invite = (route.query.invite as string) || ''
  if (invite) {
    enterInvitation(invite)

    return
  }

  // A document boundary, not a route change - see composables/session.ts
  // for what survived the old router.push and who it belonged to.
  enterConsole(safeReturnPath(route.query.next))
}

async function passkeyLogin() {
  passkeyLoading.value = true
  error.value = ''
  try {
    const begin = await authApi.passkeyLoginBegin()
    const assertion = await webauthn.getCredential(begin.data)
    const res = await authApi.passkeyLoginFinish(assertion)
    auth.setUser(res.data.user)
    afterSignIn()
  } catch (e) {
    // A rejected request carries a response, a dismissed browser
    // prompt does not.
    const fromServer = (e as { response?: unknown }).response
    error.value = fromServer
      ? apiErrorMessage(e, 'Passkey sign-in failed')
      : webauthn.ceremonyErrorMessage(e, 'Passkey sign-in failed')
  } finally {
    passkeyLoading.value = false
  }
}

async function submit() {
  if (!email.value || !password.value) return
  if (requires2fa.value && !totpCode.value) return
  loading.value = true
  error.value = ''
  try {
    await auth.login(email.value, password.value, totpCode.value || undefined)
    // A gated page outside the SPA (currently /docs) sends the reader
    // here with where they were trying to go. Only same-origin
    // absolute paths are honored, so the parameter cannot be used to
    // bounce somebody to another site after they authenticate.
    afterSignIn()
  } catch (e: unknown) {
    const data = (
      e as { response?: { data?: { requires_2fa?: boolean; requires_verification?: boolean } } }
    ).response?.data
    if (data?.requires_2fa === true) {
      // The account has 2FA - reveal the code field and ask again.
      if (!requires2fa.value) {
        requires2fa.value = true
        await nextTick()
        totpInput.value?.focus()
      } else {
        error.value = apiErrorMessage(e, 'Sign in failed')
      }
    } else if (data?.requires_verification === true) {
      needsVerification.value = true
      resendMessage.value = ''
      error.value = ''
    } else {
      error.value = apiErrorMessage(e, 'Sign in failed')
    }
  } finally {
    loading.value = false
  }
}

async function resendVerification() {
  if (!email.value) return
  resendLoading.value = true
  try {
    const res = await authApi.verifyEmailResend(email.value)
    resendMessage.value = res.data.message || 'A new link is on its way.'
  } catch (e) {
    resendMessage.value = apiErrorMessage(e, 'Could not send a new link')
  } finally {
    resendLoading.value = false
  }
}

// An invitation ridden in on ?invite= is handed to the start leg, which
// keeps it in the signed state cookie and lands the callback back on the
// invitation. Without it a person with no account had to return to their
// email and click the link a second time - and since a project is
// reached only by invitation, that was the whole first run.
//
// The server accepts a 64-hex token and nothing else, so passing this
// through cannot steer where the sign-in ends up.
function ssoLogin(p: LoginProvider) {
  const url = new URL(authApi.ssoStartURL(p), window.location.origin)
  const invite = (route.query.invite as string) || ''
  if (invite) {
    url.searchParams.set('invite', invite)
  }
  window.location.href = url.toString()
}
</script>

<template>
  <AuthPage title="Sign in to Mailyard">
    <div v-if="error" class="alert alert-danger">{{ error }}</div>

    <div v-if="needsVerification" class="alert alert-warning">
      <p class="m-0 mb-2">Confirm your email address first - check your mailbox for the link.</p>
      <p class="m-0" v-if="resendMessage">{{ resendMessage }}</p>
      <button
        v-else
        class="btn btn-secondary btn-sm"
        type="button"
        :disabled="resendLoading"
        @click="resendVerification"
      >
        {{ resendLoading ? 'Sending...' : 'Send a new link' }}
      </button>
    </div>

    <form class="auth-form" v-if="info?.local_enabled !== false" @submit.prevent="submit">
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
          autocomplete="current-password"
          required
        />
      </FormField>
      <FormField v-if="requires2fa" label="Authenticator or recovery code" for="totp">
        <input
          id="totp"
          ref="totpInput"
          v-model="totpCode"
          type="text"
          class="form-input"
          maxlength="19"
          autocomplete="one-time-code"
          placeholder="123456 or xxxx-xxxx-xxxx-xxxx"
          required
        />
      </FormField>
      <button class="btn btn-primary btn-block" type="submit" :disabled="loading">
        {{ loading ? 'Signing in...' : 'Sign in' }}
      </button>
      <p v-if="info?.password_reset_enabled" class="auth-aside">
        <router-link to="/forgot-password">Forgot your password?</router-link>
      </p>
      <p v-if="info?.registration_enabled" class="auth-aside">
        No account yet?
        <router-link to="/register">Create one</router-link>
      </p>
    </form>

    <template v-if="passkeyReady || info?.providers?.length">
      <div v-if="info?.local_enabled !== false" class="auth-or"><span>or</span></div>
      <button
        v-if="passkeyReady"
        class="btn btn-secondary btn-block mb-2"
        type="button"
        :disabled="passkeyLoading"
        @click="passkeyLogin"
      >
        {{ passkeyLoading ? 'Waiting for your passkey...' : 'Sign in with a passkey' }}
      </button>
      <button
        v-for="p in info?.providers ?? []"
        :key="p.slug"
        class="btn btn-secondary btn-block mb-2"
        type="button"
        @click="ssoLogin(p)"
      >
        Continue with {{ p.name }}
      </button>
    </template>
  </AuthPage>
</template>
