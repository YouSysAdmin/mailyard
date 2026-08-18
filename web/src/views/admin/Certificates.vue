<script setup lang="ts">
// The managed certificate store, and the cards that read from it.
//
// Everything here lives in the certificates table rather than on one
// node's disk, so what this page changes is what every node serves - and
// it takes effect without a restart, because a listener resolves its
// certificate per handshake through a short cache.
//
// The page owns the DATA and the reload; each card owns its own writes
// and says `changed` when it made one. Four of them are separate
// questions - what the listeners present, what a CA has signed, what the
// relay fleet trusts, what the installation keeps for itself - and they
// were one 1500 line file.
import { ref, computed } from 'vue'
import {
  certificatesApi,
  type ManagedCertificate,
  type SystemCertificate,
  type ACMEStatus,
  type ListenerState,
} from '../../api/certificates'
import { apiErrorMessage } from '../../api/client'
import { useNotificationStore } from '../../stores/notification'
import { expiryClass, expiryLabel, expiryTitle, expiringSoon } from '../../composables/certExpiry'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import PageHeader from '../../components/PageHeader.vue'
import ListenerBindings from './certificates/ListenerBindings.vue'
import AcmeCard from './certificates/AcmeCard.vue'
import RelayAuthority from './certificates/RelayAuthority.vue'
import SystemCertificates from './certificates/SystemCertificates.vue'
import CertificateDetail from './certificates/CertificateDetail.vue'
import CertificateUpload from './certificates/CertificateUpload.vue'
import CertificateGenerate from './certificates/CertificateGenerate.vue'
import AuthorityGenerate from './certificates/AuthorityGenerate.vue'

const notify = useNotificationStore()

const loading = ref(true)

const certificates = ref<ManagedCertificate[]>([])
const system = ref<SystemCertificate[]>([])
const listeners = ref<Record<string, ListenerState>>({})

// The whole ACME configuration, not just the host list, so the card can
// offer to turn it on rather than only showing what yaml already said.
const acme = ref<ACMEStatus>({
  enabled: false,
  email: '',
  directory_url: '',
  staging: false,
  hosts: [],
  tls_terminated_here: false,
})

// The authorities on hand, which is what the issuer picker offers.
const authorities = computed(() => certificates.value.filter((c) => c.details?.is_ca))

// Everything that is not an authority. A CA carries no host names and no
// ServerAuth, so a listener serving one refuses every client - the server
// refuses the assignment, and offering it would be offering a mistake.
const assignable = computed(() => certificates.value.filter((c) => !c.details?.is_ca))

const expiring = computed(() => expiringSoon([...certificates.value, ...system.value]))

async function load() {
  loading.value = true
  try {
    const [managed, sys, acmeData] = await Promise.all([
      certificatesApi.list(),
      certificatesApi.system(),
      certificatesApi.acme(),
    ])
    certificates.value = managed.data.certificates ?? []
    listeners.value = Object.fromEntries((managed.data.listeners ?? []).map((l) => [l.listener, l]))
    system.value = sys.data.certificates ?? []
    acme.value = { ...acmeData.data, hosts: acmeData.data.hosts ?? [] }
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load the certificates'))
  } finally {
    loading.value = false
  }
}

/**
 * Which stored certificates an authority signed.
 *
 * Matched on the key IDENTIFIERS, not on the issuer name: that is a
 * distinguished name and two authorities can carry the same one. Go
 * fills both in, so this needs no column of its own.
 */
function issuedBy(ca: ManagedCertificate): ManagedCertificate[] {
  const id = ca.details?.subject_key_id
  if (!id) return []

  return certificates.value.filter((c) => c.name !== ca.name && c.details?.authority_key_id === id)
}

// Which dialog is open, and which certificate the detail is showing.
const detail = ref<ManagedCertificate | null>(null)
const showUpload = ref(false)
const showGenerate = ref(false)
const showGenerateCA = ref(false)

/** A dialog reported a write: close it and read the store again. */
async function done(close: () => void) {
  close()
  await load()
}

void load()
</script>

