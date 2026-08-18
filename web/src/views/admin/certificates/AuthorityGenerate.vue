<script setup lang="ts">
// Minting an authority of your own.
//
// A separate dialog from the certificate one, because what it asks is
// different in every field that is not the Subject: no hosts (a CA
// carries none), no issuer (it signs itself), a validity measured in
// years rather than the 398 days browsers accept, and no Ed25519 - trust
// stores refuse such a root, so offering it would be offering a mistake.
import { ref } from 'vue'
import { certificatesApi } from '../../../api/certificates'
import { apiErrorMessage } from '../../../api/client'
import { useNotificationStore } from '../../../stores/notification'
import { useFieldErrors } from '../../../composables/fieldErrors'
import { blankSubject, filledSubject } from './subject'
import BaseModal from '../../../components/BaseModal.vue'
import FormField from '../../../components/FormField.vue'
import SubjectFields from './SubjectFields.vue'

const emit = defineEmits<{
  (e: 'created'): void
  (e: 'close'): void
}>()

const notify = useNotificationStore()
const { errors, capture, clear } = useFieldErrors()

const saving = ref(false)

const form = ref({
  name: '',
  algorithm: 'ecdsa',
  validity_days: 3650,
  subject: blankSubject(),
})

async function generate() {
  clear()
  saving.value = true
  try {
    await certificatesApi.generateCA({
      name: form.value.name,
      algorithm: form.value.algorithm,
      validity_days: form.value.validity_days || undefined,
      subject: filledSubject(form.value.subject),
    })
    notify.success('Authority generated - install its certificate wherever it has to be trusted')
    emit('created')
  } catch (e) {
    if (!capture(e)) notify.error(apiErrorMessage(e, 'Failed to generate the authority'))
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <BaseModal title="Generate a certificate authority" @close="emit('close')">
    <p class="text-sm text-muted">
      An authority signs the certificates your listeners serve, so whatever has to trust them trusts
      ONE certificate instead of one per listener. Install its public half - the Download button -
      wherever that is.
    </p>

    <FormField label="Name" for="ca-name" :error="errors.name">
      <input
        id="ca-name"
        v-model="form.name"
        class="form-input"
        type="text"
        placeholder="internal-ca"
      />
    </FormField>

    <div class="form-row">
      <FormField
        label="Algorithm"
        for="ca-alg"
        :error="errors.algorithm"
        hint="No Ed25519 - trust stores refuse such a root."
      >
        <select id="ca-alg" v-model="form.algorithm" class="form-select">
          <option value="ecdsa">ECDSA</option>
          <option value="rsa">RSA</option>
        </select>
      </FormField>
      <FormField
        label="Valid for"
        for="ca-days"
        :error="errors.validity_days"
        hint="Days. Nothing serves this, so it can be long."
      >
        <input
          id="ca-days"
          v-model.number="form.validity_days"
          class="form-input"
          type="number"
          min="1"
          max="7300"
        />
      </FormField>
    </div>

    <SubjectFields
      v-model="form.subject"
      id-prefix="ca"
      :errors="errors"
      :common-name-placeholder="form.name || 'Acme Internal CA'"
      common-name-hint="The common name is what you will recognise it by in a trust store listing, and defaults to the name."
    />

    <template #footer>
      <button class="btn btn-secondary" @click="emit('close')">Cancel</button>
      <button class="btn btn-primary" :disabled="saving" @click="generate">
        {{ saving ? 'Generating...' : 'Generate CA' }}
      </button>
    </template>
  </BaseModal>
</template>
