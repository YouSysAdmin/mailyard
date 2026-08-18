<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import { domainsApi, type InboundDomain, type DNSRecord } from '../../api/domains'
import { apiErrorMessage } from '../../api/client'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import { useConfirm } from '../../composables/useConfirm'
import DnsRecordList from '../../components/DnsRecordList.vue'
import { formatDate } from '../../composables/formatDate'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import PageHeader from '../../components/PageHeader.vue'
import BaseModal from '../../components/BaseModal.vue'
import FormField from '../../components/FormField.vue'
import SendersCard from './SendersCard.vue'
import { useFieldErrors } from '../../composables/fieldErrors'

const notify = useNotificationStore()
const projStore = useProjectStore()
const { confirm } = useConfirm()

const loading = ref(true)
const domains = ref<InboundDomain[]>([])

// Add-domain modal state. After a successful create the same modal
// switches to showing the DNS record the operator must publish.
const showAddModal = ref(false)
const newDomain = ref('')
const domainError = ref('')
const creating = ref(false)
const createdRecords = ref<DNSRecord[]>([])
const createdDomain = ref<InboundDomain | null>(null)

// DNS records modal for existing domains. Available after
// verification too - that is when the DKIM record appears.
const dnsModalDomain = ref<InboundDomain | null>(null)
const dnsModalRecords = ref<DNSRecord[]>([])
const dnsModalLoading = ref(false)

const verifyingId = ref<string | null>(null)
const deletingId = ref<string | null>(null)

// Approved sender addresses backed by the verified domains above.

