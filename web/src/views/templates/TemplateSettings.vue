<script setup lang="ts">
// Edit the template itself - the fields that outlive every version.
import { ref } from 'vue'
import { templatesApi } from '../../api/templates'
import { apiErrorMessage } from '../../api/client'
import type { Language, Template } from '../../api/types'
import { useNotificationStore } from '../../stores/notification'
import { useFieldErrors } from '../../composables/fieldErrors'
import BaseModal from '../../components/BaseModal.vue'
import FormField from '../../components/FormField.vue'

const props = defineProps<{
  template: Template
  languages: Language[]
}>()

const emit = defineEmits<{
  (e: 'saved', template: Template): void
  (e: 'close'): void
}>()

const notify = useNotificationStore()
const { errors, capture, clear } = useFieldErrors()

const form = ref({
  name: props.template.name,
  description: props.template.description || '',
  default_language: props.template.default_language || 'en',
  sample_data: props.template.sample_data || '',
})
const saving = ref(false)

async function save() {
  clear()
  if (!form.value.name.trim()) return

  // Refused here rather than by the server, because the server stores
  // this string opaquely - it is the preview and the test send that
  // would fail, later, somewhere else.
  if (form.value.sample_data.trim()) {
    try {
      JSON.parse(form.value.sample_data)
    } catch {
      notify.error('Sample data must be valid JSON')

      return
    }
  }

  saving.value = true
  try {
    const res = await templatesApi.update(props.template.id, form.value)
    emit('saved', res.data.template)
    notify.success('Template updated')
    emit('close')
  } catch (e) {
    if (!capture(e)) notify.error(apiErrorMessage(e, 'Failed to update the template'))
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <BaseModal title="Template settings" size="modal-w560" @close="$emit('close')">
    <FormField label="Name" :error="errors.name">
      <input v-model="form.name" class="form-input" />
    </FormField>

    <FormField label="Description" :error="errors.description">
      <input v-model="form.description" class="form-input" />
    </FormField>

    <FormField label="Default language" :error="errors.default_language">
      <select v-if="languages.length" v-model="form.default_language" class="form-select">
        <option v-for="lang in languages" :key="lang.id" :value="lang.code">
          {{ lang.name }} ({{ lang.code }})
        </option>
        <!-- The stored value may name a language the project has since
             removed. Offering it keeps the select honest instead of
             silently showing the first option as if it were the one. -->
        <option
          v-if="!languages.some((l) => l.code === form.default_language)"
          :value="form.default_language"
        >
          {{ form.default_language }}
        </option>
      </select>
      <input v-else v-model="form.default_language" class="form-input" placeholder="en" />
    </FormField>

    <FormField
      label="Sample data (JSON)"
      :error="errors.sample_data"
      hint="What a new version starts from."
    >
      <textarea
        v-model="form.sample_data"
        class="form-textarea code-font"
        rows="5"
        placeholder='{"name": "John"}'
      ></textarea>
    </FormField>

    <template #footer>
      <button class="btn btn-secondary" @click="$emit('close')">Cancel</button>
      <button class="btn btn-primary" :disabled="saving || !form.name.trim()" @click="save">
        {{ saving ? 'Saving...' : 'Save' }}
      </button>
    </template>
  </BaseModal>
</template>
