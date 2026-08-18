<script setup lang="ts">
// Certificates ordered from a CA, cached in the database so one
// certificate serves every node.
//
// Always on screen, unlike before: the card only appeared once yaml had
// already configured ACME, so the one place you would look to turn it on
// was hidden until it was on.
import { computed, ref } from 'vue'
import { certificatesApi, type ACMEStatus } from '../../../api/certificates'
import { settingsApi } from '../../../api/settings'
import { apiErrorMessage } from '../../../api/client'
import { useNotificationStore } from '../../../stores/notification'
import { useConfirm } from '../../../composables/useConfirm'
import { expiryClass, expiryLabel, expiryTitle } from '../../../composables/certExpiry'
import EmptyState from '../../../components/EmptyState.vue'
import BaseModal from '../../../components/BaseModal.vue'
import FormField from '../../../components/FormField.vue'
import Notice from '../../../components/Notice.vue'

const props = defineProps<{ acme: ACMEStatus }>()

const emit = defineEmits<{
  /** Something changed on the server, so the page should re-read. */
  (e: 'changed'): void
}>()

const notify = useNotificationStore()
const { confirm } = useConfirm()

// One host at a time, and one flag for both order and renew: they are
// the same round trip from the operator's side.
const working = ref('')
const saving = ref(false)

const newHost = ref('')

// Trimmed once, so the button state and what gets sent cannot disagree.
const hostToAdd = computed(() => newHost.value.trim().toLowerCase())

const settings = ref<{ enabled: boolean; email: string; directory_url: string } | null>(null)

async function order(host: string) {
  working.value = host
  try {
    const res = await certificatesApi.order(host)
    notify.success(res.data.message)
    emit('changed')
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Could not obtain a certificate'))
  } finally {
    working.value = ''
  }
}

async function renew(host: string) {
  working.value = host
  try {
    const res = await certificatesApi.renew(host)
    notify.success(res.data.message)
    emit('changed')
  } catch (e) {
    // The CA's own words, not a paraphrase. "DNS problem: NXDOMAIN
    // looking up A for mail.example.com" says what to fix, and
    // apiErrorMessage passes it through.
    notify.error(apiErrorMessage(e, 'Renewal failed'))
  } finally {
    working.value = ''
  }
}

/**
 * The host list is a setting, so changing it is a settings write.
 *
 * Done here rather than on the platform settings page because a JSON
 * array in a text box is not something to ask a person to edit by hand.
 */
async function writeHosts(hosts: string[]) {
  saving.value = true
  try {
    await settingsApi.update([
      { key: 'acme_hosts', value: hosts.length ? JSON.stringify(hosts) : '' },
    ])
    emit('changed')
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to save the host list'))
  } finally {
    saving.value = false
  }
}

async function addHost() {
  const h = hostToAdd.value
  if (!h) return

  if (props.acme.hosts.some((x) => x.host === h)) {
    notify.error(`${h} is already listed`)

    return
  }

  await writeHosts([...props.acme.hosts.map((x) => x.host), h])
  newHost.value = ''
}

async function removeHost(host: string) {
  const ok = await confirm({
    title: `Stop issuing for ${host}`,
    message:
      `New certificates will not be ordered for ${host}, and it stops being renewed. ` +
      `Whatever is already cached keeps being served until it expires.`,
    confirmText: 'Remove',
    variant: 'danger',
  })
  if (!ok) return

  await writeHosts(props.acme.hosts.map((x) => x.host).filter((h) => h !== host))
}

function openSettings() {
  settings.value = {
    enabled: props.acme.enabled,
    email: props.acme.email,
    directory_url: props.acme.directory_url,
  }
}

