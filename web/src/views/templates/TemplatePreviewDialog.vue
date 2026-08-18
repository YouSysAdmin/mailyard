<script setup lang="ts">
// Render one localization the way the server would, against data you
// can edit in place.
//
// The render happens SERVER side - the same code path a real send uses -
// so what this shows is the message, not a browser's approximation of
// it. That is also why the data box is here: the interesting failures
// are a field the template names and the data does not carry.
import { ref } from 'vue'
import { templatesApi, type RenderedPreview } from '../../api/templates'
import { apiErrorMessage } from '../../api/client'
import BaseModal from '../../components/BaseModal.vue'
import FormField from '../../components/FormField.vue'
import RenderedMessage from '../../components/RenderedMessage.vue'

const props = defineProps<{
  templateId: string
  versionId: string
  language: string
  /** What the data box starts holding - the version's sample data. */
  sampleData?: string
}>()

defineEmits<{ (e: 'close'): void }>()

const data = ref(props.sampleData || '{}')
const rendered = ref<RenderedPreview | null>(null)
const busy = ref(false)
const failure = ref('')

async function render() {
  busy.value = true
  failure.value = ''
  rendered.value = null

  let values: Record<string, unknown>
  try {
    values = JSON.parse(data.value || '{}')
  } catch {
    // Reported inside the render rather than as a toast: the box
    // holding the bad JSON is right above it.
    failure.value = 'The sample data is not valid JSON.'
    busy.value = false

    return
  }

  try {
    const res = await templatesApi.previewVersion(props.templateId, props.versionId, {
      language: props.language || undefined,
      data: values,
    })
    rendered.value = res.data.preview
  } catch (e) {
    failure.value = apiErrorMessage(e, 'Failed to render the preview')
  } finally {
    busy.value = false
  }
}

void render()
</script>

<template>
  <BaseModal size="modal-w800" @close="$emit('close')">
    <template #header>
      <h3>Preview - {{ language }}</h3>
    </template>

    <FormField label="Sample data (JSON)">
      <textarea
        v-model="data"
        class="form-textarea code-font"
        rows="3"
        placeholder='{"name": "John"}'
      ></textarea>
    </FormField>

    <RenderedMessage :preview="rendered" :busy="busy" :error="failure">
      <template #actions>
        <button class="btn btn-secondary btn-sm" :disabled="busy" @click="render">
          {{ busy ? 'Rendering...' : 'Render again' }}
        </button>
      </template>
    </RenderedMessage>

    <template #footer>
      <button class="btn btn-secondary" @click="$emit('close')">Close</button>
    </template>
  </BaseModal>
</template>
