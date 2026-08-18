<script setup lang="ts">
// What an existing key is and what it can reach.
//
// A RECORD, not a form with every control greyed out. Nothing here is
// editable - a key's reach is fixed at creation - so reading it back as
// disabled inputs was showing a shape that invited an edit and then
// refused it. The permissions stay a grid, because a matrix is how they
// are chosen and how they are best read.
import { computed } from 'vue'
import type { APIKey, PermissionResource } from '../../api/types'
import { formatDate } from '../../composables/formatDate'
import BaseModal from '../../components/BaseModal.vue'
import PermissionGrid from '../../components/PermissionGrid.vue'

const props = defineProps<{ apiKey: APIKey; catalog: PermissionResource[] }>()

defineEmits<{ (e: 'close'): void }>()

// SANDBOX_GRANTS mirrors permission.ForKey: the flag carries these on
// its own, and they are NOT in the stored list. Pinned to the server by
// TestTheConsoleNamesWhatTheSandboxFlagGrants - a key whose real access
// the view understates is the exact confusion that made the flag look
// broken in the first place.
const SANDBOX_GRANTS = ['sandbox:read', 'sandbox:write', 'sandbox:delete']

/** What this key actually holds, flag included. */
const held = computed(() =>
  props.apiKey.sandbox
    ? [...new Set([...props.apiKey.permissions, ...SANDBOX_GRANTS])]
    : [...props.apiKey.permissions],
)

const expired = computed(
  () => !!props.apiKey.expires_at && new Date(props.apiKey.expires_at) < new Date(),
)

const facts = computed(() => [
  { label: 'Prefix', value: `${props.apiKey.key_prefix}...`, mono: true },
  { label: 'Mode', value: props.apiKey.sandbox ? 'Sandbox - captured, never sent' : 'Live' },
  { label: 'Created', value: formatDate(props.apiKey.created_at) },
  { label: 'Last used', value: formatDate(props.apiKey.last_used_at, 'Never') },
  {
    label: 'Expires',
    value: props.apiKey.expires_at
      ? formatDate(props.apiKey.expires_at) + (expired.value ? ' - expired' : '')
      : 'Never',
  },
  {
    label: 'Usable from',
    value:
      props.apiKey.allowed_ips.length > 0 ? props.apiKey.allowed_ips.join(', ') : 'Any address',
    mono: props.apiKey.allowed_ips.length > 0,
  },
])
</script>

<template>
  <BaseModal :title="apiKey.name" size="modal-w720" @close="$emit('close')">
    <dl class="facts">
      <template v-for="f in facts" :key="f.label">
        <dt>{{ f.label }}</dt>
        <dd :class="{ 'code-font': f.mono }">{{ f.value }}</dd>
      </template>
    </dl>

    <h4 class="section">Permissions</h4>

    <p v-if="apiKey.sandbox" class="text-sm text-muted mb-4">
      The sandbox flag carries its own access. These are not stored on the key - they come with the
      flag, and the flag is all a sandbox key has.
    </p>

    <PermissionGrid :model-value="held" :catalog="catalog" allow-wildcard readonly />

    <template #footer>
      <button class="btn btn-secondary" @click="$emit('close')">Close</button>
    </template>
  </BaseModal>
</template>

<style scoped>
/* Placement only - the list itself is the stylesheet's. Space before
   the Permissions caption that follows it. */
.facts {
  margin-bottom: 20px;
}

/* A caption for the grid under it, not a heading in the document
   sense - so it is sized down rather than up. */
.section {
  margin: 0 0 8px;
  color: var(--text-muted);
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.06em;
  text-transform: uppercase;
}
</style>
