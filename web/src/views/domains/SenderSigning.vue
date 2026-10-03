<script setup lang="ts">
// The key one sender address signs its mail with, S/MIME or PGP.
//
// Two faces: with no key, the ways to get one, and with a key, what it
// is and the two switches. DKIM proves the domain to the receiving
// server, this proves the ADDRESS to the person reading, in the client
// they read in - which is why both kinds are offered and the operator
// picks by where their recipients read.
import { computed, ref } from 'vue'
import { type Sender, sendersApi, type SigningPayload } from '../../api/senders'
import { apiErrorMessage } from '../../api/client'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import { useConfirm } from '../../composables/useConfirm'
import { useFieldErrors } from '../../composables/fieldErrors'
import { formatDate } from '../../composables/formatDate'
import { expiryClass, expiryLabel, expiryTitle } from '../../composables/certExpiry'
import BaseModal from '../../components/BaseModal.vue'
import FormField from '../../components/FormField.vue'

const props = defineProps<{ sender: Sender }>()

const emit = defineEmits<{
  (e: 'changed', sender: Sender): void
  (e: 'close'): void
}>()

const notify = useNotificationStore()
const projStore = useProjectStore()
const { confirm } = useConfirm()
const { capture, clear } = useFieldErrors()

// The sender as the last answer described it, so the face follows a
// write without waiting for the list to reload.
const current = ref<Sender>(props.sender)
const signing = computed(() => current.value.signing ?? null)
const kindLabel = computed(() => (signing.value?.kind === 'pgp' ? 'PGP' : 'S/MIME'))

const canWrite = computed(() => projStore.can('senders:write'))
const canDelete = computed(() => projStore.can('senders:delete'))

// The import form.
const kind = ref<'smime' | 'pgp'>('smime')
const smimeSource = ref<'p12' | 'pem'>('p12')
const pgpImport = ref(false)
const certificate = ref('')
const privateKey = ref('')
const passphrase = ref('')
const pkcs12 = ref('')
const pkcs12Name = ref('')
const saving = ref(false)

/** The .p12 file as the base64 the request carries. */
function onPkcs12File(e: Event) {
  const file = (e.target as HTMLInputElement).files?.[0]
  pkcs12.value = ''
  pkcs12Name.value = ''
  if (!file) return

  const reader = new FileReader()
  reader.onload = () => {
    // A data URL, which is the base64 after its comma.
    const url = String(reader.result)
    pkcs12.value = url.slice(url.indexOf(',') + 1)
    pkcs12Name.value = file.name
  }
  reader.readAsDataURL(file)
}

function payload(): SigningPayload {
  if (kind.value === 'pgp') {
    if (!pgpImport.value) return { kind: 'pgp', mode: 'generate' }

    return {
      kind: 'pgp',
      mode: 'import',
      private_key: privateKey.value,
      passphrase: passphrase.value,
    }
  }

  if (smimeSource.value === 'p12') {
    return { kind: 'smime', pkcs12: pkcs12.value, passphrase: passphrase.value }
  }

  return { kind: 'smime', certificate: certificate.value, private_key: privateKey.value }
}

const ready = computed(() => {
  if (kind.value === 'pgp') return !pgpImport.value || privateKey.value.trim() !== ''
  if (smimeSource.value === 'p12') return pkcs12.value !== ''

  return certificate.value.trim() !== '' && privateKey.value.trim() !== ''
})

async function save() {
  clear()
  saving.value = true
  try {
    const res = await sendersApi.setSigning(current.value.id, payload())
    current.value = res.data.sender
    emit('changed', res.data.sender)
    notify.success(kind.value === 'pgp' && !pgpImport.value ? 'Key generated' : 'Key stored')
  } catch (e) {
    if (!capture(e)) notify.error(apiErrorMessage(e, 'The key was not accepted'))
  } finally {
    saving.value = false
  }
}

async function setFlag(field: 'sign' | 'attach_key', value: boolean) {
  try {
    const res = await sendersApi.setSigningFlags(current.value.id, { [field]: value })
    current.value = res.data.sender
    emit('changed', res.data.sender)
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to update the key'))
  }
}

/**
 * The public half becomes a file here. The route answers JSON because
 * every /api/v1 route generates an SDK method, and one that cannot
 * decode its own answer is worse than four lines of download.
 */
