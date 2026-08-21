<script setup lang="ts">
// Hand-editing one localization of a template version, beside a live
// render of what it produces.
//
// Split view rather than a preview tab, because the two halves answer
// each other: template syntax is only correct once you see what it
// renders for a given set of data, and switching back and forth to
// check hides exactly the mistakes worth catching.
//
// Three documents make up a localization - subject, HTML and text.
// The two BODIES share one pane as tabs, with sample data as a third
// tab: they are all multi-line editors over the same space. The
// subject is not - it is one line, and as a tab it rendered a whole
// CodeMirror pane for a sentence while hiding the body being written.
// It sits above the pane as a plain input instead, mirroring the
// subject row the preview draws at the same height opposite.
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { onBeforeRouteLeave, useRoute, useRouter } from 'vue-router'
import { templatesApi, type RenderedPreview } from '../../api/templates'
import { stylesheetsApi } from '../../api/stylesheets'
import { languagesApi } from '../../api/languages'
import { apiErrorMessage } from '../../api/client'
import type { Language, Template, TemplateLocalization, TemplateVersion } from '../../api/types'
import { useNotificationStore } from '../../stores/notification'
import { useConfirm } from '../../composables/useConfirm'
import { useCodeMirror } from '../../composables/useCodeMirror'
import RenderedMessage from '../../components/RenderedMessage.vue'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'

type DocumentTab = 'html' | 'text' | 'data'

/** Shown until a version or template supplies its own. */
const EXAMPLE_DATA = '{\n  "name": "John",\n  "company": "Acme"\n}'

/** How long typing has to pause before the preview is re-rendered. */
const PREVIEW_IDLE = 400

const route = useRoute()
const router = useRouter()
const notify = useNotificationStore()
const { confirm } = useConfirm()

const templateId = String(route.params.id)
const versionId = String(route.params.versionId)

const loading = ref(true)
const saving = ref(false)
const dirty = ref(false)

const template = ref<Template | null>(null)
const version = ref<TemplateVersion | null>(null)
const localizations = ref<TemplateLocalization[]>([])
const languages = ref<Language[]>([])
const language = ref('')
const stylesheetCSS = ref('')

const tab = ref<DocumentTab>('html')

// The three pane editors. Each owns its own element, text and
// teardown, so this view holds intent rather than CodeMirror
// bookkeeping.
//
// Two of them are the localization and one is not: editing the sample
// data re-renders the preview but leaves nothing to save, so it must
// not arm the unsaved-changes guard.
const editLocalization = () => {
  dirty.value = true
  schedulePreview()
}

const htmlDoc = useCodeMirror({
  language: 'html',
  placeholder: '<html>...</html>',
  onEdit: editLocalization,
})
const textDoc = useCodeMirror({
  placeholder: 'Plain text version...',
  onEdit: editLocalization,
})
const dataDoc = useCodeMirror({
  language: 'json',
  placeholder: '{"key": "value"}',
  onEdit: () => schedulePreview(),
})

// The subject is a plain input, not a CodeMirror: template syntax works
// in one line exactly as well, and what an editor pane bought was an
// empty page under a sentence.
const subject = ref('')
const subjectInput = ref<HTMLInputElement | null>(null)

const preview = ref<RenderedPreview | null>(null)
const previewOpen = ref(true)
const previewBusy = ref(false)
const previewError = ref('')

/** The localization being edited, or null while a new language is started. */
const current = computed(() => localizations.value.find((l) => l.language === language.value))

/** Project languages that do not have a localization on this version yet. */
const unusedLanguages = computed(() =>
  languages.value.filter((l) => !localizations.value.some((loc) => loc.language === l.code)),
)

/** True when the selected language is neither stored nor on offer. */
const isAdHocLanguage = computed(
  () =>
    language.value !== '' &&
    !localizations.value.some((l) => l.language === language.value) &&
    !unusedLanguages.value.some((l) => l.code === language.value),
)

let idleTimer: ReturnType<typeof setTimeout> | null = null

/**
 * Re-render after typing stops.
 *
 * Every keystroke is a round trip otherwise: the render happens on the
 * server, because a template is rendered there at send time and a
 * preview that used a different engine would not be a preview.
 */
function schedulePreview() {
  if (idleTimer) clearTimeout(idleTimer)
  idleTimer = setTimeout(renderPreview, PREVIEW_IDLE)
}