// Loose FQDN check to catch obvious typos before the server-side
// fqdn validation runs.
const DOMAIN_RE = /^(?=.{1,253}$)([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,}$/i

const { errors: fieldErrors, capture, clear } = useFieldErrors()

async function load() {
  loading.value = true
  try {
    const res = await domainsApi.list()
    domains.value = res.data.domains ?? []
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load domains'))
  } finally {
    loading.value = false
  }
}

// Verifying a domain is what makes an address on it registerable, so
// the senders card is told to re-read itself.
const sendersCard = ref<{ reload: () => void } | null>(null)

onMounted(load)
watch(() => projStore.currentProjectId, load)

function openAddModal() {
  newDomain.value = ''
  domainError.value = ''
  createdRecords.value = []
  createdDomain.value = null
  showAddModal.value = true
}

function closeAddModal() {
  showAddModal.value = false
  createdRecords.value = []
  createdDomain.value = null
}

async function createDomain() {
  const name = newDomain.value.trim().toLowerCase()
  domainError.value = ''
  if (!name) {
    domainError.value = 'Domain is required'
    return
  }
  if (!DOMAIN_RE.test(name)) {
    domainError.value = 'Enter a valid domain name like example.com'
    return
  }
  creating.value = true
  try {
    const res = await domainsApi.create(name)
    domains.value.push(res.data.domain)
    domains.value.sort((a, b) => a.domain.localeCompare(b.domain))
    createdDomain.value = res.data.domain
    createdRecords.value = res.data.dns_records ?? []
    notify.success('Domain added - publish the DNS records to verify it')
  } catch (e) {
    domainError.value = apiErrorMessage(e, 'Failed to add domain')
  } finally {
    creating.value = false
  }
}

async function showDnsRecord(d: InboundDomain) {
  dnsModalDomain.value = d
  dnsModalRecords.value = []
  dnsModalLoading.value = true
  try {
    const res = await domainsApi.get(d.id)
    dnsModalRecords.value = res.data.dns_records ?? []
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load DNS records'))
    dnsModalDomain.value = null
  } finally {
    dnsModalLoading.value = false
  }
}

function closeDnsModal() {
  dnsModalDomain.value = null
  dnsModalRecords.value = []
}

async function verifyDomain(d: InboundDomain) {
  if (verifyingId.value) return
  verifyingId.value = d.id
  try {
    const res = await domainsApi.verify(d.id)
    const updated = res.data.domain
    const idx = domains.value.findIndex((x) => x.id === d.id)
    if (idx !== -1) domains.value[idx] = updated
    if (updated.verified) {
      if (!d.verified) {
        // First successful verification is the moment the DKIM key is
        // minted, so the record the operator most needs to publish has
        // only just come into existence. Point them at it.
        notify.success(
          `${updated.domain} verified - a DKIM key was generated, publish its DNS record`,
        )
      } else {
        notify.success(`${updated.domain} records re-checked`)
      }
      // Refresh the modal rather than closing it - the DKIM row now
      // carries a real value instead of the placeholder text.
      if (dnsModalDomain.value?.id === d.id) await showDnsRecord(updated)
      // A verified domain is what makes an address on it registerable,
      // so the card below now has something more to offer.
      if (!d.verified) sendersCard.value?.reload()
    } else {
      notify.info('TXT record not found yet - DNS changes can take a while to propagate')
    }
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to verify domain'))
  } finally {
    verifyingId.value = null
  }
}

async function deleteDomain(d: InboundDomain) {
  const ok = await confirm({
    title: 'Delete domain',
    message: `Delete ${d.domain}? Inbound mail for this domain will no longer be accepted.`,
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!ok) return
  deletingId.value = d.id
  try {
    await domainsApi.remove(d.id)
    domains.value = domains.value.filter((x) => x.id !== d.id)
    notify.success('Domain deleted')
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to delete domain'))
  } finally {
    deletingId.value = null
  }
}
</script>

<template>
  <div>
    <PageHeader title="Domains">
      <button v-if="projStore.can('domains:write')" class="btn btn-primary" @click="openAddModal">
        Add domain
      </button>
    </PageHeader>

    <div class="card">
      <LoadingBlock v-if="loading" />

      <template v-else>
        <EmptyState
          v-if="domains.length === 0"
          title="No domains yet"
          text="Claim a domain and publish its DNS TXT record to start receiving inbound mail."
        />
        <div v-else class="table-wrapper">
          <table>
            <thead>
              <tr>
                <th>Domain</th>
                <th>Status</th>
                <th>Verified At</th>
                <th>Added</th>
                <th class="col-actions"></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="d in domains" :key="d.id">
                <td>
                  <code>{{ d.domain }}</code>
                </td>
                <td>
                  <span :class="d.verified ? 'badge badge-success' : 'badge badge-warning'">
                    {{ d.verified ? 'Verified' : 'Pending' }}
                  </span>
                </td>
                <td>{{ formatDate(d.verified_at) }}</td>
                <td>{{ formatDate(d.created_at) }}</td>
                <td class="col-actions">
                  <div class="row-actions">
                    <button class="btn btn-secondary btn-sm" @click="showDnsRecord(d)">
                      DNS records
                    </button>
                    <button
                      v-if="projStore.can('domains:write')"
                      class="btn btn-secondary btn-sm"
                      :disabled="verifyingId === d.id"
                      @click="verifyDomain(d)"
                    >
                      {{ verifyingId === d.id ? 'Checking...' : 'Verify' }}
                    </button>
                    <button
                      v-if="projStore.can('domains:delete')"
                      class="btn btn-danger btn-sm"
                      :disabled="deletingId === d.id"
                      @click="deleteDomain(d)"
                    >
                      Delete
                    </button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </template>
    </div>

    <!-- Its own resource, its own routes, its own permissions - it
         lives here only because you register an address for a domain
         you have just verified. -->
    <SendersCard v-if="projStore.can('senders:read')" ref="sendersCard" />

    <!-- Add domain modal -->
    <!-- One dialog with two faces: the form, and the records it answers
         with. The form wrapper follows the branch, so Enter submits
         while the form is showing and nothing submits afterwards. -->
    <BaseModal
      v-if="showAddModal"
      :title="createdRecords.length ? 'Publish these DNS records' : 'Add Domain'"
      :form="!createdRecords.length"
      @submit="createDomain"
      @close="closeAddModal"
    >
      <FormField
        v-if="!createdRecords.length"
        label="Domain"
        for="new-domain"
        :error="fieldErrors.domain || domainError"
        hint="The bare recipient domain. You will prove ownership with a DNS TXT record."
      >
        <input
          id="new-domain"
          v-model="newDomain"
          type="text"
          class="form-input"
          placeholder="example.com"
          autocomplete="off"
          autofocus
        />
      </FormField>

      <template v-else>
        <p class="dns-intro">
          Publish these records at your DNS provider, then click Verify on
          <code>{{ createdDomain?.domain }}</code
          >. Only the ownership record is required to start - the others improve how receivers treat
          your mail and can be added later.
        </p>
        <DnsRecordList :records="createdRecords" />
      </template>

      <template #footer>
        <template v-if="!createdRecords.length">
          <button type="button" class="btn btn-secondary" @click="closeAddModal">Cancel</button>
          <button type="submit" class="btn btn-primary" :disabled="creating || !newDomain.trim()">
            {{ creating ? 'Adding...' : 'Add domain' }}
          </button>
        </template>
        <button v-else type="button" class="btn btn-primary" @click="closeAddModal">Done</button>
      </template>
    </BaseModal>

    <!-- DNS records modal, pending or verified domains alike -->
    <BaseModal v-if="dnsModalDomain" @close="closeDnsModal">
      <template #header>
        <h3>DNS records for {{ dnsModalDomain.domain }}</h3>
      </template>
      <LoadingBlock v-if="dnsModalLoading" />
      <template v-else-if="dnsModalRecords.length">
        <p class="dns-intro">
          Publish these records, then click Verify. DNS changes can take a while to propagate.
        </p>
        <DnsRecordList :records="dnsModalRecords" />
      </template>
      <template #footer>
        <button type="button" class="btn btn-secondary" @click="closeDnsModal">Close</button>
        <button
          v-if="projStore.can('domains:write')"
          type="button"
          class="btn btn-primary"
          :disabled="verifyingId === dnsModalDomain.id"
          @click="verifyDomain(dnsModalDomain)"
        >
          {{ verifyingId === dnsModalDomain.id ? 'Checking...' : 'Verify now' }}
        </button>
      </template>
    </BaseModal>
  </div>
</template>

<style scoped>
.col-actions {
  text-align: right;
  white-space: nowrap;
}

.row-actions {
  display: inline-flex;
  gap: 8px;
}

.dns-intro {
  font-size: 14px;
  color: var(--text-secondary);
  margin-bottom: 14px;
}
</style>
