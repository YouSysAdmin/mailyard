<script setup lang="ts">
// Two-factor authentication: enrol an authenticator app, or turn it off.
//
// Enrolment is two steps and the middle one is the interesting part -
// the server hands out a secret that is NOT yet active, and only a code
// proving the app holds it turns it on. So an abandoned setup leaves
// the account exactly as it was.
import { computed, onMounted, ref, watch } from 'vue'
import QRCode from 'qrcode'
import { authApi } from '../../api/auth'
import { apiErrorMessage } from '../../api/client'
import { useAuthStore } from '../../stores/auth'
import { useNotificationStore } from '../../stores/notification'
import { useConfirm } from '../../composables/useConfirm'
import FormField from '../../components/FormField.vue'
import CopyButton from '../../components/CopyButton.vue'
import RecoveryCodes from './RecoveryCodes.vue'

const auth = useAuthStore()
const notify = useNotificationStore()
const { confirm } = useConfirm()

const on = computed(() => auth.user?.totp_enabled === true)

// Non-null only while an enrolment is in progress on this page. The
// secret is live but inert until a code confirms it.
const pending = ref<{ secret: string; qr: string } | null>(null)
const code = ref('')
const busy = ref(false)

// Recovery codes: the count of unspent ones while the factor is on, a
// fresh set shown once after enabling or regenerating, and the
// password prompt regeneration is gated on.
const remaining = ref<number | null>(null)
const shownCodes = ref<string[] | null>(null)
const regenerating = ref(false)
const password = ref('')

async function loadRemaining() {
  if (!on.value) {
    remaining.value = null
    return
  }

  try {
    const res = await authApi.recoveryCodesStatus()
    remaining.value = res.data.remaining
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Could not read the recovery codes'))
  }
}

async function regenerate() {
  if (!password.value) return

  busy.value = true
  try {
    const res = await authApi.recoveryCodesRegenerate(password.value)
    password.value = ''
    regenerating.value = false
    shownCodes.value = res.data.codes
    await loadRemaining()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Could not generate new codes'))
  } finally {
    busy.value = false
  }
}

function cancelRegenerate() {
  regenerating.value = false
  password.value = ''
}

onMounted(loadRemaining)
watch(on, loadRemaining)

/** Re-read the profile, since totp_enabled is what this page renders. */
async function refresh() {
  const res = await authApi.me()
  if (res.data.user) auth.setUser(res.data.user)
}

async function start() {
  busy.value = true
  try {
    const res = await authApi.totpSetup()
    pending.value = {
      secret: res.data.secret,
      // Drawn in the browser: the otpauth URL carries the secret, so
      // rendering it anywhere else would post it to a third party.
      qr: await QRCode.toDataURL(res.data.otpauth_url, { width: 220, margin: 1 }),
    }
    code.value = ''
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Could not start the setup'))
  } finally {
    busy.value = false
  }
}

async function enable() {
  if (!code.value) return

  busy.value = true
  try {
    const res = await authApi.totpEnable(code.value)
    pending.value = null
    code.value = ''
    notify.success('Two-factor authentication is on')
    // Shown once, here. The dialog is persistent because there is no
    // second chance to read them.
    shownCodes.value = res.data.recovery_codes ?? null
    await refresh()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'That code was not accepted'))
  } finally {
    busy.value = false
  }
}

async function disable() {
  if (!code.value) return

  const ok = await confirm({
    title: 'Turn off two-factor authentication',
    message: 'Signing in will need only your password after this. Continue?',
    confirmText: 'Turn it off',
    variant: 'danger',
  })
  if (!ok) return

  busy.value = true
  try {
    await authApi.totpDisable(code.value)
    code.value = ''
    notify.success('Two-factor authentication is off')
    await refresh()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'That code was not accepted'))
  } finally {
    busy.value = false
  }
}

/** Abandon an enrolment. The secret was never activated, so it dies here. */
function cancel() {
  pending.value = null
  code.value = ''
}
</script>