async function saveSettings() {
  const s = settings.value
  if (!s) return

  saving.value = true
  try {
    await settingsApi.update([
      { key: 'acme_enabled', value: String(s.enabled) },
      { key: 'acme_email', value: s.email.trim() },
      { key: 'acme_directory_url', value: s.directory_url.trim() },
    ])
    settings.value = null
    emit('changed')
  } catch (e) {
    // Plainly, not on a field. Every write here is a settings write -
    // the body is a list of {key, value} pairs - so a refusal names
    // `key` or `value` and never `email` or `directory_url`. Two fields
    // used to bind those names and could never have shown anything.
    notify.error(apiErrorMessage(e, 'Failed to save'))
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="card">
    <div class="card-header">
      <div>
        <h2>Let's Encrypt</h2>
        <p class="text-sm text-muted">
          Certificates ordered from a CA and cached in the database, so one certificate serves every
          node. A listener uses these only when nothing is assigned to it above.
        </p>
      </div>
      <button class="btn btn-secondary btn-sm" :disabled="saving" @click="openSettings">
        Settings
      </button>
    </div>

    <EmptyState v-if="!acme.enabled">
      <p>
        ACME is off. Turn it on in <strong>Settings</strong>, name the hosts to issue for, and
        order.
      </p>
    </EmptyState>

    <template v-else>
      <!-- The one thing worth knowing before pressing Order. With no
           handshake of our own the CA cannot validate over ALPN, and
           port 80 becomes the only route - which is a different setup
           job, not a slower one. -->
      <Notice
        v-if="!acme.tls_terminated_here && !acme.challenge_addr"
        kind="warning"
        class="card-notice"
      >
        <p>
          This process does not terminate TLS, so the CA cannot validate by connecting to it. Either
          turn on <code>server.tls.enabled</code>, or set <code>acme.challenge_addr</code> (usually
          <code>:80</code>) so an HTTP-01 challenge can be answered. Ordering will fail until one of
          those is true.
        </p>
      </Notice>

      <!-- On with no hosts is a silent no-op: nothing is ordered and
           nothing logs it. The empty table row says so too, but that
           reads as an empty table, not as an unfinished setup. -->
      <Notice v-if="!acme.hosts.length" kind="warning" class="card-notice">
        <p>
          ACME is on and no host is listed, so nothing is ordered and nothing is renewed - the
          listeners serve the self-signed pair exactly as they do with ACME off, with nothing in the
          log to say why. Add the hostname to issue for below.
        </p>
      </Notice>

      <Notice v-if="acme.staging" kind="warning" class="card-notice">
        <p>
          Using a non-production directory, so anything issued here is deliberately
          <strong>not trusted</strong> by browsers. Clear <code>acme_directory_url</code> when the
          setup works.
        </p>
      </Notice>

      <div v-if="acme.hosts.length" class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th>Host</th>
              <th>Expires</th>
              <th class="text-right"></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="h in acme.hosts" :key="h.host">
              <td>
                <strong>{{ h.host }}</strong>
              </td>
              <td>
                <span v-if="!h.details" class="badge badge-warning">not issued yet</span>
                <span v-else :class="expiryClass(h.details)" :title="expiryTitle(h.details)">
                  {{ expiryLabel(h.details) }}
                </span>
              </td>
              <td class="text-right">
                <div class="table-actions">
                  <button
                    v-if="!h.details"
                    class="btn btn-sm"
                    :disabled="working === h.host"
                    @click="order(h.host)"
                  >
                    {{ working === h.host ? 'Ordering...' : 'Order' }}
                  </button>
                  <button
                    v-else
                    class="btn btn-secondary btn-sm"
                    :disabled="working === h.host"
                    @click="renew(h.host)"
                  >
                    {{ working === h.host ? 'Renewing...' : 'Renew now' }}
                  </button>
                  <button
                    class="btn btn-secondary btn-sm"
                    :disabled="saving"
                    @click="removeHost(h.host)"
                  >
                    Remove
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div class="card-body">
        <div class="add-host">
          <input
            v-model="newHost"
            class="form-input"
            :placeholder="acme.suggested || 'mail.example.com'"
            aria-label="Hostname to issue for"
            @keyup.enter="addHost"
          />
          <button class="btn" :disabled="saving || !hostToAdd" @click="addHost">Add host</button>
        </div>

        <p v-if="acme.suggested" class="text-sm text-muted suggestion">
          This installation calls itself <code>{{ acme.suggested }}</code
          >. Adding it is probably what you want.
        </p>
      </div>
    </template>

    <!-- A dialog rather than the platform settings page: these values
         only make sense together, and the host list beside them is a
         JSON array nobody should hand-edit. -->
    <BaseModal v-if="settings" title="Let's Encrypt settings" @close="settings = null">
      <FormField
        hint="Off means nothing is ordered and nothing is renewed. Listed hosts stay listed."
      >
        <template #label>
          <input v-model="settings.enabled" type="checkbox" /> Order certificates from a CA
        </template>
      </FormField>

      <FormField
        label="Account email"
        for="acme-email"
        hint="Where the CA sends expiry warnings. Used when the account is first registered, so changing it later does not re-register."
      >
        <input
          id="acme-email"
          v-model="settings.email"
          class="form-input"
          type="email"
          placeholder="ops@example.com"
        />
      </FormField>

      <FormField
        label="Directory URL"
        for="acme-directory"
        hint="Empty means Let's Encrypt production. Point it at a staging directory to rehearse without spending the rate limit."
      >
        <input
          id="acme-directory"
          v-model="settings.directory_url"
          class="form-input"
          placeholder="https://acme-v02.api.letsencrypt.org/directory"
        />
      </FormField>

      <template #footer>
        <button class="btn btn-secondary" @click="settings = null">Cancel</button>
        <button class="btn btn-primary" :disabled="saving" @click="saveSettings">
          {{ saving ? 'Saving...' : 'Save' }}
        </button>
      </template>
    </BaseModal>
  </div>
</template>

<style scoped>
/* A hostname field and its button on one line, wrapping on a narrow
   screen rather than squeezing the field to nothing. */
.add-host {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.add-host .form-input {
  flex: 1;
  min-width: 220px;
}

.suggestion {
  margin: 8px 0 0;
}

/* Its own rule rather than reaching for .alert, which turns out to be a
   per-view scoped style in the auth pages and does not exist globally. */
/* Placement only - the notice itself is the stylesheet's. These sit
   between the card header and the table rather than inside a card-body,
   so they carry the gutters a card-body would have given them. */
.card-notice {
  margin: 0 16px 12px;
}
</style>
