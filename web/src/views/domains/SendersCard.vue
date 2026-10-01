<script setup lang="ts">
// The addresses this project may send from.
//
// It sits on the Domains page because you register an address for a
// domain you have just verified - but it is a SEPARATE resource with its
// own routes and its own permissions, and every control here is gated on
// senders rather than on domains. A member who may read domains is not
// thereby somebody who may add a From address.
import { onMounted, ref } from 'vue'
import { sendersApi, type Sender } from '../../api/senders'
import { apiErrorMessage } from '../../api/client'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import { useConfirm } from '../../composables/useConfirm'
import { useFieldErrors } from '../../composables/fieldErrors'
import { formatDate } from '../../composables/formatDate'
import { daysLeft, expiryClass } from '../../composables/certExpiry'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import SenderSigning from './SenderSigning.vue'

const notify = useNotificationStore()
const projStore = useProjectStore()
const { confirm } = useConfirm()
const { errors, capture, clear } = useFieldErrors()

const senders = ref<Sender[]>([])
const loading = ref(true)
const adding = ref(false)
const deletingId = ref<string | null>(null)

const email = ref('')
const name = ref('')

// The sender whose signing dialog is open.
const signingFor = ref<Sender | null>(null)

/** The dialog wrote: the row follows without a reload. */
function signingChanged(s: Sender) {
  senders.value = senders.value.map((x) => (x.id === s.id ? s : x))
  signingFor.value = s
}

/**
 * The badge for a key: its kind, red or amber as it runs out, and
 * grey once signing is switched off.
 */
function signingClass(s: Sender): string {
  const k = s.signing
  if (!k) return ''
  if (!k.sign) return 'badge badge-neutral'
  if (k.not_after && (daysLeft(k) ?? 0) <= 30) return expiryClass(k)

  return 'badge badge-success'
}

function signingLabel(s: Sender): string {
  const k = s.signing
  if (!k) return ''
  const kind = k.kind === 'pgp' ? 'PGP' : 'S/MIME'
  if (!k.sign) return `${kind} off`
  const days = k.not_after ? daysLeft(k) : null
  if (days !== null && days < 0) return `${kind} expired`
  if (days !== null && days <= 30) return `${kind} ${days}d`

  return kind
}

async function load() {
  loading.value = true
  try {
    senders.value = (await sendersApi.list()).data.senders ?? []
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load sender addresses'))
  } finally {
    loading.value = false
  }
}

async function add() {
  clear()
  const address = email.value.trim().toLowerCase()
  if (!address) return

  adding.value = true
  try {
    const payload: { email: string; name?: string } = { email: address }
    const display = name.value.trim()
    if (display) payload.name = display

    const res = await sendersApi.create(payload)
    senders.value.push(res.data.sender)
    senders.value.sort((a, b) => a.email.localeCompare(b.email))
    email.value = ''
    name.value = ''
    notify.success('Sender address added')
  } catch (e) {
    // A 400 here means the address domain is not verified by this
    // project, a 409 that the address is already registered - both of
    // which the server attributes to the field.
    if (!capture(e)) notify.error(apiErrorMessage(e, 'Failed to add sender address'))
  } finally {
    adding.value = false
  }
}

async function remove(s: Sender) {
  const ok = await confirm({
    title: 'Delete sender address',
    message: `Delete ${s.email}? It will disappear from all From selectors.`,
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!ok) return

  deletingId.value = s.id
  try {
    await sendersApi.remove(s.id)
    senders.value = senders.value.filter((x) => x.id !== s.id)
    notify.success('Sender address deleted')
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to delete sender address'))
  } finally {
    deletingId.value = null
  }
}

onMounted(load)

// The page reloads this after verifying a domain, since an address can
// only be registered once its domain is verified.
defineExpose({ reload: load })
</script>

<template>
  <div class="card">
    <div class="card-header">
      <h2>Sender Addresses</h2>
    </div>

    <LoadingBlock v-if="loading" />

    <template v-else>
      <EmptyState
        v-if="senders.length === 0"
        title="No sender addresses yet"
        text="Register the addresses you send from once a domain above is verified."
      />

      <div v-else class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th>Email</th>
              <th>Name</th>
              <th>Signing</th>
              <th>Created</th>
              <th class="col-actions"></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="s in senders" :key="s.id">
              <td>
                <code>{{ s.email }}</code>
              </td>
              <td>{{ s.name || '-' }}</td>
              <td>
                <template v-if="s.signing">
                  <span :class="signingClass(s)">{{ signingLabel(s) }}</span>
                  <code class="fingerprint">{{ s.signing.fingerprint.slice(-16) }}</code>
                </template>
                <span v-else class="text-muted">-</span>
              </td>
              <td>{{ formatDate(s.created_at) }}</td>
              <td class="col-actions">
                <div class="row-actions">
                  <button class="btn btn-secondary btn-sm" @click="signingFor = s">Signing</button>
                  <button
                    v-if="projStore.can('senders:delete')"
                    class="btn btn-danger btn-sm"
                    :disabled="deletingId === s.id"
                    @click="remove(s)"
                  >
                    Delete
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-if="projStore.can('senders:write')" class="card-body">
        <form class="sender-add-form" @submit.prevent="add">
          <input
            v-model="email"
            type="email"
            class="form-input"
            placeholder="billing@example.com"
            autocomplete="off"
          />
          <input
            v-model="name"
            type="text"
            class="form-input"
            placeholder="Display name (optional)"
            autocomplete="off"
          />
          <button type="submit" class="btn btn-primary" :disabled="adding || !email.trim()">
            {{ adding ? 'Adding...' : 'Add' }}
          </button>
        </form>
        <p v-if="errors.email" class="form-error">{{ errors.email }}</p>
        <p v-else class="form-hint sender-hint">
          Addresses can only be added for domains verified above. They appear in every From
          selector.
        </p>
      </div>
    </template>

    <SenderSigning
      v-if="signingFor"
      :sender="signingFor"
      @changed="signingChanged"
      @close="signingFor = null"
    />
  </div>
</template>

<style scoped>
/* Two fields and a button on one line, the address taking the slack -
   it is the long one and the only required one. */
.sender-add-form {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.sender-add-form .form-input {
  flex: 1;
  min-width: 180px;
}

.sender-hint {
  margin-top: 8px;
}

.col-actions {
  width: 180px;
  text-align: right;
  white-space: nowrap;
}

.row-actions {
  display: inline-flex;
  gap: 8px;
}

/* The key id beside its badge: small, because it is there to be
   compared against a client's display, not read. */
.fingerprint {
  margin-left: 8px;
  font-size: 0.74rem;
}
</style>
