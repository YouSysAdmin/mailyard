<script setup lang="ts">
// Editing one account, from the platform side.
//
// FOUR kinds of thing in one dialog and they are not variations on each
// other: fields saved together by Save, three account actions that take
// effect the moment they are pressed, and a read-only list of the
// projects this person belongs to.
//
// A PLATFORM ADMIN CANNOT DISARM THEMSELVES. The admin flag, the
// disabled flag, 2FA and passkeys are all refused on your own row, and
// the dialog says so rather than looking broken. The server refuses the
// same way - this is so nobody presses it and wonders.
//
// It owns none of the LIST. Every act here says `changed` and the page
// re-reads, because the row this dialog is showing is the page's row.
import { ref, watch } from 'vue'
import { usersApi } from '../../api/users'
import { apiErrorMessage } from '../../api/client'
import type { Project, User } from '../../api/types'
import { useAuthStore } from '../../stores/auth'
import { useNotificationStore } from '../../stores/notification'
import { useConfirm } from '../../composables/useConfirm'
import { useFieldErrors } from '../../composables/fieldErrors'
import BaseModal from '../../components/BaseModal.vue'
import FormField from '../../components/FormField.vue'

const props = defineProps<{ user: User }>()

const emit = defineEmits<{
  /** Something about this account changed - re-read the list. */
  (e: 'changed'): void
  (e: 'close'): void
}>()

const auth = useAuthStore()
const notify = useNotificationStore()
const { confirm } = useConfirm()
const { errors, capture, clear } = useFieldErrors()

const saving = ref(false)
const resettingTOTP = ref(false)
const resettingPasskeys = ref(false)
const revokingSessions = ref(false)

const form = ref({ password: '', admin: false, disabled: false, email_verified: true })
const projects = ref<Project[]>([])

/** Your own row. Everything that could lock you out is refused on it. */
function isSelf(u: User): boolean {
  return u.id === auth.user?.id
}

watch(
  () => props.user,
  (u) => {
    clear()
    form.value = {
      // Never prefilled - the server does not return it, and empty on
      // an edit means "keep what is stored".
      password: '',
      admin: u.admin,
      disabled: u.disabled,
      email_verified: u.email_verified !== false,
    }

    projects.value = []
    usersApi
      .projects(u.id)
      .then((res) => {
        // The dialog may have moved to another user while this was in
        // flight - only apply the answer that matches.
        if (props.user.id === u.id) projects.value = res.data.projects ?? []
      })
      .catch(() => {
        // Memberships are informational, the dialog works without them.
      })
  },
  { immediate: true },
)

async function save() {
  clear()
  if (form.value.password && form.value.password.length < 8) {
    notify.error('Password must be at least 8 characters')

    return
  }

  saving.value = true
  try {
    // Self-edits may only change the PASSWORD - the backend refuses
    // changes to your own admin or disabled flags, so sending them
    // would turn a save into a refusal.
    const payload: Parameters<typeof usersApi.update>[1] = {}
    if (form.value.password) payload.password = form.value.password
    if (!isSelf(props.user)) {
      payload.admin = form.value.admin
      payload.disabled = form.value.disabled
      payload.email_verified = form.value.email_verified
    }

    await usersApi.update(props.user.id, payload)
    notify.success('User updated')
    emit('changed')
    emit('close')
  } catch (err) {
    if (!capture(err)) notify.error(apiErrorMessage(err, 'Failed to update user'))
  } finally {
    saving.value = false
  }
}

// The three below take effect the moment they are pressed, which is why
// each asks first: there is no Save to reconsider at.
async function resetPasskeys(u: User) {
  clear()
  const ok = await confirm({
    title: 'Reset passkeys',
    message: `Remove every passkey from "${u.email}"? They will have to sign in with their password and enrol again.`,
    confirmText: 'Reset passkeys',
    variant: 'danger',
  })
  if (!ok) return

  resettingPasskeys.value = true
  try {
    const res = await usersApi.resetPasskeys(u.id)
    const n = res.data.removed
    notify.success(`${n} passkey${n === 1 ? '' : 's'} removed`)
    // The row carries the count, so the page re-reads rather than this
    // guessing at the new one.
    emit('changed')
  } catch (err) {
    if (!capture(err)) notify.error(apiErrorMessage(err, 'Failed to reset passkeys'))
  } finally {
    resettingPasskeys.value = false
  }
}

