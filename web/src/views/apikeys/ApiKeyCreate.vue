<script setup lang="ts">
// Mint an API key.
//
// Everything here is decided ONCE: the permissions, the sandbox flag,
// the address list and the expiry are all fixed at creation, because a
// credential whose reach can be widened later is a credential nobody
// can reason about from its audit trail.
//
// Its own component, and not the detail view wearing `:disabled`. That
// is what it was - one form with nine `viewing !== null` conditionals
// deciding whether each control was live - and the form stayed armed
// with a create action while it was showing an existing key.
import { computed, ref, watch } from 'vue'
import { apiKeysApi, type CreateKeyPayload } from '../../api/apikeys'
import { apiErrorMessage } from '../../api/client'
import type { APIKey, PermissionResource } from '../../api/types'
import { useNotificationStore } from '../../stores/notification'
import { useExpiry } from '../../composables/useExpiry'
import { useFieldErrors } from '../../composables/fieldErrors'
import BaseModal from '../../components/BaseModal.vue'
import FormField from '../../components/FormField.vue'
import PermissionGrid from '../../components/PermissionGrid.vue'

defineProps<{ catalog: PermissionResource[] }>()

const emit = defineEmits<{
  /** Minted. The token is readable exactly once, so it comes with it. */
  (e: 'created', key: APIKey, token: string): void
  (e: 'close'): void
}>()

const notify = useNotificationStore()
const { errors, capture, clear } = useFieldErrors()
const { never, at, reset, invalid: expiryInvalid, payload: expiryPayload } = useExpiry()

const name = ref('')
const permissions = ref<string[]>([])
const addresses = ref('')
const sandbox = ref(false)
const gridInvalid = ref(false)
const busy = ref(false)

reset()

// A sandbox key is defined by the flag alone - the server grants it
// sandbox access and ignores a permission list - so ticking the box
// drops what was selected rather than submitting it unseen.
watch(sandbox, (on) => {
  if (!on) return

  permissions.value = []
  gridInvalid.value = false
})

const ready = computed(
  () => !busy.value && name.value.trim() !== '' && !gridInvalid.value && !expiryInvalid.value,
)

/** One address or CIDR per line, commas tolerated because people paste. */
function addressList(): string[] {
  return addresses.value
    .split(/[\n,]/)
    .map((a) => a.trim())
    .filter(Boolean)
}

async function submit() {
  if (!ready.value) return

  clear()
  busy.value = true

  const body: CreateKeyPayload = { name: name.value.trim(), permissions: [...permissions.value] }
  const allowed = addressList()
  if (allowed.length > 0) body.allowed_ips = allowed

  const expires = expiryPayload()
  if (expires) body.expires_at = expires
  if (sandbox.value) body.sandbox = true

  try {
    const res = await apiKeysApi.create(body)
    emit('created', res.data.api_key, res.data.token)
  } catch (e) {
    if (!capture(e)) notify.error(apiErrorMessage(e, 'Failed to create the key'))
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <BaseModal title="New API key" form @submit="submit" @close="emit('close')">
    <FormField
      label="Name"
      :error="errors.name"
      hint="Only ever shown here, so name it after what will hold it."
    >
      <input v-model="name" class="form-input" placeholder="Production" required />
    </FormField>

    <!-- No error slot: a checkbox can only send true or false, so there
         is nothing here the server refuses by field. -->
    <FormField
      hint="Works only with the Inbound Sandbox: mail is captured and never delivered. This cannot be switched off later."
    >
      <label class="checkbox-label">
        <input v-model="sandbox" type="checkbox" />
        <span>Sandbox key</span>
      </label>
    </FormField>

    <!-- Hidden rather than disabled while sandbox is on: the flag
         carries its own access, so an empty grid there is not a choice
         anybody is being asked to make. -->
    <FormField
      v-if="!sandbox"
      label="Permissions"
      :error="errors.permissions"
      hint="Fixed at creation. A key with nothing selected can do nothing - the same catalogue governs people and machines."
    >
      <PermissionGrid
        v-model="permissions"
        :catalog="catalog"
        allow-wildcard
        @update:invalid="gridInvalid = $event"
      />
    </FormField>

    <FormField
      :error="errors.allowed_ips"
      hint="One address or CIDR per line. Empty means the key works from anywhere."
    >
      <template #label>Allowed addresses <span class="text-muted">(optional)</span></template>
      <textarea
        v-model="addresses"
        class="form-textarea code-font"
        rows="3"
        :placeholder="'192.168.1.1\n10.0.0.0/24'"
      ></textarea>
    </FormField>

    <FormField label="Expiry" :error="errors.expires_at">
      <label class="checkbox-label">
        <input v-model="never" type="checkbox" />
        <span>Never expires</span>
      </label>
      <!-- A date is REQUIRED unless "never" is ticked, so a cleared
           field cannot quietly mint a permanent credential. -->
      <input v-if="!never" v-model="at" type="datetime-local" class="form-input" required />
    </FormField>

    <template #footer>
      <button type="button" class="btn btn-secondary" @click="emit('close')">Cancel</button>
      <button type="submit" class="btn btn-primary" :disabled="!ready">
        {{ busy ? 'Creating...' : 'Create' }}
      </button>
    </template>
  </BaseModal>
</template>
