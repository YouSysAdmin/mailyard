<script setup lang="ts">
// One recorded event, in full.
//
// The table shows what a person scans by; this shows what they came to
// check. The long fields are here rather than in a cell because a user
// agent and a request path are read once, and putting them in the table
// pushed the columns that ARE scanned off the side.
//
// It reads and does nothing: an audit record is history, and there is no
// act this dialog could offer that would not be rewriting it.
import type { AuditEvent } from '../../api/audit'
import { formatDate } from '../../composables/formatDate'
import BaseModal from '../../components/BaseModal.vue'

defineProps<{ event: AuditEvent }>()

const emit = defineEmits<{ (e: 'close'): void }>()

// A refusal is an event in its own right on the security trail - the
// point of recording a failed sign-in is that it failed - so it wears
// the danger colour rather than reading like anything else.
function isFailure(e: AuditEvent): boolean {
  return e.type.endsWith('.failed') || e.type.endsWith('.denied')
}
</script>

<template>
  <BaseModal :title="event.type" @close="emit('close')">
    <dl class="detail-grid">
      <dt>When</dt>
      <dd>{{ formatDate(event.created_at) }}</dd>

      <dt>Event</dt>
      <dd>
        <span class="badge" :class="isFailure(event) ? 'badge-danger' : 'badge-info'">
          {{ event.type }}
        </span>
      </dd>

      <dt>Trail</dt>
      <dd>{{ event.category === 'project' ? 'Project activity' : 'Account security' }}</dd>

      <dt>Actor</dt>
      <dd>{{ event.actor_email || 'Not recorded' }}</dd>

      <dt>Seen from</dt>
      <dd>
        <code v-if="event.client_ip">{{ event.client_ip }}</code
        ><span v-else>-</span>
        <div class="text-sm text-muted">
          The address the request reached the server from. A shared egress - iCloud Private Relay, a
          VPN, an office NAT - puts many people behind one, and carries none of their own.
        </div>
      </dd>

      <dt>User agent</dt>
      <dd class="wrap">{{ event.user_agent || '-' }}</dd>

      <dt>Request</dt>
      <dd>
        <code v-if="event.method" class="wrap">{{ event.method }} {{ event.path }}</code>
        <span v-else>-</span>
      </dd>

      <dt>Status</dt>
      <dd>{{ event.status || '-' }}</dd>

      <dt>Detail</dt>
      <dd class="wrap">{{ event.detail || '-' }}</dd>

      <dt>Event ID</dt>
      <dd>
        <code class="wrap">{{ event.id }}</code>
      </dd>

      <dt v-if="event.project_id">Project ID</dt>
      <dd v-if="event.project_id">
        <code class="wrap">{{ event.project_id }}</code>
      </dd>

      <dt v-if="event.actor_id">Actor ID</dt>
      <dd v-if="event.actor_id">
        <code class="wrap">{{ event.actor_id }}</code>
      </dd>
    </dl>
    <template #footer>
      <button class="btn btn-secondary" @click="emit('close')">Close</button>
    </template>
  </BaseModal>
</template>

<style scoped>
/* Label and value, the labels sharing a column so the values line up -
   which is the whole reason this is a dl rather than a stack of rows. */
.detail-grid {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: 8px 16px;
  margin: 0;
  font-size: 13px;
}

.detail-grid dt {
  color: var(--text-muted);
}

.detail-grid dd {
  margin: 0;
}

/* A user agent is one long unbroken string and must not push the dialog
   sideways. */
.wrap {
  overflow-wrap: anywhere;
}
</style>
