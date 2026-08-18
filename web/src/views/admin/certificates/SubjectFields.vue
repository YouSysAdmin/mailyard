<script setup lang="ts">
// The distinguished name on a certificate about to be minted.
//
// Six fields, and both dialogs that mint something ask for all six in
// the same order - so they were the same fifty lines of markup twice,
// differing only in an id prefix and one placeholder. The two that gave
// them away: `subject.locality` is labelled City in both, and only one
// of them said the whole block was optional.
//
// It is optional. An empty Subject is a real answer - the server falls
// back to the first host for a certificate and to the name for an
// authority - which is why nothing here is marked required.
import type { CertificateSubject } from '../../../api/certificates'
import FormField from '../../../components/FormField.vue'

defineProps<{
  /**
   * Prefixes each control's id, so a label points at its own field and
   * not at the other dialog's. Both are mounted behind v-if and only
   * one is ever open, but an id is a document-wide name and writing two
   * of them is how that stops being true.
   */
  idPrefix: string
  /** Field errors from the last refused save, keyed by json name. */
  errors: Record<string, string>
  /** What the common name falls back to, shown as its placeholder. */
  commonNamePlaceholder: string
  commonNameHint?: string
}>()

const subject = defineModel<CertificateSubject>({ required: true })
</script>

<template>
  <div>
    <h4 class="subject-heading">Subject</h4>
    <p class="form-hint">All optional. {{ commonNameHint || 'It can be left entirely empty.' }}</p>

    <FormField label="Common name" :for="idPrefix + '-cn'" :error="errors.common_name">
      <input
        :id="idPrefix + '-cn'"
        v-model="subject.common_name"
        class="form-input"
        type="text"
        :placeholder="commonNamePlaceholder"
      />
    </FormField>

    <div class="form-row">
      <FormField label="Organization" :for="idPrefix + '-org'" :error="errors.organization">
        <input
          :id="idPrefix + '-org'"
          v-model="subject.organization"
          class="form-input"
          type="text"
        />
      </FormField>
      <FormField label="Unit" :for="idPrefix + '-unit'" :error="errors.unit">
        <input :id="idPrefix + '-unit'" v-model="subject.unit" class="form-input" type="text" />
      </FormField>
      <FormField label="Country" :for="idPrefix + '-country'" :error="errors.country">
        <input
          :id="idPrefix + '-country'"
          v-model="subject.country"
          class="form-input"
          type="text"
          maxlength="2"
          placeholder="UA"
        />
      </FormField>
      <FormField label="State" :for="idPrefix + '-state'" :error="errors.state">
        <input :id="idPrefix + '-state'" v-model="subject.state" class="form-input" type="text" />
      </FormField>
      <FormField label="City" :for="idPrefix + '-city'" :error="errors.locality">
        <input :id="idPrefix + '-city'" v-model="subject.locality" class="form-input" type="text" />
      </FormField>
    </div>
  </div>
</template>

<style scoped>
.subject-heading {
  margin: 20px 0 4px;
  font-size: 0.9rem;
}
</style>