async function download() {
  try {
    const res = await sendersApi.publicKey(current.value.id)
    const pgp = res.data.kind === 'pgp'
    const id = signing.value?.fingerprint.slice(-16) ?? ''
    const name = pgp ? `OpenPGP_0x${id}.asc` : `${current.value.email}.pem`
    const type = pgp ? 'application/pgp-keys' : 'application/x-pem-file'
    const url = URL.createObjectURL(new Blob([res.data.public_key], { type }))
    const a = document.createElement('a')
    a.href = url
    a.download = name
    a.click()
    URL.revokeObjectURL(url)
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to read the public key'))
  }
}

async function remove(replace: boolean) {
  const ok = await confirm({
    title: replace ? 'Replace the signing key' : 'Remove the signing key',
    message: replace
      ? `Mail from ${current.value.email} goes out unsigned until the new key is stored.`
      : `Mail from ${current.value.email} goes out unsigned from here on.`,
    confirmText: replace ? 'Replace' : 'Remove',
    variant: 'danger',
  })
  if (!ok) return

  try {
    await sendersApi.removeSigning(current.value.id)
    current.value = { ...current.value, signing: null }
    emit('changed', current.value)
    if (!replace) notify.success('Signing key removed')
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to remove the key'))
  }
}
</script>

<template>
  <BaseModal size="modal-w800" :form="!signing" @submit="save" @close="emit('close')">
    <template #header>
      <h3>
        Signing for <code>{{ current.email }}</code>
      </h3>
    </template>

    <!-- With a key: what it is and the switches. -->
    <template v-if="signing">
      <dl class="detail-list">
        <dt>Kind</dt>
        <dd>
          <span class="badge badge-success">{{ kindLabel }}</span>
        </dd>

        <dt>Fingerprint</dt>
        <dd class="mono">{{ signing.fingerprint }}</dd>

        <dt>Algorithm</dt>
        <dd>{{ signing.algorithm }}</dd>

        <dt>{{ signing.kind === 'pgp' ? 'User ID' : 'Subject' }}</dt>
        <dd>{{ signing.subject }}</dd>

        <template v-if="signing.issuer">
          <dt>Issuer</dt>
          <dd>{{ signing.issuer }}</dd>
        </template>

        <dt>Stored</dt>
        <dd>{{ formatDate(signing.created_at) }}</dd>

        <dt>Expires</dt>
        <dd>
          <template v-if="signing.not_after">
            {{ formatDate(signing.not_after) }}
            <span :class="expiryClass(signing)" :title="expiryTitle(signing)">
              {{ expiryLabel(signing) }}
            </span>
          </template>
          <span v-else class="text-muted">never</span>
        </dd>
      </dl>

      <FormField hint="Off keeps the key stored and sends unsigned.">
        <label class="checkbox-label">
          <input
            type="checkbox"
            :checked="signing.sign"
            :disabled="!canWrite"
            @change="setFlag('sign', ($event.target as HTMLInputElement).checked)"
          />
          <span>Sign outgoing mail</span>
        </label>
      </FormField>

      <FormField
        v-if="signing.kind === 'pgp'"
        hint="An Autocrypt header and a key file on every message, so a client that has never seen this address learns the key from the first mail. An S/MIME signature carries its certificate by itself."
      >
        <label class="checkbox-label">
          <input
            type="checkbox"
            :checked="signing.attach_key"
            :disabled="!canWrite"
            @change="setFlag('attach_key', ($event.target as HTMLInputElement).checked)"
          />
          <span>Attach the public key</span>
        </label>
      </FormField>
    </template>

    <!-- Without one: the ways to get one. -->
    <template v-else>
      <p class="signing-intro">
        Mail from this address is signed inside the message, under DKIM, and the reader's client
        shows a seal beside it. S/MIME is verified by Outlook, Apple Mail and Google Workspace
        without plugins and needs a certificate from an authority they trust. PGP is verified by
        Thunderbird and GPG users and the key can be made here.
      </p>

      <FormField label="Kind" field="kind">
        <div class="kind-switch">
          <label class="checkbox-label">
            <input v-model="kind" type="radio" value="smime" />
            <span>S/MIME certificate</span>
          </label>
          <label class="checkbox-label">
            <input v-model="kind" type="radio" value="pgp" />
            <span>PGP key</span>
          </label>
        </div>
      </FormField>

      <template v-if="kind === 'smime'">
        <FormField label="Provided as">
          <div class="kind-switch">
            <label class="checkbox-label">
              <input v-model="smimeSource" type="radio" value="p12" />
              <span>A .p12 or .pfx file</span>
            </label>
            <label class="checkbox-label">
              <input v-model="smimeSource" type="radio" value="pem" />
              <span>PEM certificate and key</span>
            </label>
          </div>
        </FormField>

        <template v-if="smimeSource === 'p12'">
          <FormField
            label="File"
            for="signing-p12"
            field="pkcs12"
            hint="What a certificate authority hands over for a mail address. The certificate must name this address and be issued for email protection."
          >
            <input id="signing-p12" type="file" accept=".p12,.pfx" @change="onPkcs12File" />
          </FormField>
          <FormField label="Password" for="signing-p12-pass" field="passphrase">
            <input
              id="signing-p12-pass"
              v-model="passphrase"
              type="password"
              class="form-input"
              autocomplete="off"
            />
          </FormField>
        </template>

        <template v-else>
          <FormField
            label="Certificate"
            for="signing-cert"
            field="certificate"
            hint="The full chain, leaf first, if you have one."
          >
            <textarea
              id="signing-cert"
              v-model="certificate"
              class="form-textarea code-font"
              rows="7"
              placeholder="-----BEGIN CERTIFICATE-----"
            ></textarea>
          </FormField>
          <FormField
            label="Private key"
            for="signing-key"
            field="private_key"
            hint="Unencrypted PEM. A password-protected key goes in as a .p12 file instead. Checked against the certificate before anything is stored, and encrypted at rest."
          >
            <textarea
              id="signing-key"
              v-model="privateKey"
              class="form-textarea code-font"
              rows="5"
              placeholder="-----BEGIN PRIVATE KEY-----"
            ></textarea>
          </FormField>
        </template>
      </template>

      <template v-else>
        <FormField
          hint="A fresh Ed25519 key with this address as its user id. Or import one you already use for the address."
        >
          <label class="checkbox-label">
            <input v-model="pgpImport" type="checkbox" />
            <span>Import an existing key instead of generating one</span>
          </label>
        </FormField>

        <template v-if="pgpImport">
          <FormField
            label="Private key"
            for="signing-pgp-key"
            field="private_key"
            hint="The armored private key block. One of its user ids must be this address."
          >
            <textarea
              id="signing-pgp-key"
              v-model="privateKey"
              class="form-textarea code-font"
              rows="8"
              placeholder="-----BEGIN PGP PRIVATE KEY BLOCK-----"
            ></textarea>
          </FormField>
          <FormField
            label="Passphrase"
            for="signing-pgp-pass"
            field="passphrase"
            hint="Removed on import. The key is stored encrypted at rest instead."
          >
            <input
              id="signing-pgp-pass"
              v-model="passphrase"
              type="password"
              class="form-input"
              autocomplete="off"
            />
          </FormField>
        </template>
      </template>
    </template>

    <template #footer>
      <button type="button" class="btn btn-secondary" @click="emit('close')">Close</button>
      <template v-if="signing">
        <button type="button" class="btn" @click="download">Download public key</button>
        <button v-if="canWrite && canDelete" type="button" class="btn" @click="remove(true)">
          Replace
        </button>
        <button v-if="canDelete" type="button" class="btn btn-danger" @click="remove(false)">
          Remove
        </button>
      </template>
      <button
        v-else-if="canWrite"
        type="submit"
        class="btn btn-primary"
        :disabled="saving || !ready"
      >
        {{ saving ? 'Saving...' : kind === 'pgp' && !pgpImport ? 'Generate key' : 'Store key' }}
      </button>
    </template>
  </BaseModal>
</template>

<style scoped>
.signing-intro {
  font-size: 14px;
  color: var(--text-secondary);
  margin-bottom: 14px;
}

.kind-switch {
  display: flex;
  flex-wrap: wrap;
  gap: 16px;
}

/* A dense read-once list, sized down from body text like the
   certificate detail it mirrors. */
.detail-list {
  display: grid;
  grid-template-columns: max-content 1fr;
  column-gap: 16px;
  row-gap: 6px;
  margin: 0 0 16px;
  font-size: 0.8rem;
  line-height: 1.45;
}

.detail-list dt {
  color: var(--text-muted);
}

.detail-list dd {
  margin: 0;
  overflow-wrap: anywhere;
}

.mono {
  font-family: var(--font-mono);
  font-size: 0.74rem;
}

@media (max-width: 640px) {
  .detail-list {
    grid-template-columns: 1fr;
    row-gap: 2px;
  }
}
</style>
