<script setup lang="ts">
// The relay-node listing, wherever it is being read.
//
// TWO pages show it and they were 60% the same file, down to the same
// name in two directories: a project's own nodes under Infrastructure,
// and every node on the installation under Admin. What they actually
// differ in is small - who may approve what, and one extra line marking
// a tenant's node - and it was buried under eighty lines of identical
// markup that had already started to drift. Last seen said "5 min ago"
// here and "5m ago" in the notification bell for the same interval.
//
// It renders rows and raises intent. The WRITES stay with each page,
// because they go to different endpoints: a project admin approves
// through their own project's routes and a platform admin through the
// admin ones, and which is which is not this component's business.
import type { RelayNode } from '../api/types'
import { formatDate, timeAgo } from '../composables/formatDate'

const props = defineProps<{
  nodes: RelayNode[]
  /** The node whose request is in flight, so its buttons go quiet. */
  busy: string
  /**
   * Whether to offer the actions at all. The platform page always does;
   * a project page offers them to a member holding smtp:write.
   */
  canEdit: boolean
  /** Removing is a separate grant from approving and suspending. */
  canDelete: boolean
  /**
   * True on the listing that sees every project's nodes.
   *
   * It marks the rows a tenant owns and withholds Approve on them,
   * because approving one is that project's decision and the API
   * refuses it here. Suspend and Remove deliberately DO cross that
   * line: an operator has to be able to stop a machine that is sending
   * badly from their installation.
   */
  showsOtherProjects?: boolean
}>()

const emit = defineEmits<{
  (e: 'approve', node: RelayNode): void
  (e: 'suspend', node: RelayNode): void
  (e: 'remove', node: RelayNode): void
}>()

/** The exact stamp behind a relative one, for the title attribute. */
function exact(at?: string): string {
  return formatDate(at, 'nothing has come through this node yet')
}

function mayApprove(node: RelayNode): boolean {
  if (node.status === 'enabled') return false

  return !(props.showsOtherProjects && node.project_id)
}
</script>

<template>
  <div class="table-wrapper">
    <table>
      <thead>
        <tr>
          <th>Name</th>
          <th>Address</th>
          <th>Version</th>
          <th>Last seen</th>
          <th>State</th>
          <th v-if="canEdit" class="text-right">Actions</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="node in nodes" :key="node.id">
          <td>
            <strong>{{ node.name }}</strong>
            <div class="text-sm text-muted">{{ node.host }}:{{ node.port }}</div>
            <div v-if="showsOtherProjects && node.project_id" class="text-sm text-muted">
              belongs to a project
            </div>
            <div
              v-if="node.inbound_enabled"
              class="text-sm text-muted"
              :title="exact(node.last_inbound_at)"
            >
              MX - last mail {{ timeAgo(node.last_inbound_at) }}
            </div>
          </td>
          <td>{{ node.public_ip || '-' }}</td>
          <td>{{ node.version || '-' }}</td>
          <td :title="exact(node.last_seen_at)">{{ timeAgo(node.last_seen_at) }}</td>
          <td>
            <span
              class="badge"
              :class="{
                'badge-success': node.status === 'enabled',
                'badge-warning': node.status === 'pending',
                'badge-danger': node.status === 'disabled' || node.status === 'invalid',
              }"
              >{{ node.status }}</span
            >
            <!-- Approved and dead is a real state, and the one worth
                 showing: delivery has already stopped using this node,
                 so a row saying only "enabled" would be lying. -->
            <span v-if="node.status === 'enabled' && !node.alive" class="badge badge-danger">
              not reporting
            </span>
            <!-- Mail this node took and cannot hand back. It is the only
                 visible symptom of a broken link in that direction, so
                 it is a badge and not a column to go looking for. -->
            <span
              v-if="node.inbound_queued > 0"
              class="badge badge-warning"
              title="Received mail this node is holding because it cannot reach Mailyard"
            >
              {{ node.inbound_queued }} unforwarded
            </span>
          </td>

          <td v-if="canEdit" class="text-right">
            <button
              v-if="mayApprove(node)"
              class="btn btn-sm btn-primary"
              :disabled="busy === node.id"
              @click="emit('approve', node)"
            >
              Approve
            </button>
            <button
              v-else-if="node.status === 'enabled'"
              class="btn btn-sm"
              :disabled="busy === node.id"
              @click="emit('suspend', node)"
            >
              Suspend
            </button>
            <button
              v-if="canDelete"
              class="btn btn-sm btn-danger"
              :disabled="busy === node.id"
              @click="emit('remove', node)"
            >
              Remove
            </button>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
