<script setup lang="ts">
// The installation's own authority for its relay fleet.
//
// Its own card rather than three rows in the list below. As rows an
// operator could see that it exists and nothing about what it has
// signed, with no way to end it.
import { computed } from 'vue'
import type { SystemCertificate } from '../../../api/certificates'
import { relayNodesApi } from '../../../api/relayNodes'
import { apiErrorMessage } from '../../../api/client'
import { useNotificationStore } from '../../../stores/notification'
import { useConfirm } from '../../../composables/useConfirm'
import { expiryClass, expiryLabel, expiryTitle } from '../../../composables/certExpiry'

const props = defineProps<{ system: SystemCertificate[] }>()

const emit = defineEmits<{ (e: 'changed'): void }>()

const notify = useNotificationStore()
const { confirm } = useConfirm()

const authority = computed(() => props.system.find((s) => s.scope === 'relay-ca'))
const workerCert = computed(() => props.system.find((s) => s.scope === 'relay-client'))

// Counted, not listed: an installation with fifty nodes has fifty
// issued certificates, and the question is "what has my authority
// signed", not fifty rows of uuid.
const issued = computed(() => props.system.filter((s) => s.scope === 'relay-node').length)

async function destroy() {
  const ok = await confirm({
    title: 'Destroy the relay authority',
    message:
      `Every relay node holds a certificate signed by this authority, so all of them ` +
      `stop carrying mail at once. Their enrolments go too, because a node left with a ` +
      `certificate from a destroyed authority keeps reporting as alive and quietly ` +
      `carries nothing.\n\n` +
      `Nothing comes back on its own: each node has to enrol AGAIN, which means giving ` +
      `it its enrolment token. Other API nodes keep the old authority cached until they ` +
      `restart.`,
    confirmText: 'Destroy it',
    variant: 'danger',
  })
  if (!ok) return

  try {
    const res = await relayNodesApi.resetAuthority()
    notify.success(res.data.message)
    emit('changed')
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to destroy the relay authority'))
  }
}
</script>

<template>
  <div v-if="authority" class="card">
    <div class="card-header">
      <div>
        <h2>Relay authority</h2>
        <p class="text-sm text-muted">
          Its own private authority, minted the first time a relay node enrolled. It signs each
          node's certificate and the one every delivery worker presents, and nothing outside this
          installation trusts it.
        </p>
      </div>
      <button class="btn btn-danger btn-sm" @click="destroy">Destroy</button>
    </div>

    <div class="table-wrapper">
      <table>
        <thead>
          <tr>
            <th>What</th>
            <th>Subject</th>
            <th>Expires</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td>
              <strong>Authority</strong>
            </td>
            <td class="text-sm truncate" :title="authority.details?.subject">
              {{ authority.details?.subject ?? '-' }}
            </td>
            <td>
              <span :class="expiryClass(authority.details)" :title="expiryTitle(authority.details)">
                {{ expiryLabel(authority.details) }}
              </span>
            </td>
          </tr>

          <tr v-if="workerCert">
            <td>
              <strong>Worker certificate</strong>
            </td>
            <td class="text-sm truncate" :title="workerCert.details?.subject">
              {{ workerCert.details?.subject ?? '-' }}
            </td>
            <td>
              <span
                :class="expiryClass(workerCert.details)"
                :title="expiryTitle(workerCert.details)"
              >
                {{ expiryLabel(workerCert.details) }}
              </span>
            </td>
          </tr>

          <tr>
            <td>
              <strong>Issued to nodes</strong>
            </td>
            <td class="text-sm" colspan="2">
              <span v-if="issued === 0" class="text-muted">nothing yet</span>
              <span v-else>{{ issued }}</span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
