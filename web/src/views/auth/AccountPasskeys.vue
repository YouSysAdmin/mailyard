<script setup lang="ts">
// Passkeys registered against this account.
//
// A passkey signs in ON ITS OWN - no password, no code - so both adding
// and removing one asks for the account password first. Changing how an
// account can be entered should not be something a borrowed session can
// do quietly.
import { ref } from 'vue'
import { authApi, type Passkey } from '../../api/auth'
import * as webauthn from '../../api/webauthn'
import { apiErrorMessage } from '../../api/client'
import { useNotificationStore } from '../../stores/notification'
import { formatDate } from '../../composables/formatDate'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import BaseModal from '../../components/BaseModal.vue'
import FormField from '../../components/FormField.vue'
import PasswordManagerHint from '../../components/PasswordManagerHint.vue'

defineProps<{ email?: string }>()

const notify = useNotificationStore()

const keys = ref<Passkey[]>([])
const loading = ref(true)

/**
 * Whether the SERVER will hold one for this account.
 *
 * It answers 403 for an account an identity provider owns. Recorded
 * rather than assumed, so the card can say why instead of offering an
 * Add button that could only ever fail.
 */
const offered = ref(false)
const refusal = ref('')

/** Whether this BROWSER can run the ceremony at all. */
const usable = webauthn.supported()

// Add and remove differ by one field and one verb, so they are one
// dialog. null means closed.
const dialog = ref<{
  mode: 'add' | 'remove'
  target?: Passkey
  name: string
  password: string
} | null>(null)
const busy = ref(false)

async function load() {
  loading.value = true
  try {
    keys.value = (await authApi.passkeyList()).data.passkeys ?? []
    offered.value = true
  } catch (e) {
    const status = (e as { response?: { status?: number } }).response?.status
    if (status === 403) {
      offered.value = false
      refusal.value = apiErrorMessage(e, 'Passkeys are not available for this account')

      return
    }

    notify.error(apiErrorMessage(e, 'Failed to load the passkeys'))
  } finally {
    loading.value = false
  }
}

function open(mode: 'add' | 'remove', target?: Passkey) {
  dialog.value = { mode, target, name: '', password: '' }
}

async function submit() {
  const d = dialog.value
  if (!d || !d.password) return

  busy.value = true
  try {
    if (d.mode === 'add') {
      // The password is checked BEFORE the browser prompt, so a wrong
      // one fails without anybody being asked to touch a sensor.
      const begin = await authApi.passkeyRegisterBegin(d.password)
      const credential = await webauthn.createCredential(begin.data)
      await authApi.passkeyRegisterFinish(d.name.trim() || 'Passkey', credential)
      notify.success('Passkey added')
    } else if (d.target) {
      await authApi.passkeyDelete(d.target.id, d.password)
      notify.success('Passkey removed')
    }

    dialog.value = null
    await load()
  } catch (e) {
    // A refused request carries a response, a dismissed browser prompt
    // does not - and the two need very different wording.
    const fromServer = (e as { response?: unknown }).response
    notify.error(
      fromServer
        ? apiErrorMessage(e, 'The passkey could not be saved')
        : webauthn.ceremonyErrorMessage(e, 'The passkey could not be saved'),
    )
  } finally {
    busy.value = false
  }
}

void load()
</script>

<template>
  <div class="card">
    <div class="card-header">
      <h2>Passkeys</h2>
      <button v-if="offered && usable" class="btn btn-primary btn-sm" @click="open('add')">
        Add a passkey
      </button>
    </div>

    <LoadingBlock v-if="loading" />

    <div v-else-if="!offered" class="card-body">
      <p class="text-sm text-muted">{{ refusal }}</p>
    </div>

    <div v-else-if="!usable" class="card-body">
      <p class="text-sm text-muted">This browser cannot use passkeys.</p>
    </div>

    <template v-else>
      <EmptyState v-if="keys.length === 0" title="No passkeys yet">
        <p>
          A passkey signs you in with your fingerprint, face or device PIN, and cannot be phished
          the way a password and a code can.
        </p>
      </EmptyState>

      <template v-else>
        <div class="table-wrapper">
          <table>
            <thead>
              <tr>
                <th>Name</th>
                <th>Added</th>
                <th>Last used</th>
                <th class="text-right"></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="k in keys" :key="k.id">
                <td>
                  <strong>{{ k.name }}</strong>
                </td>
                <td>{{ formatDate(k.created_at) }}</td>
                <td>{{ k.last_used_at ? formatDate(k.last_used_at) : 'Never' }}</td>
                <td class="text-right">
                  <button class="btn btn-danger btn-sm" :disabled="busy" @click="open('remove', k)">
                    Remove
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <div class="card-body">
          <p class="text-sm text-muted">
            Each of these signs you in on its own. Adding or removing one asks for your password.
          </p>
        </div>
      </template>
    </template>

    <BaseModal
      v-if="dialog"
      :title="dialog.mode === 'add' ? 'Add a passkey' : 'Remove a passkey'"
      form
      @submit="submit"
      @close="dialog = null"
    >
      <FormField
        v-if="dialog.mode === 'add'"
        label="Name"
        for="passkey-name"
        hint="So you can tell it apart later. Defaults to Passkey."
      >
        <input
          id="passkey-name"
          v-model="dialog.name"
          class="form-input"
          placeholder="MacBook Touch ID"
          maxlength="60"
        />
      </FormField>

      <p v-else class="text-sm">
        <strong>{{ dialog.target?.name }}</strong> will no longer be able to sign in.
      </p>

      <PasswordManagerHint :email="email" />

      <FormField label="Your password" for="passkey-password" hint="Confirms it is you.">
        <input
          id="passkey-password"
          v-model="dialog.password"
          type="password"
          class="form-input"
          autocomplete="current-password"
          required
        />
      </FormField>

      <template #footer>
        <button type="button" class="btn btn-secondary" @click="dialog = null">Cancel</button>
        <button
          type="submit"
          class="btn"
          :class="dialog.mode === 'add' ? 'btn-primary' : 'btn-danger'"
          :disabled="busy || !dialog.password"
        >
          {{ busy ? 'Working...' : dialog.mode === 'add' ? 'Continue' : 'Remove' }}
        </button>
      </template>
    </BaseModal>
  </div>
</template>