async function renderPreview() {
  let data: Record<string, unknown>
  try {
    data = JSON.parse(dataDoc.text.value || '{}')
  } catch {
    // Reported in place rather than as a toast: it is a state of the
    // sample-data editor a few characters away, not an event.
    previewError.value = 'The sample data is not valid JSON'

    return
  }

  previewBusy.value = true
  previewError.value = ''
  try {
    const res = await templatesApi.preview({
      // The endpoint requires a subject, and an empty editor is the
      // ordinary state of a template being started.
      subject: subject.value || ' ',
      html: htmlDoc.text.value,
      text: textDoc.text.value,
      // The version's stylesheet, so the preview is styled the way a
      // real send would be rather than as bare markup.
      css: stylesheetCSS.value || undefined,
      data,
    })
    preview.value = res.data.preview
  } catch (e) {
    previewError.value = apiErrorMessage(e, 'Could not render the preview')
  } finally {
    previewBusy.value = false
  }
}

/** Put a localization's three documents into the editors. */
function load(loc: TemplateLocalization | undefined) {
  subject.value = loc?.subject ?? ''
  htmlDoc.set(loc?.html ?? '')
  textDoc.set(loc?.text ?? '')
  dirty.value = false
}

/** Open another language, asking first if there is work to lose. */
async function selectLanguage(next: string): Promise<boolean> {
  if (!next || next === language.value) return false

  if (dirty.value) {
    const discard = await confirm({
      title: 'Discard changes',
      message: `This localization has unsaved changes. Discard them and switch to ${next}?`,
      confirmText: 'Discard',
      variant: 'danger',
    })
    if (!discard) return false
  }

  language.value = next
  load(localizations.value.find((l) => l.language === next))
  void renderPreview()

  return true
}

async function onLanguageChange(event: Event) {
  const select = event.target as HTMLSelectElement
  const opened = await selectLanguage(select.value)

  // Put the control back when the switch was refused. The select is
  // bound to `language`, which did NOT change - so Vue has nothing to
  // re-render and the element would go on displaying a language that
  // was never opened.
  if (!opened) select.value = language.value
}

async function save() {
  if (saving.value || !language.value) return

  if (!subject.value.trim()) {
    notify.error('A localization needs a subject')
    subjectInput.value?.focus()

    return
  }

  saving.value = true
  try {
    // Upserts on (version, language), so this both creates a new
    // localization and updates an existing one.
    const res = await templatesApi.putLocalization(templateId, versionId, {
      language: language.value,
      subject: subject.value,
      html: htmlDoc.text.value,
      text: textDoc.text.value,
    })

    const stored = res.data.localization
    const at = localizations.value.findIndex((l) => l.language === stored.language)
    if (at >= 0) localizations.value[at] = stored
    else localizations.value.push(stored)

    dirty.value = false
    notify.success('Localization saved')
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to save the localization'))
  } finally {
    saving.value = false
  }
}

function onKeydown(e: KeyboardEvent) {
  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 's') {
    e.preventDefault()
    void save()
  }
}

function onBeforeUnload(e: BeforeUnloadEvent) {
  if (dirty.value) e.preventDefault()
}

onBeforeRouteLeave(async () => {
  if (!dirty.value) return true

  return await confirm({
    title: 'Leave the editor',
    message: 'This localization has unsaved changes. Leave and discard them?',
    confirmText: 'Discard',
    variant: 'warning',
  })
})

function openVisualBuilder() {
  router.push({
    name: 'template-builder',
    params: { id: templateId, versionId },
    query: { lang: language.value },
  })
}

function goBack() {
  router.push({ name: 'template-detail', params: { id: templateId } })
}

/** The version's own stylesheet, if it names one. */
async function loadStylesheet(id: string) {
  try {
    const res = await stylesheetsApi.get(id)
    stylesheetCSS.value = res.data.stylesheet?.css ?? ''
  } catch {
    // Renders unstyled, which is what the server does with a
    // stylesheet it cannot read either.
  }
}

onMounted(async () => {
  window.addEventListener('keydown', onKeydown)
  window.addEventListener('beforeunload', onBeforeUnload)

  try {
    const [templateRes, localeRes, languageRes] = await Promise.all([
      templatesApi.get(templateId),
      templatesApi.listLocalizations(templateId, versionId),
      languagesApi.list(),
    ])

    template.value = templateRes.data.template ?? null
    version.value = (templateRes.data.versions ?? []).find((v) => v.id === versionId) ?? null
    localizations.value = localeRes.data.localizations ?? []
    languages.value = languageRes.data.languages ?? []

    if (!template.value || !version.value) {
      loading.value = false

      return
    }

    if (version.value.stylesheet_id) await loadStylesheet(version.value.stylesheet_id)

    // The requested language, then the template's default, then
    // whatever exists. A version with no localizations at all starts
    // one for the default language.
    const wanted = String(route.query.lang ?? '')
    const start =
      localizations.value.find((l) => l.language === wanted) ??
      localizations.value.find((l) => l.language === template.value?.default_language) ??
      localizations.value[0]

    language.value = start?.language || wanted || template.value.default_language || 'en'
    loading.value = false

    // The containers exist only once loading is false, and CodeMirror
    // mounts into a real element - hence nextTick rather than the
    // zero-delay timeout this used to guess with.
    await nextTick()
    subject.value = start?.subject ?? ''
    htmlDoc.mount(start?.html ?? '')
    textDoc.mount(start?.text ?? '')
    dataDoc.mount(version.value.sample_data || template.value.sample_data || EXAMPLE_DATA)
    dirty.value = false

    void renderPreview()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load the template'))
    loading.value = false
  }
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeydown)
  window.removeEventListener('beforeunload', onBeforeUnload)
  if (idleTimer) clearTimeout(idleTimer)
})
</script>

