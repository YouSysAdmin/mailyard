<script setup lang="ts">
// One template, and the things hanging off it.
//
// Assembly. A template is four separate concerns - what it is, the files
// it carries, its versions, and the content of the version you are
// looking at - and each of them owns its own loading, its own dialogs
// and its own writes. What is left here is the two facts only the page
// can hold: which template, and which version the cards below belong to.
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { templatesApi } from '../../api/templates'
import { stylesheetsApi } from '../../api/stylesheets'
import { languagesApi } from '../../api/languages'
import { apiErrorMessage } from '../../api/client'
import type { Language, Stylesheet, Template, TemplateVersion } from '../../api/types'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import PageHeader from '../../components/PageHeader.vue'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import TemplateSummary from './TemplateSummary.vue'
import TemplateAttachments from './TemplateAttachments.vue'
import TemplateVersions from './TemplateVersions.vue'
import TemplateLocalizations from './TemplateLocalizations.vue'
import TemplateSendTest from './TemplateSendTest.vue'
import TemplateSettings from './TemplateSettings.vue'

const route = useRoute()
const router = useRouter()
const notify = useNotificationStore()
const projects = useProjectStore()

const templateId = String(route.params.id)

const template = ref<Template | null>(null)
const stylesheets = ref<Stylesheet[]>([])
const languages = ref<Language[]>([])
const loading = ref(true)

// Reported back by the versions card, which owns them. The page needs
// the list only to name the active one, and the selection only to say
// which version the content card should show.
const versions = ref<TemplateVersion[]>([])
const selected = ref<TemplateVersion | null>(null)

// Reported back by the content card, for the test send's language list -
// offering a language the template has no content for would send the
// default and look like the picker was ignored.
const written = ref<string[]>([])

const settingsOpen = ref(false)
const testOpen = ref(false)

const canSendTest = computed(() => !!template.value?.active_version_id)

// The version's own data if it has any, else the template's. A version
// created through the console inherits it, but one created through the
// API need not - and a preview against {} renders "<no value>" where
// every field should be, which reads as a broken template.
const sampleData = computed(() => selected.value?.sample_data || template.value?.sample_data)

async function load() {
  loading.value = true
  try {
    // Three independent reads, so they go together. Stylesheets and
    // languages are the pickers the cards below render, not the page's
    // own data - they are fetched here because both cards need them and
    // neither should ask twice.
    const [tmpl, styles, langs] = await Promise.all([
      templatesApi.get(templateId),
      stylesheetsApi.list(),
      languagesApi.list(),
    ])
    template.value = tmpl.data.template ?? null
    stylesheets.value = styles.data.stylesheets ?? []
    languages.value = langs.data.languages ?? []
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load the template'))
  } finally {
    loading.value = false
  }
}

void load()
</script>

<template>
  <div>
    <PageHeader :title="template?.name || 'Template'">
      <template v-if="projects.can('templates:write')">
        <button class="btn btn-primary" :disabled="!canSendTest" @click="testOpen = true">
          Send a test
        </button>
        <button class="btn btn-outline-primary" @click="settingsOpen = true">Settings</button>
      </template>
      <button class="btn btn-secondary" @click="router.push({ name: 'templates' })">
        All templates
      </button>
    </PageHeader>

    <LoadingBlock v-if="loading" />

    <template v-else-if="template">
      <TemplateSummary :template="template" :versions="versions" />

      <TemplateAttachments :template-id="templateId" />

      <TemplateVersions
        :template-id="templateId"
        :stylesheets="stylesheets"
        :active-id="template.active_version_id || ''"
        :default-sample-data="template.sample_data"
        @versions="versions = $event"
        @select="selected = $event"
        @activate="template.active_version_id = $event"
      />

      <TemplateLocalizations
        v-if="selected"
        :template-id="templateId"
        :version="selected"
        :default-language="template.default_language || ''"
        :languages="languages"
        :sample-data="sampleData"
        @languages="written = $event"
      />

      <TemplateSettings
        v-if="settingsOpen"
        :template="template"
        :languages="languages"
        @saved="template = $event"
        @close="settingsOpen = false"
      />

      <TemplateSendTest
        v-if="testOpen"
        :template-id="templateId"
        :languages="written"
        :default-language="template.default_language || 'en'"
        :sample-data="sampleData"
        @close="testOpen = false"
      />
    </template>

    <EmptyState v-else title="No such template" />
  </div>
</template>
