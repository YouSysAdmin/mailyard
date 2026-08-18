<script setup lang="ts">
// A whole page for previewing a template, reached from the list.
//
// The template page has a preview DIALOG on each localization row, which
// is the quick look. This is the other question: comparing versions and
// languages against the same data, where a dialog you have to close and
// reopen is the wrong shape.
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { templatesApi, type RenderedPreview } from '../../api/templates'
import { apiErrorMessage } from '../../api/client'
import type { Template, TemplateLocalization, TemplateVersion } from '../../api/types'
import { useNotificationStore } from '../../stores/notification'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import PageHeader from '../../components/PageHeader.vue'
import FormField from '../../components/FormField.vue'
import RenderedMessage from '../../components/RenderedMessage.vue'

/** How long typing has to pause before the render is asked for again. */
const IDLE = 500

const route = useRoute()
const router = useRouter()
const notify = useNotificationStore()

const templateId = String(route.params.id)

const template = ref<Template | null>(null)
const versions = ref<TemplateVersion[]>([])
const localizations = ref<TemplateLocalization[]>([])
const loading = ref(true)

const versionId = ref('')
const language = ref('')
const data = ref('{}')

const rendered = ref<RenderedPreview | null>(null)
const busy = ref(false)
const failure = ref('')

const version = computed(() => versions.value.find((v) => v.id === versionId.value))

/** Which version is live, marked so the reader knows what sends today. */
function versionLabel(v: TemplateVersion): string {
  return v.id === template.value?.active_version_id ? `v${v.version} (active)` : `v${v.version}`
}

async function load() {
  try {
    const res = await templatesApi.get(templateId)
    template.value = res.data.template ?? null
    versions.value = res.data.versions ?? []

    if (!template.value) return

    const active = versions.value.find((v) => v.id === template.value?.active_version_id)
    const start = active ?? versions.value[0]
    if (start) {
      versionId.value = start.id
      await onVersion()
    }
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load the template'))
  } finally {
    loading.value = false
  }
}

/** A different version is a different set of languages and its own data. */
async function onVersion() {
  rendered.value = null
  failure.value = ''
  localizations.value = []
  language.value = ''
  if (!versionId.value) return

  data.value = version.value?.sample_data || template.value?.sample_data || '{}'

  try {
    const res = await templatesApi.listLocalizations(templateId, versionId.value)
    localizations.value = res.data.localizations ?? []
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load the localizations'))

    return
  }

  const preferred =
    localizations.value.find((l) => l.language === template.value?.default_language) ??
    localizations.value[0]
  language.value = preferred?.language ?? ''
  if (language.value) await render()
}

async function render() {
  if (!versionId.value || !language.value) return

  let values: Record<string, unknown>
  try {
    values = JSON.parse(data.value || '{}')
  } catch {
    failure.value = 'The sample data is not valid JSON.'

    return
  }

  busy.value = true
  failure.value = ''
  try {
    const res = await templatesApi.previewVersion(templateId, versionId.value, {
      language: language.value,
      data: values,
    })
    rendered.value = res.data.preview
  } catch (e) {
    failure.value = apiErrorMessage(e, 'Failed to render')
  } finally {
    busy.value = false
  }
}

// Typing in the data box re-renders once the typing stops. Every
// keystroke is a round trip otherwise, since the render is the server's.
let idle: ReturnType<typeof setTimeout> | undefined
watch(data, () => {
  clearTimeout(idle)
  idle = setTimeout(render, IDLE)
})

void load()
</script>

<template>
  <div>
    <PageHeader :title="template ? `Preview - ${template.name}` : 'Preview'">
      <button class="btn btn-secondary" @click="router.push({ name: 'templates' })">
        All templates
      </button>
    </PageHeader>

    <LoadingBlock v-if="loading" />

    <template v-else-if="template">
      <div class="card">
        <div class="card-body">
          <div class="form-row">
            <FormField label="Version">
              <select v-model="versionId" class="form-select" @change="onVersion">
                <option value="" disabled>Pick a version</option>
                <option v-for="v in versions" :key="v.id" :value="v.id">
                  {{ versionLabel(v) }}
                </option>
              </select>
            </FormField>

            <FormField label="Language">
              <select
                v-model="language"
                class="form-select"
                :disabled="localizations.length === 0"
                @change="render"
              >
                <option value="" disabled>Pick a language</option>
                <option v-for="l in localizations" :key="l.id" :value="l.language">
                  {{ l.language }}
                </option>
              </select>
            </FormField>
          </div>

          <p v-if="versions.length === 0" class="form-hint">
            This template has no versions yet, so there is nothing to render.
          </p>
          <p v-else-if="versionId && localizations.length === 0" class="form-hint">
            This version has no content in any language yet.
          </p>
        </div>
      </div>

      <div v-if="language" class="card">
        <div class="card-body">
          <FormField
            label="Sample data (JSON)"
            hint="Re-renders as you stop typing. Fields the template names and this does not carry come out empty."
          >
            <textarea
              v-model="data"
              class="form-textarea code-font"
              rows="5"
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
        </div>
      </div>
    </template>

    <EmptyState v-else title="No such template" />
  </div>
</template>
