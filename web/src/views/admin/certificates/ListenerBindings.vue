<script setup lang="ts">
// Which certificate each listener presents.
//
// Two columns that look alike and are not: Certificate is the INTENTION
// recorded in settings, and Serving is what is on the wire now. They
// differ in exactly the cases worth seeing - a listener with TLS off
// presents nothing whatever is assigned, and an assignment naming a row
// that has been deleted falls through with a warning rather than taking
// the listener down.
import { ref } from 'vue'
import type { ListenerState, ManagedCertificate } from '../../../api/certificates'
import { settingsApi } from '../../../api/settings'
import { apiErrorMessage } from '../../../api/client'
import { useNotificationStore } from '../../../stores/notification'

const props = defineProps<{
  /** Keyed by listener, so a row is read without searching an array. */
  listeners: Record<string, ListenerState>
  /** What may be assigned - authorities are not among them. */
  assignable: ManagedCertificate[]
}>()

const emit = defineEmits<{ (e: 'changed'): void }>()

const notify = useNotificationStore()

const saving = ref(false)

/** The three listeners, named as the server names them. */
const LISTENERS = [
  { key: 'server', label: 'HTTP', setting: 'tls_certificate_server' },
  { key: 'submission', label: 'SMTP submission', setting: 'tls_certificate_submission' },
  { key: 'inbound', label: 'Inbound MX', setting: 'tls_certificate_inbound' },
]

/**
 * The label on the unassigned option, which is the whole fix: it names
 * what falls out of the chain instead of describing the chain.
 *
 * "Nothing assigned (ACME, then self-signed)" was mechanically true and
 * mentioned no certificate, so a Let's Encrypt certificate that had been
 * ordered, issued, cached and put on the wire appeared in no selector on
 * this page. The operator's reading - that it was not being used - was
 * the only one the page supported.
 */
function automatic(key: string): string {
  const st = props.listeners[key]
  if (!st) return 'Automatic'

  // A listener with no handshake presents nothing, so naming what the
  // chain WOULD resolve to would be promising a certificate it will not
  // serve. The Serving column says why.
  if (!st.tls) return 'Automatic'

  const names = (st.fallback_names ?? []).join(', ')
  if (st.fallback === 'acme') {
    return names ? `Automatic - Let's Encrypt: ${names}` : "Automatic - Let's Encrypt"
  }

  return names ? `Automatic - self-signed: ${names}` : 'Automatic - self-signed'
}

/** What is on the wire for this listener. */
function serving(key: string): string {
  const st = props.listeners[key]
  if (!st) return ''

  const names = (st.serving_names ?? []).join(', ')
  switch (st.serving) {
    case 'none':
      return 'nothing - TLS is off here'
    case 'managed':
      return names
    case 'acme':
      // Per SNI, so the rest of the names are not covered by it. Said
      // here because an MX answers for names ACME was never asked for.
      return `Let's Encrypt: ${names}, self-signed for any other name`
    default:
      return `self-signed: ${names}`
  }
}

/**
 * An assignment that is recorded and not in force.
 *
 * Worth flagging: the Certificate column would otherwise read as if it
 * were being served.
 */
function dormant(key: string): boolean {
  const st = props.listeners[key]

  return !!st && !!st.assigned && st.serving !== 'managed'
}

async function assign(setting: string, name: string) {
  saving.value = true
  try {
    await settingsApi.update([{ key: setting, value: name }])
    notify.success(name ? `Assigned ${name}` : 'Back to the configured certificate')
    emit('changed')
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to assign the certificate'))
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="card">
    <div class="card-header">
      <div>
        <h2>Listeners</h2>
        <p class="text-sm text-muted">
          What each listener serves. Changing this takes effect without a restart, and Serving is
          what is on the wire now - a listener always has something to present, so Automatic is a
          working answer rather than an empty one.
        </p>
      </div>
    </div>

    <div class="table-wrapper">
      <table>
        <thead>
          <tr>
            <th>Listener</th>
            <!-- Its own column rather than a badge after the name. The
                 names differ in length, so a trailing badge sat at a
                 different offset on every row. -->
            <th class="col-tls">TLS</th>
            <th>Certificate</th>
            <!-- The column the page was missing. Everything else here
                 describes an INTENTION, and the one thing an operator
                 came to check is which certificate a client gets. -->
            <th>Serving</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="l in LISTENERS" :key="l.key">
            <td>
              <strong>{{ l.label }}</strong>
            </td>

            <td class="col-tls">
              <span
                v-if="listeners[l.key] && !listeners[l.key].tls"
                class="tls-off"
                :title="`${l.label} does not terminate TLS, so an assignment here is recorded and nothing presents it`"
              >
                Off
              </span>
              <span v-else class="tls-on">On</span>
            </td>

            <td>
              <select
                class="form-select"
                :value="listeners[l.key]?.assigned ?? ''"
                :disabled="saving"
                :aria-label="`Certificate for the ${l.label} listener`"
                @change="assign(l.setting, ($event.target as HTMLSelectElement).value)"
              >
                <!-- Automatic is the ACME-then-self-signed fallback, and
                     it NAMES what that currently resolves to. Neither is
                     an option of its own: ACME is resolved per handshake
                     from the SNI name, so pinning one host's certificate
                     to a listener would present it for every other name
                     too, and the self-signed pair is what Automatic
                     already ends at. -->
                <option value="">{{ automatic(l.key) }}</option>
                <option v-for="c in assignable" :key="c.name" :value="c.name">{{ c.name }}</option>
              </select>
            </td>

            <td class="text-sm">
              <span :class="listeners[l.key]?.serving === 'none' ? 'text-muted' : ''">
                {{ serving(l.key) }}
              </span>
              <div v-if="dormant(l.key)" class="text-xs text-muted">
                {{ listeners[l.key].assigned }} is assigned and not being served
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<style scoped>
/* Narrow enough to read as a status column and not to push the select
   around. Both states are spelled out, so the column reads down as one
   answer rather than as a mark that is present or absent. */
.col-tls {
  width: 64px;
  white-space: nowrap;
}

.tls-off {
  color: var(--warning-fg);
  font-weight: 500;
}

.tls-on {
  color: var(--text-muted);
}
</style>
