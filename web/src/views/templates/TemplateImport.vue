<script setup lang="ts">
// Bringing a template in from an exported document.
//
// A FILE OR A PASTE, and the file button only fills the textarea rather
// than sending anything - so what gets imported is always what is on
// screen, and a document that came from somewhere odd can be read before
// it is accepted.
//
// The JSON is parsed HERE so an unparseable paste says so plainly. The
// server would answer "invalid body", which is true and useless.
import { ref } from 'vue'
import { templatesApi, type TemplateExportDoc } from '../../api/templates'
import { apiErrorMessage } from '../../api/client'
import { useNotificationStore } from '../../stores/notification'
import BaseModal from '../../components/BaseModal.vue'
import FormField from '../../components/FormField.vue'

const emit = defineEmits<{
  (e: 'imported'): void
  (e: 'close'): void
}>()

const notify = useNotificationStore()

const text = ref('')
const busy = ref(false)
const fileInput = ref<HTMLInputElement | null>(null)

function pickFile() {
  fileInput.value?.click()
}

async function onFile(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return

  text.value = await file.text()
}

async function run() {
  if (!text.value.trim()) return

  let doc: TemplateExportDoc
  try {
    doc = JSON.parse(text.value)
  } catch {
    notify.error('The document is not valid JSON')

    return
  }

  busy.value = true
  try {
    await templatesApi.import(doc)
    notify.success('Template imported')
    emit('imported')
  } catch (e) {
    // The document is ONE textarea, so there is no input to attribute a
    // refusal of `format` or `versions` to. The server's summary already
    // names the key it refused, which is what a person editing the
    // document needs.
    notify.error(apiErrorMessage(e, 'Failed to import template'))
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <BaseModal title="Import Template" size="modal-w640" @close="emit('close')">
    <FormField hint="Or paste an exported template document below">
      <input
        class="hidden"
        ref="fileInput"
        type="file"
        accept=".json,application/json"
        @change="onFile"
      />
      <button class="btn btn-secondary" @click="pickFile">Choose JSON File</button>
    </FormField>
    <FormField label="Export Document (JSON)">
      <textarea
        v-model="text"
        class="form-textarea code-font"
        rows="12"
        placeholder='{"format": "mailyard-template-v1", "template": {}, "versions": []}'
      ></textarea>
    </FormField>
    <template #footer>
      <button class="btn btn-secondary" @click="emit('close')">Cancel</button>
      <button class="btn btn-primary" :disabled="busy || !text.trim()" @click="run">
        {{ busy ? 'Importing...' : 'Import' }}
      </button>
    </template>
  </BaseModal>
</template>