async function resetTOTP(u: User) {
  clear()
  const ok = await confirm({
    title: 'Reset 2FA',
    message: `Remove two-factor auth from "${u.email}"? They can sign in with just their password until they enroll again.`,
    confirmText: 'Reset 2FA',
    variant: 'danger',
  })
  if (!ok) return

  resettingTOTP.value = true
  try {
    await usersApi.resetTOTP(u.id)
    notify.success('Two-factor auth removed')
    emit('changed')
  } catch (err) {
    if (!capture(err)) notify.error(apiErrorMessage(err, 'Failed to reset 2FA'))
  } finally {
    resettingTOTP.value = false
  }
}

// Allowed on your OWN account, unlike the two above: signing yourself
// out everywhere is undone by signing back in.
async function revokeSessions(u: User) {
  const ok = await confirm({
    title: 'Revoke Sessions',
    message: `Sign "${u.email}" out everywhere? Every session they hold stops working immediately.`,
    confirmText: 'Revoke',
    variant: 'danger',
  })
  if (!ok) return

  revokingSessions.value = true
  try {
    const res = await usersApi.revokeSessions(u.id)
    notify.success(`Revoked ${res.data.revoked} session(s)`)
  } catch (err) {
    notify.error(apiErrorMessage(err, 'Failed to revoke sessions'))
  } finally {
    revokingSessions.value = false
  }
}
</script>

<template>
  <BaseModal title="Edit User" form @submit="save" @close="emit('close')">
    <FormField label="Email">
      <input :value="user.email" class="form-input" disabled />
    </FormField>
    <FormField label="New Password (optional)" :error="errors.password">
      <input
        v-model="form.password"
        type="password"
        class="form-input"
        placeholder="Leave empty to keep the current password"
        minlength="8"
      />
    </FormField>
    <FormField>
      <label class="checkbox-label">
        <input v-model="form.admin" type="checkbox" :disabled="isSelf(user)" />
        <span>Platform administrator</span>
      </label>
    </FormField>
    <FormField>
      <label class="checkbox-label">
        <input v-model="form.disabled" type="checkbox" :disabled="isSelf(user)" />
        <span>Disabled (the user cannot sign in)</span>
      </label>
    </FormField>
    <FormField v-if="user.email_verified === false">
      <label class="checkbox-label">
        <input v-model="form.email_verified" type="checkbox" />
        <span>Email verified (unstick an account whose link never arrived)</span>
      </label>
    </FormField>
    <p v-if="isSelf(user)" class="form-hint">
      You cannot change your own administrator or disabled flags.
    </p>

    <FormField label="Account actions">
      <div class="account-actions-row">
        <button
          type="button"
          class="btn btn-secondary btn-sm"
          :disabled="isSelf(user) || !user.totp_enabled || resettingTOTP"
          :title="
            isSelf(user)
              ? 'Disable your own 2FA from your profile'
              : user.totp_enabled
                ? 'Remove this user\'s second factor'
                : '2FA is not enabled for this user'
          "
          @click="resetTOTP(user)"
        >
          {{ resettingTOTP ? 'Resetting...' : 'Reset 2FA' }}
        </button>
        <button
          type="button"
          class="btn btn-secondary btn-sm"
          :disabled="isSelf(user) || !user.passkey_count || resettingPasskeys"
          :title="
            isSelf(user)
              ? 'Remove your own passkeys from your profile'
              : user.passkey_count
                ? 'Remove every passkey on this account'
                : 'This user has no passkeys'
          "
          @click="resetPasskeys(user)"
        >
          {{ resettingPasskeys ? 'Resetting...' : 'Reset passkeys' }}
        </button>
        <button
          type="button"
          class="btn btn-secondary btn-sm"
          :disabled="revokingSessions"
          @click="revokeSessions(user)"
        >
          {{ revokingSessions ? 'Revoking...' : 'Revoke all sessions' }}
        </button>
      </div>
    </FormField>

    <FormField v-if="projects.length" label="Projects">
      <div class="proj-list">
        <span v-for="w in projects" :key="w.id" class="badge badge-neutral">
          {{ w.name }}
        </span>
      </div>
    </FormField>
    <template #footer>
      <button type="button" class="btn btn-secondary" @click="emit('close')">Cancel</button>
      <button type="submit" class="btn btn-primary" :disabled="saving">
        {{ saving ? 'Saving...' : 'Save' }}
      </button>
    </template>
  </BaseModal>
</template>

<style scoped>
/* Three buttons on one line. They are actions, not fields, so they sit
   together rather than stacking like the controls above them. */
.account-actions-row {
  display: flex;
  gap: 8px;
}

/* Badges, wrapping - an account can belong to a dozen projects and the
   list is a fact rather than something to scan down. */
.proj-list {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
</style>
