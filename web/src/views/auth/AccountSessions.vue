<script setup lang="ts">
// Everywhere this account is currently signed in.
//
// Revoking takes effect on the next request that session makes, so this
// is the control for a laptop left behind - not an audit trail. The
// device column is a guess at the user agent, good enough to answer the
// only question a reader has: which one of these is me.
import { computed, ref } from 'vue'
import { sessionsApi, type UserSession } from '../../api/sessions'
import { apiErrorMessage } from '../../api/client'
import { useNotificationStore } from '../../stores/notification'
import { useConfirm } from '../../composables/useConfirm'
import { formatDate } from '../../composables/formatDate'
import { beginLeaving, leaveConsole } from '../../composables/session'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'

const notify = useNotificationStore()
const { confirm } = useConfirm()

const sessions = ref<UserSession[]>([])
const loading = ref(true)

/** The id being revoked, or 'others' for the bulk action. */
const busy = ref('')

const others = computed(() => sessions.value.filter((s) => !s.current).length)

async function load() {
  loading.value = true
  try {
    sessions.value = (await sessionsApi.list()).data.sessions ?? []
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load the sessions'))
  } finally {
    loading.value = false
  }
}

async function revoke(s: UserSession) {
  const ok = await confirm({
    title: s.current ? 'Sign out here' : 'Revoke this session',
    message: s.current
      ? 'This signs you out on this device, now.'
      : `Sign out ${describe(s)}? That device has to sign in again.`,
    confirmText: s.current ? 'Sign out' : 'Revoke',
    variant: 'danger',
  })
  if (!ok) return

  busy.value = s.id
  try {
    await sessionsApi.revoke(s.id)
    if (s.current) {
      // Our own session: the cookie is already gone server side, so
      // this IS a sign-out and goes the same way - beginLeaving first,
      // or the interceptor reports the interrupted requests as an
      // expired session.
      beginLeaving()
      leaveConsole()

      return
    }

    notify.success('Session revoked')
    await load()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to revoke the session'))
  } finally {
    busy.value = ''
  }
}

async function revokeOthers() {
  const ok = await confirm({
    title: 'Sign out everywhere else',
    message: 'Every other signed-in device has to sign in again. This one stays.',
    confirmText: 'Sign out others',
    variant: 'danger',
  })
  if (!ok) return

  busy.value = 'others'
  try {
    const res = await sessionsApi.revokeOthers()
    notify.success(
      res.data.revoked === 1 ? 'One session signed out' : `${res.data.revoked} sessions signed out`,
    )
    await load()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to sign the other sessions out'))
  } finally {
    busy.value = ''
  }
}

/**
 * A user agent, as something a person recognises.
 *
 * Deliberately crude. It only has to answer "is this me?", and a real
 * parser here would be a dependency and a table of regexes to keep
 * current for a string nobody acts on except by recognising it.
 */
function describe(s: UserSession): string {
  const ua = s.user_agent || ''

  const browser =
    [
      ['Firefox/', 'Firefox'],
      ['Edg/', 'Edge'],
      ['Chrome/', 'Chrome'],
      ['Safari/', 'Safari'],
    ].find(([token]) => ua.includes(token))?.[1] ??
    ua.split(' ')[0] ??
    'Unknown client'

  const os =
    [
      ['Mac OS X', 'macOS'],
      ['Windows', 'Windows'],
      ['Android', 'Android'],
      ['iPhone', 'iOS'],
      ['iPad', 'iOS'],
      ['Linux', 'Linux'],
    ].find(([token]) => ua.includes(token))?.[1] ?? ''

  return os ? `${browser} on ${os}` : browser
}

/** Called by the page when something invalidated the list. */
defineExpose({ reload: load })

void load()
</script>

<template>
  <div class="card">
    <div class="card-header">
      <h2>Signed in</h2>
      <button
        v-if="others > 0"
        class="btn btn-danger btn-sm"
        :disabled="busy === 'others'"
        @click="revokeOthers"
      >
        {{ busy === 'others' ? 'Signing out...' : 'Sign out everywhere else' }}
      </button>
    </div>

    <LoadingBlock v-if="loading" />

    <EmptyState v-else-if="sessions.length === 0" text="No active sessions." />

    <template v-else>
      <div class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th>Device</th>
              <th>Signed in with</th>
              <th>Address</th>
              <th>Since</th>
              <th>Last seen</th>
              <th class="text-right"></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="s in sessions" :key="s.id">
              <td>
                <strong>{{ describe(s) }}</strong>
                <span v-if="s.current" class="badge badge-success ml-2">This device</span>
              </td>
              <td>{{ s.auth_provider_id ? 'Single sign-on' : 'Password' }}</td>
              <td>
                <code>{{ s.ip || '-' }}</code>
              </td>
              <td>{{ formatDate(s.created_at) }}</td>
              <td>{{ formatDate(s.last_seen_at) }}</td>
              <td class="text-right">
                <button class="btn btn-danger btn-sm" :disabled="busy === s.id" @click="revoke(s)">
                  {{ s.current ? 'Sign out' : 'Revoke' }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div class="card-body">
        <p class="text-sm text-muted">
          The address is where that session last connected from, which behind a proxy is the proxy.
          Changing your password signs out everything except this device.
        </p>
      </div>
    </template>
  </div>
</template>
