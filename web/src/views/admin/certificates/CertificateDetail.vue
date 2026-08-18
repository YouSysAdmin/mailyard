<script setup lang="ts">
// Everything about one stored certificate that is read ONCE.
//
// The fingerprint, the serial, the exact dates and the key identifiers
// used to sit in table cells and pushed the columns that are scanned off
// the side of the page. They are here instead, and the row opens it.
//
// It owns the two acts that can only be done to a certificate you are
// looking at - download it, delete it - and says `changed` when the
// second one lands, because after that there is nothing to be looking
// at.
import { certificatesApi, type ManagedCertificate } from '../../../api/certificates'
import { apiErrorMessage } from '../../../api/client'
import { useNotificationStore } from '../../../stores/notification'
import { useConfirm } from '../../../composables/useConfirm'
import { formatDate } from '../../../composables/formatDate'
import { expiryClass, expiryLabel } from '../../../composables/certExpiry'
import BaseModal from '../../../components/BaseModal.vue'
import EmptyState from '../../../components/EmptyState.vue'

const props = defineProps<{
  certificate: ManagedCertificate
  /**
   * What this one signed, resolved by the page - only it holds the rest
   * of the store, and the match is on key identifiers rather than on the
   * issuer name, which two authorities can share.
   */
  issued: ManagedCertificate[]
}>()

const emit = defineEmits<{
  (e: 'changed'): void
  (e: 'close'): void
}>()

const notify = useNotificationStore()
const { confirm } = useConfirm()

/**
 * The PEM arrives as JSON and becomes a file here.
 *
 * Which is why the route does not stream one: every /api/v1 route
 * generates an SDK method, and a method that cannot decode its own
 * response is worse than a download the console assembles in four lines.
 */
async function download() {
  try {
    const res = await certificatesApi.pem(props.certificate.name)
    const url = URL.createObjectURL(new Blob([res.data.pem], { type: 'application/x-pem-file' }))
    const a = document.createElement('a')
    a.href = url
    a.download = `${props.certificate.name}.pem`
    a.click()
    URL.revokeObjectURL(url)
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to read the certificate'))
  }
}

async function remove() {
  const ok = await confirm({
    title: 'Delete certificate',
    message: `Delete ${props.certificate.name}? Any listener holding it falls back to the certificate its config file builds.`,
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!ok) return

  try {
    await certificatesApi.remove(props.certificate.name)
    notify.success('Certificate deleted')
    emit('changed')
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to delete the certificate'))
  }
}
</script>

<template>
  <BaseModal @close="emit('close')">
    <template #header>
      <h3>
        {{ certificate.name }}
        <span v-if="certificate.details?.is_ca" class="badge badge-info">CA</span>
      </h3>
    </template>

    <EmptyState v-if="!certificate.details" title="This certificate will not parse">
      <p>The stored bytes are not a certificate any more. Delete it and store the pair again.</p>
    </EmptyState>

    <dl v-else class="detail-list">
      <dt>Subject</dt>
      <dd>{{ certificate.details.subject }}</dd>

      <dt>Issuer</dt>
      <dd>
        {{ certificate.details.issuer }}
        <span v-if="certificate.details.self_signed" class="text-muted">(itself)</span>
      </dd>

      <dt>Host names</dt>
      <dd>
        <span v-if="!certificate.details.dns_names?.length" class="text-muted">
          none - an authority carries none, and a server certificate with none matches nothing
        </span>
        <span v-else>{{ certificate.details.dns_names.join(', ') }}</span>
      </dd>

      <dt>Valid</dt>
      <dd>
        {{ formatDate(certificate.details.not_before) }} to
        {{ formatDate(certificate.details.not_after) }}
        <span :class="expiryClass(certificate.details)">
          {{ expiryLabel(certificate.details) }}
        </span>
      </dd>

      <dt>Key</dt>
      <dd>{{ certificate.details.key_algorithm }}</dd>

      <dt>SHA-256</dt>
      <dd class="mono">{{ certificate.details.fingerprint }}</dd>

      <dt>Serial</dt>
      <dd class="mono">{{ certificate.details.serial }}</dd>

      <dt>Certificates in the file</dt>
      <dd>{{ certificate.details.chain_length }}</dd>

      <template v-if="certificate.details.is_ca">
        <dt>Signed</dt>
        <dd>
          <span v-if="!issued.length" class="text-muted">nothing yet</span>
          <span v-else>{{ issued.map((i) => i.name).join(', ') }}</span>
        </dd>
      </template>

      <dt>In use</dt>
      <dd>
        <span v-if="certificate.used_by?.length" class="badge badge-info">
          {{ certificate.used_by.join(', ') }}
        </span>
        <span v-else-if="certificate.dormant?.length" class="badge badge-warning">
          assigned to {{ certificate.dormant.join(', ') }}, which does not terminate TLS
        </span>
        <span v-else class="text-muted">no listener</span>
      </dd>

      <dt>Stored</dt>
      <dd>{{ formatDate(certificate.created_at) }}</dd>
    </dl>

    <template #footer>
      <button class="btn btn-secondary" @click="emit('close')">Close</button>
      <button class="btn" @click="download">Download</button>
      <button class="btn btn-danger" @click="remove">Delete</button>
    </template>
  </BaseModal>
</template>

<style scoped>
/* A dense read-once list, so it is sized down from body text. At the
   default size eleven rows of it filled the viewport and the values
   read as headings. */
.detail-list {
  display: grid;
  grid-template-columns: max-content 1fr;
  column-gap: 16px;
  row-gap: 6px;
  margin: 0;
  font-size: 0.8rem;
  line-height: 1.45;
}

.detail-list dt {
  color: var(--text-muted);
}

.detail-list dd {
  margin: 0;
  /* A fingerprint and a distinguished name are both long and neither
     may push the modal sideways. */
  overflow-wrap: anywhere;
}

/* Smaller again than .code-font, which is body size: a fingerprint is
   64 hex characters and has to fit the column without wrapping twice. */
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