<template>
  <div class="editor">
    <header class="editor-bar">
      <div class="editor-bar-group">
        <button class="btn btn-secondary btn-sm" @click="goBack">Back</button>

        <h2 class="editor-name">{{ template?.name ?? 'Template' }}</h2>

        <!-- Stored localizations and languages that could be added are
             one control, grouped: both answer "which language am I
             editing", and separating them into a picker plus an Add
             button would make starting a translation a different act
             from opening one. -->
        <select
          class="form-select editor-language"
          :value="language"
          aria-label="Language"
          @change="onLanguageChange"
        >
          <optgroup v-if="localizations.length" label="Localizations">
            <option v-for="l in localizations" :key="l.id" :value="l.language">
              {{ l.language }}
            </option>
          </optgroup>
          <optgroup v-if="unusedLanguages.length" label="Add language">
            <option v-for="l in unusedLanguages" :key="l.id" :value="l.code">
              + {{ l.name }} ({{ l.code }})
            </option>
          </optgroup>
          <!-- A language reached by URL that the project does not
               define. Listed so the control shows what is open rather
               than snapping to something else. -->
          <option v-if="isAdHocLanguage" :value="language">{{ language }}</option>
        </select>
      </div>

      <div class="editor-bar-group">
        <!-- State reads beside the actions rather than under the
             name: stacked, three badges crowd the title, and Unsaved
             next to Save is where it actually means something. -->
        <div class="editor-tags">
          <span v-if="version" class="badge badge-neutral">v{{ version.version }}</span>
          <span v-if="!current" class="badge badge-warning">New language</span>
          <span v-if="dirty" class="badge badge-warning">Unsaved</span>
        </div>

        <button class="btn btn-secondary btn-sm" @click="openVisualBuilder">Visual Builder</button>
        <button class="btn btn-secondary btn-sm" @click="previewOpen = !previewOpen">
          {{ previewOpen ? 'Hide Preview' : 'Show Preview' }}
        </button>
        <button class="btn btn-primary btn-sm" :disabled="saving" @click="save">
          {{ saving ? 'Saving...' : 'Save' }}
        </button>
      </div>
    </header>

    <LoadingBlock v-if="loading" />

    <div v-else-if="template && version" class="editor-split" :class="{ solo: !previewOpen }">
      <section class="editor-source">
        <!-- The stylesheet's tab strip, the same one the preview
             pane opposite uses. This pane had invented its own -
             underlined rather than pilled - and putting the two side by
             side is what made that visible. -->
        <div class="strip">
          <div class="tabs">
            <button
              v-for="t in ['html', 'text', 'data'] as DocumentTab[]"
              :key="t"
              class="tab"
              :class="{ active: tab === t }"
              @click="tab = t"
            >
              {{ t === 'html' ? 'HTML' : t === 'text' ? 'Plain Text' : 'Sample Data' }}
            </button>
          </div>
        </div>

        <!-- One line above the pane, mirroring the subject row the
             preview draws at the same height opposite - so what is
             typed here and what it renders to sit on one eye line. -->
        <div class="editor-subject">
          <label class="editor-subject-label" for="editor-subject-input">Subject</label>
          <input
            id="editor-subject-input"
            ref="subjectInput"
            v-model="subject"
            type="text"
            class="form-input editor-subject-input"
            placeholder="e.g. Welcome {{ .name }}"
            @input="editLocalization"
          />
        </div>

        <!-- v-show, not v-if: an editor destroyed on tab change loses
             its undo history and scroll position, and CodeMirror is
             not cheap to build. -->
        <div class="editor-docs">
          <div v-show="tab === 'html'" :ref="htmlDoc.host" class="editor-doc"></div>
          <div v-show="tab === 'text'" :ref="textDoc.host" class="editor-doc"></div>
          <div v-show="tab === 'data'" :ref="dataDoc.host" class="editor-doc"></div>
        </div>
      </section>

      <section v-if="previewOpen" class="editor-preview">
        <!-- The same rendered-message block the preview dialog and the
             preview page use, given the pane's height rather than its
             own. Refreshing is offered here because typing pauses are
             what usually trigger a render and a person who changed the
             stylesheet wants to force one. -->
        <RenderedMessage :preview="preview" :busy="previewBusy" :error="previewError" fill>
          <template #actions>
            <button class="btn btn-secondary btn-sm" :disabled="previewBusy" @click="renderPreview">
              {{ previewBusy ? 'Rendering...' : 'Refresh' }}
            </button>
          </template>
        </RenderedMessage>
      </section>
    </div>

    <EmptyState v-else title="Not found">
      <p>This template or version no longer exists.</p>
      <button class="btn btn-secondary" @click="goBack">Back to templates</button>
    </EmptyState>
  </div>
