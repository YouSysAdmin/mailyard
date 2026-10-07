<script setup lang="ts">
// Who one verified domain is shared with, and the control to share it
// with one more project or take it back.
//
// A shared project may SEND as the domain through its own servers,
// signed with this domain's DKIM key. The records, the key and the
// inbound mail stay here. The other project is named by its slug,
// because a member of this project cannot list anybody else's.
import { onMounted, ref } from 'vue'
import { type DomainGrant, domainsApi, type InboundDomain } from '../../api/domains'
import { apiErrorMessage } from '../../api/client'
import { useNotificationStore } from '../../stores/notification'
import { useConfirm } from '../../composables/useConfirm'
import { useFieldErrors } from '../../composables/fieldErrors'
import { formatDate } from '../../composables/formatDate'
import BaseModal from '../../components/BaseModal.vue'
import FormField from '../../components/FormField.vue'
import LoadingBlock from '../../components/LoadingBlock.vue'
import StatusBadge from '../../components/StatusBadge.vue'

const props = defineProps<{ domain: InboundDomain }>()
const emit = defineEmits<{ close: [] }>()

const notify = useNotificationStore()
const { confirm } = useConfirm()
const { capture, clear } = useFieldErrors()

const grants = ref<DomainGrant[]>([])
const loading = ref(true)
const sharing = ref(false)
const revokingId = ref<string | null>(null)
const slug = ref('')

async function load() {
  loading.value = true
  try {
    grants.value = (await domainsApi.grants(props.domain.id)).data.grants ?? []
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load who this domain is shared with'))
    emit('close')
  } finally {
    loading.value = false
  }
}

async function share() {
  const value = slug.value.trim()
  if (!value) return

  clear()
  sharing.value = true
  try {
    grants.value = (await domainsApi.share(props.domain.id, value)).data.grants ?? []
    slug.value = ''
    notify.success(`${props.domain.domain} shared`)
  } catch (e) {
    if (!capture(e)) notify.error(apiErrorMessage(e, 'Failed to share the domain'))
  } finally {
    sharing.value = false
  }
}

// The name arrives once the other project accepts. Until then the
// owner sees the slug it typed, which is all it is entitled to.
function grantName(g: DomainGrant) {
  return g.project_name || g.project_slug
}

async function revoke(g: DomainGrant) {
  const pending = g.status === 'pending'
  const ok = await confirm({
    title: pending ? 'Withdraw the offer' : 'Stop sharing',
    message: pending
      ? `${grantName(g)} will no longer be offered ${props.domain.domain}.`
      : `${grantName(g)} will no longer be able to send as ${props.domain.domain}.`,
    confirmText: pending ? 'Withdraw' : 'Stop sharing',
    variant: 'danger',
  })
  if (!ok) return

  revokingId.value = g.project_id
  try {
    await domainsApi.unshare(props.domain.id, g.project_id)
    grants.value = grants.value.filter((x) => x.project_id !== g.project_id)
    notify.success(
      pending
        ? `The offer to ${grantName(g)} was withdrawn`
        : `${grantName(g)} can no longer send as ${props.domain.domain}`,
    )
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to stop sharing'))
  } finally {
    revokingId.value = null
  }
}

onMounted(load)
</script>

<template>
  <BaseModal
    :title="`Share ${domain.domain}`"
    size="modal-w640"
    form
    @submit="share"
    @close="emit('close')"
  >
    <p class="share-intro">
      A project you share this domain with can send as it and its subdomains through its own SMTP
      servers, signed with this domain's DKIM key, once it accepts the offer. DNS records, the key
      and inbound mail stay with this project.
    </p>

    <LoadingBlock v-if="loading" />

    <div v-else-if="grants.length" class="table-wrapper share-list">
      <table>
        <thead>
          <tr>
            <th>Project</th>
            <th>Status</th>
            <th>Shared</th>
            <th class="col-actions"></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="g in grants" :key="g.project_id">
            <td>
              <template v-if="g.project_name">{{ g.project_name }} </template>
              <code>{{ g.project_slug }}</code>
            </td>
            <td><StatusBadge :status="g.status" scope="share" /></td>
            <td>{{ formatDate(g.accepted_at ?? g.created_at) }}</td>
            <td class="col-actions">
              <button
                type="button"
                class="btn btn-danger btn-sm"
                :disabled="revokingId === g.project_id"
                @click="revoke(g)"
              >
                {{ g.status === 'pending' ? 'Withdraw' : 'Stop sharing' }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <FormField
      label="Project slug"
      for="share-slug"
      field="project_slug"
      hint="The slug of the project to share with, as shown in its settings."
    >
      <input
        id="share-slug"
        v-model="slug"
        type="text"
        class="form-input"
        placeholder="auth-app"
        autocomplete="off"
        spellcheck="false"
      />
    </FormField>

    <template #footer>
      <button type="button" class="btn btn-secondary" @click="emit('close')">Close</button>
      <button type="submit" class="btn btn-primary" :disabled="sharing || !slug.trim()">
        {{ sharing ? 'Sharing...' : 'Share' }}
      </button>
    </template>
  </BaseModal>
</template>

<style scoped>
.share-intro {
  font-size: 14px;
  color: var(--text-secondary);
  margin-bottom: 14px;
}

.share-list {
  margin-bottom: 16px;
}

.col-actions {
  text-align: right;
  white-space: nowrap;
}
</style>