<template>
  <div>
    <PageHeader title="Certificates">
      <button class="btn btn-secondary" @click="showGenerateCA = true">Generate CA</button>
      <button class="btn btn-secondary" @click="showGenerate = true">Generate</button>
      <button class="btn btn-primary" @click="showUpload = true">Upload</button>
    </PageHeader>

    <LoadingBlock v-if="loading" />

    <template v-else>
      <div v-if="expiring.length" class="card">
        <div class="card-header">
          <div>
            <h2>Expiring</h2>
            <p class="text-sm text-muted">
              A listener holding an expired certificate still starts. Only the handshake fails, so
              nothing else reports it.
            </p>
          </div>
        </div>
        <div class="table-wrapper">
          <table>
            <tbody>
              <tr v-for="c in expiring" :key="('scope' in c ? c.scope : 'managed') + c.name">
                <td>
                  <strong>{{ c.name || '(unnamed)' }}</strong>
                </td>
                <td>
                  <span class="badge badge-neutral">{{ 'scope' in c ? c.scope : 'managed' }}</span>
                </td>
                <td>
                  <span :class="expiryClass(c.details)" :title="expiryTitle(c.details)">
                    {{ expiryLabel(c.details) }}
                  </span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <ListenerBindings :listeners="listeners" :assignable="assignable" @changed="load" />

      <!-- Authorities in a table of their own. They share a storage scope
           with server certificates and behave nothing alike: no host
           names, cannot be assigned to a listener, and the thing an
           operator does with one is install it elsewhere. A badge in a
           shared table said that and read as a footnote. -->
      <div v-if="authorities.length" class="card">
        <div class="card-header">
          <div>
            <h2>Certificate authorities</h2>
            <p class="text-sm text-muted">
              Yours to sign listener certificates with. Install one wherever it has to be trusted
              and every certificate it signs is trusted with it.
            </p>
          </div>
        </div>
        <div class="table-wrapper">
          <table>
            <thead>
              <tr>
                <th>Name</th>
                <th>Subject</th>
                <th>Issued</th>
                <th>Expires</th>
                <th>Key</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="c in authorities" :key="c.name" class="row-clickable" @click="detail = c">
                <td>
                  <strong>{{ c.name }}</strong>
                </td>
                <td class="text-sm truncate" :title="c.details?.subject">
                  {{ c.details?.subject ?? '-' }}
                </td>
                <td class="text-sm">
                  <span v-if="!issuedBy(c).length" class="text-muted">-</span>
                  <span
                    v-else
                    :title="
                      issuedBy(c)
                        .map((i) => i.name)
                        .join(', ')
                    "
                  >
                    {{ issuedBy(c).length }}
                  </span>
                </td>
                <td>
                  <span :class="expiryClass(c.details)" :title="expiryTitle(c.details)">
                    {{ expiryLabel(c.details) }}
                  </span>
                </td>
                <td class="text-sm">{{ c.details?.key_algorithm ?? '-' }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <div class="card">
        <div class="card-header">
          <div>
            <h2>Managed</h2>
            <p class="text-sm text-muted">
              Certificates you uploaded or generated. Private keys are encrypted at rest and never
              returned.
            </p>
          </div>
        </div>

        <EmptyState
          v-if="assignable.length === 0"
          title="No certificates yet"
          text="Upload a pair you already have, or generate a self-signed one."
        />
        <div v-else class="table-wrapper">
          <table>
            <thead>
              <tr>
                <th>Name</th>
                <th>Subject</th>
                <th>Names</th>
                <th>Expires</th>
                <th>Key</th>
                <th>In use</th>
              </tr>
            </thead>
            <tbody>
              <!-- The whole row opens the detail. The fingerprint, the
                   serial and the exact dates used to sit in the cells and
                   pushed everything else off the side - they are what you
                   look at once, not what you scan. -->
              <tr v-for="c in assignable" :key="c.name" class="row-clickable" @click="detail = c">
                <td>
                  <strong>{{ c.name }}</strong>
                </td>
                <td class="text-sm">
                  <span v-if="!c.details" class="text-muted">-</span>
                  <span v-else class="truncate" :title="c.details.subject">
                    {{ c.details.subject }}
                  </span>
                  <div
                    v-if="c.details && !c.details.self_signed"
                    class="text-sm text-muted truncate"
                    :title="'Issued by ' + c.details.issuer"
                  >
                    by {{ c.details.issuer }}
                  </div>
                </td>
                <td>
                  <span v-if="!c.details" class="badge badge-danger">will not parse</span>
                  <span v-else-if="!c.details.dns_names?.length" class="text-muted">-</span>
                  <span v-else :title="c.details.dns_names.join(', ')">
                    {{ c.details.dns_names.length }}
                  </span>
                </td>
                <td>
                  <span :class="expiryClass(c.details)" :title="expiryTitle(c.details)">
                    {{ expiryLabel(c.details) }}
                  </span>
                </td>
                <td class="text-sm">{{ c.details?.key_algorithm ?? '-' }}</td>
                <td>
                  <span v-if="c.used_by?.length" class="badge badge-info">
                    {{ c.used_by.join(', ') }}
                  </span>
                  <!-- Assigned to a listener that does not terminate TLS.
                       Shown as a warning rather than as "in use", which is
                       what this cell used to claim while openssl showed
                       the listener speaking plaintext. -->
                  <span
                    v-else-if="c.dormant?.length"
                    class="badge badge-warning"
                    :title="
                      c.dormant.join(', ') +
                      ' is assigned this certificate but does not terminate TLS'
                    "
                  >
                    {{ c.dormant.join(', ') }}, TLS off
                  </span>
                  <span v-else class="text-muted">-</span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <AcmeCard :acme="acme" @changed="load" />

      <RelayAuthority :system="system" @changed="load" />

      <SystemCertificates :system="system" />
    </template>

    <CertificateDetail
      v-if="detail"
      :certificate="detail"
      :issued="issuedBy(detail)"
      @changed="done(() => (detail = null))"
      @close="detail = null"
    />

    <CertificateUpload
      v-if="showUpload"
      @created="done(() => (showUpload = false))"
      @close="showUpload = false"
    />

    <CertificateGenerate
      v-if="showGenerate"
      :authorities="authorities"
      @created="done(() => (showGenerate = false))"
      @close="showGenerate = false"
    />

    <AuthorityGenerate
      v-if="showGenerateCA"
      @created="done(() => (showGenerateCA = false))"
      @close="showGenerateCA = false"
    />
  </div>
</template>