</template>

<style scoped>
/* Full-bleed like the visual builder: two panes that scroll
   independently, and a page that does not scroll at all. The negative
   margin escapes the layout's content padding, so the height is the
   whole window less the topbar - 52px, which this said was 60 and left
   eight dead pixels under the sample-data pane. dvh rather than vh so a
   mobile browser's collapsing address bar does not hang it below the
   fold, which is the same pair the two-pane reader uses. */
.editor {
  display: flex;
  flex-direction: column;
  height: calc(100dvh - 52px);
  margin: calc(-1 * var(--gutter));
  /* A visible END. Without it the editors run flush into the window
     edge, which reads as a scrollbar that stopped working rather than
     as the bottom of the page - the pane is dark, the edge is dark,
     and nothing says "this is all of it". Half the gutter: enough to
     be an edge, not enough to be a row somebody looks for content in. */
  padding-bottom: calc(var(--gutter) / 2);
  overflow: hidden;
}

.editor-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-shrink: 0;
  gap: 16px;
  padding: 10px 16px;
  border-bottom: 1px solid var(--border-primary);
  background: var(--bg-primary);
}

.editor-bar-group {
  display: flex;
  align-items: center;
  gap: 12px;
}

.editor-name {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
  color: var(--text-primary);
  /* One line, and it keeps its width: overflow:hidden gives a flex
     item an automatic minimum of ZERO, so without flex-shrink:0 the
     name is the first thing the row squeezes and a twelve-character
     title elides to three. Only a genuinely long one is cut. */
  flex-shrink: 0;
  max-width: 24ch;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}

.editor-tags {
  display: flex;
  gap: 6px;
  /* A little air before the buttons, so status and actions read as
     two groups rather than one row of pills. */
  margin-right: 4px;
}

.editor-language {
  max-width: 200px;
  padding: 4px 8px;
  font-size: 13px;
}

/* Equal halves, collapsing to one when the preview is hidden. The
   bottom border is what makes the inset under it read as a frame
   rather than a leak. */
.editor-split {
  display: grid;
  grid-template-columns: 1fr 1fr;
  flex: 1;
  min-height: 0;
  overflow: hidden;
  border-bottom: 1px solid var(--border-primary);
}

.editor-split.solo {
  grid-template-columns: 1fr;
}

.editor-source {
  display: flex;
  flex-direction: column;
  overflow: hidden;
  border-right: 1px solid var(--border-primary);
}

.editor-docs {
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

.editor-doc {
  height: 100%;
  overflow: auto;
}

/* CodeMirror sizes itself to its own wrapper, which is not the element
   we mount into - so the height has to be handed down explicitly. */
.editor-doc :deep(.cm-editor) {
  height: 100%;
}

/* The same recipe as the preview pane's .subject row opposite -
   height, border and background - so the input and what it renders to
   line up across the split. */
.editor-subject {
  display: flex;
  align-items: center;
  flex-shrink: 0;
  gap: 8px;
  padding: 4px 12px;
  border-bottom: 1px solid var(--border-primary);
  background: var(--bg-tertiary);
}

.editor-subject-label {
  color: var(--text-secondary);
  font-size: 13px;
  font-weight: 600;
}

/* form-input, flattened into the bar: the system class keeps the
   focus ring, the placeholder color and the disabled look, and this
   takes back only what makes it a BOX - a bordered rounded field
   inside an 8px bar reads as a form, and this row is a title line. */
.editor-subject-input.form-input {
  height: auto;
  padding: 4px 0;
  border: none;
  border-radius: 0;
  background: transparent;
  font-size: 13px;
}

.editor-subject-input.form-input:hover:not(:disabled):not(:focus) {
  border: none;
}

.editor-preview {
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: var(--bg-primary);
}
</style>
