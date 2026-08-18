<script setup lang="ts">
// The certificates the installation keeps for itself: the ACME cache
// and the self-signed pair.
//
// Read-only, because nothing here is a decision - these are maintained
// by the code that needs them, and the operator's only interest is
// whether one is about to run out.
import { computed } from 'vue'
import type { SystemCertificate } from '../../../api/certificates'
import { expiryClass, expiryLabel, expiryTitle } from '../../../composables/certExpiry'

const props = defineProps<{ system: SystemCertificate[] }>()

// The relay scopes are a different question - a fleet's identity rather
// than this installation's TLS material - and have their own card.
const held = computed(() => props.system.filter((s) => !s.scope.startsWith('relay-')))
</script>

<template>
  <div v-if="held.length" class="card">
    <div class="card-header">
      <div>
        <h2>Held by the installation</h2>
        <p class="text-sm text-muted">
          The ACME cache and the self-signed pair. Maintained by the code that needs them.
        </p>
      </div>
    </div>

    <div class="table-wrapper">
      <table>
        <thead>
          <tr>
            <th>Scope</th>
            <th>Name</th>
            <th>Expires</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="s in held" :key="s.scope + '/' + s.name">
            <td>
              <span class="badge badge-neutral">{{ s.scope }}</span>
            </td>
            <td class="truncate">{{ s.name || '-' }}</td>
            <td>
              <span :class="expiryClass(s.details)" :title="expiryTitle(s.details)">
                {{ expiryLabel(s.details) }}
              </span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
