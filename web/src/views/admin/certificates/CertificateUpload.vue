<script setup lang="ts">
// Storing a pair you already have.
//
// Wide, because the two textareas hold PEM: at the default dialog width
// a certificate wraps onto forty lines and nothing about it is legible.
import { ref } from 'vue'
import { certificatesApi } from '../../../api/certificates'
import { apiErrorMessage } from '../../../api/client'
import { useNotificationStore } from '../../../stores/notification'
import { useFieldErrors } from '../../../composables/fieldErrors'
import BaseModal from '../../../components/BaseModal.vue'
import FormField from '../../../components/FormField.vue'

const emit = defineEmits<{
  (e: 'created'): void
  (e: 'close'): void
}>()

const notify = useNotificationStore()
const { errors, capture, clear } = useFieldErrors()

const saving = ref(false)
const form = ref({ name: '', certificate: '', private_key: '' })

async function upload() {
  clear()
  saving.value = true
  try {
    await certificatesApi.upload(form.value)
    notify.success('Certificate stored')
    emit('created')
  } catch (e) {
    if (!capture(e)) notify.error(apiErrorMessage(e, 'Failed to store the certificate'))
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <BaseModal title="Upload a certificate" size="modal-w900" @close="emit('close')">
    <FormField label="Name" for="cert-name" :error="errors.name">
      <input
        id="cert-name"
        v-model="form.name"
        class="form-input"
        type="text"
        placeholder="production"
      />
    </FormField>

    <FormField
      label="Certificate"
      for="cert-pem"
      :error="errors.certificate"
      hint="The full chain, leaf first, if you have one."
    >
      <textarea
        id="cert-pem"
        v-model="form.certificate"
        class="form-textarea code-font"
        rows="8"
        placeholder="-----BEGIN CERTIFICATE-----"
      ></textarea>
    </FormField>

    <FormField
      label="Private key"
      for="cert-key"
      :error="errors.private_key"
      hint="Checked against the certificate before anything is stored, and encrypted at rest."
    >
      <textarea
        id="cert-key"
        v-model="form.private_key"
        class="form-textarea code-font"
        rows="6"
        placeholder="-----BEGIN PRIVATE KEY-----"
      ></textarea>
    </FormField>

    <template #footer>
      <button class="btn btn-secondary" @click="emit('close')">Cancel</button>
      <button class="btn btn-primary" :disabled="saving" @click="upload">
        {{ saving ? 'Saving...' : 'Save' }}
      </button>
    </template>
  </BaseModal>
</template>
