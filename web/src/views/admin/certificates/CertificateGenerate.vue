<script setup lang="ts">
// Minting a certificate a listener can serve.
//
// The dialog owns the write and reports `created`, the way every other
// card on this page does - the page owns the data and the reload, and
// nothing here needs to know what is already stored except which
// authorities can sign.
import { computed, ref } from 'vue'
import { certificatesApi, type ManagedCertificate } from '../../../api/certificates'
import { apiErrorMessage } from '../../../api/client'
import { useNotificationStore } from '../../../stores/notification'
import { useFieldErrors } from '../../../composables/fieldErrors'
import { blankSubject, filledSubject } from './subject'
import BaseModal from '../../../components/BaseModal.vue'
import FormField from '../../../components/FormField.vue'
import SubjectFields from './SubjectFields.vue'

const props = defineProps<{
  /** The authorities on hand, which is what the issuer picker offers. */
  authorities: ManagedCertificate[]
}>()

const emit = defineEmits<{
  (e: 'created'): void
  (e: 'close'): void
}>()

const notify = useNotificationStore()
const { errors, capture, clear } = useFieldErrors()

const saving = ref(false)

const form = ref({
  name: '',
  hosts: '',
  algorithm: 'ecdsa',
  issuer: '',
  validity_days: 365,
  subject: blankSubject(),
})

// The first host, which is what the server names the certificate after
// when the common name is left empty. Shown as the placeholder so the
// field says what leaving it alone will do.
const firstHost = computed(() => form.value.hosts.split(',')[0].trim() || 'mail.internal')

async function generate() {
  clear()
  saving.value = true
  try {
    await certificatesApi.generate({
      name: form.value.name,
      hosts: form.value.hosts
        .split(',')
        .map((h) => h.trim())
        .filter(Boolean),
      algorithm: form.value.algorithm,
      issuer: form.value.issuer || undefined,
      validity_days: form.value.validity_days || undefined,
      subject: filledSubject(form.value.subject),
    })
    notify.success('Certificate generated')
    emit('created')
  } catch (e) {
    if (!capture(e)) notify.error(apiErrorMessage(e, 'Failed to generate the certificate'))
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <BaseModal title="Generate a certificate" @close="emit('close')">
    <FormField label="Name" for="gen-name" :error="errors.name">
      <input
        id="gen-name"
        v-model="form.name"
        class="form-input"
        type="text"
        placeholder="internal"
      />
    </FormField>

    <FormField
      label="Hosts"
      for="gen-hosts"
      :error="errors.hosts"
      hint="Comma separated. They go in the SAN list, and at least one is needed."
    >
      <input
        id="gen-hosts"
        v-model="form.hosts"
        class="form-input"
        type="text"
        placeholder="mail.internal, localhost"
      />
    </FormField>

    <div class="form-row">
      <FormField label="Algorithm" for="gen-alg" :error="errors.algorithm">
        <select id="gen-alg" v-model="form.algorithm" class="form-select">
          <option value="ecdsa">ECDSA</option>
          <option value="rsa">RSA</option>
          <option value="ed25519">Ed25519</option>
        </select>
      </FormField>
      <FormField
        label="Valid for"
        for="gen-days"
        :error="errors.validity_days"
        hint="Days, at most 398 - browsers refuse a longer one."
      >
        <input
          id="gen-days"
          v-model.number="form.validity_days"
          class="form-input"
          type="number"
          min="1"
          max="398"
        />
      </FormField>
    </div>

    <FormField
      label="Signed by"
      for="gen-issuer"
      :error="errors.issuer"
      :hint="
        authorities.length
          ? 'A certificate signed by your own authority is trusted by anything that trusts it.'
          : 'Generate a CA first to have something to sign with.'
      "
    >
      <select id="gen-issuer" v-model="form.issuer" class="form-select">
        <option value="">Itself (self-signed)</option>
        <option v-for="a in props.authorities" :key="a.name" :value="a.name">{{ a.name }}</option>
      </select>
    </FormField>

    <SubjectFields
      v-model="form.subject"
      id-prefix="gen"
      :errors="errors"
      :common-name-placeholder="firstHost"
      common-name-hint="The common name defaults to the first host."
    />

    <template #footer>
      <button class="btn btn-secondary" @click="emit('close')">Cancel</button>
      <button class="btn btn-primary" :disabled="saving" @click="generate">
        {{ saving ? 'Generating...' : 'Generate' }}
      </button>
    </template>
  </BaseModal>
</template>