<template>
  <div class="card">
    <div class="card-header">
      <h2>Two-factor authentication</h2>
      <span class="badge" :class="on ? 'badge-success' : 'badge-neutral'">
        {{ on ? 'on' : 'off' }}
      </span>
    </div>

    <div class="card-body">
      <template v-if="on">
        <p class="note">
          Signing in asks for a code from your authenticator app. To turn that off, enter a current
          code.
        </p>

        <div class="recovery">
          <p class="note">
            Recovery codes sign you in when the authenticator is gone.
            <template v-if="remaining !== null">
              <strong>{{ remaining }}</strong> of 10 left.
            </template>
          </p>
          <button
            v-if="!regenerating"
            class="btn btn-secondary btn-sm"
            :disabled="busy"
            @click="regenerating = true"
          >
            Generate new codes
          </button>
          <form v-else class="code-row" @submit.prevent="regenerate">
            <FormField label="Your password" for="recovery-password">
              <input
                id="recovery-password"
                v-model="password"
                type="password"
                class="form-input"
                autocomplete="current-password"
                required
              />
            </FormField>
            <button class="btn btn-primary" type="submit" :disabled="busy || !password">
              {{ busy ? 'Generating...' : 'Generate' }}
            </button>
            <button
              class="btn btn-secondary"
              type="button"
              :disabled="busy"
              @click="cancelRegenerate"
            >
              Cancel
            </button>
          </form>
        </div>

        <form class="code-row" @submit.prevent="disable">
          <FormField label="Code from your app" for="totp-off">
            <input
              id="totp-off"
              v-model="code"
              class="form-input"
              inputmode="numeric"
              pattern="[0-9]*"
              maxlength="6"
              autocomplete="one-time-code"
              placeholder="123456"
              required
            />
          </FormField>
          <button class="btn btn-danger" type="submit" :disabled="busy || !code">
            {{ busy ? 'Checking...' : 'Turn off' }}
          </button>
        </form>
      </template>

      <template v-else-if="!pending">
        <p class="note">
          A second step at sign-in: a six digit code from an authenticator app on your phone.
        </p>
        <button class="btn btn-primary" :disabled="busy" @click="start">
          {{ busy ? 'Preparing...' : 'Set it up' }}
        </button>
      </template>

      <template v-else>
        <p class="note">Scan this with your authenticator app, or type the secret in by hand.</p>

        <div class="qr">
          <img :src="pending.qr" alt="Setup QR code" />
        </div>

        <div class="secret">
          <code>{{ pending.secret }}</code>
          <CopyButton :value="pending.secret" />
        </div>

        <form class="code-row" @submit.prevent="enable">
          <FormField label="Code from your app" for="totp-on">
            <input
              id="totp-on"
              v-model="code"
              class="form-input"
              inputmode="numeric"
              pattern="[0-9]*"
              maxlength="6"
              autocomplete="one-time-code"
              placeholder="123456"
              required
            />
          </FormField>
          <button class="btn btn-primary" type="submit" :disabled="busy || !code">
            {{ busy ? 'Checking...' : 'Turn on' }}
          </button>
        </form>

        <!-- Nothing is on yet, so leaving costs nothing. Without a way
             out the only exit from a half-finished setup was to leave
             the page. -->
        <button class="btn btn-secondary btn-sm" :disabled="busy" @click="cancel">Cancel</button>
      </template>
    </div>

    <RecoveryCodes v-if="shownCodes" :codes="shownCodes" @close="shownCodes = null" />
  </div>
</template>

<style scoped>
.note {
  margin-bottom: 14px;
  color: var(--text-tertiary);
  font-size: 13px;
}

.qr {
  display: flex;
  justify-content: center;
  margin-bottom: 14px;
}

/* White behind it whatever the theme: a QR code is read by a camera,
   and inverting one stops most scanners cold. */
.qr img {
  width: 220px;
  height: 220px;
  border: 1px solid var(--border-primary);
  border-radius: var(--radius);
  background: #fff;
}

.secret {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 16px;
}

/* Breaks anywhere: a base32 secret has no spaces and would otherwise
   push the card open. */
.secret code {
  flex: 1;
  padding: 8px 10px;
  border-radius: var(--radius);
  background: var(--bg-tertiary);
  color: var(--text-primary);
  font-size: 12px;
  word-break: break-all;
}

/* The recovery block sits above the turn-off form with room between
   them: two forms stacked tight read as one. */
.recovery {
  padding-bottom: 16px;
  margin-bottom: 16px;
  border-bottom: 1px solid var(--border-primary);
}

/* The field and its button on one line, aligned on their bottoms so the
   button sits level with the input rather than with the label. */
.code-row {
  display: flex;
  align-items: flex-end;
  gap: 12px;
}

.code-row .form-group {
  flex: 1;
  margin-bottom: 0;
}
</style>
