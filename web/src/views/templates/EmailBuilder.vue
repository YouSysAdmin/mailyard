<script setup lang="ts">
// Drag-and-drop editing for one localization of one template version.
//
// GrapesJS with its newsletter preset does the work: email HTML is
// table-layout HTML, and the preset is what supplies blocks that
// survive Outlook rather than the div-and-flexbox output a generic
// page builder emits.
//
// The sibling route is the code editor, and the two are deliberately
// one-way compatible: anything drawn here opens there, but hand-written
// HTML opened here is re-serialised by GrapesJS and will not come back
// byte for byte. That is why switching to code is a button and
// switching back is not offered.
import { computed, onBeforeUnmount, onMounted, ref, useTemplateRef } from 'vue'
import { onBeforeRouteLeave, useRoute, useRouter } from 'vue-router'
import grapesjs, { type Editor, type PropertySelect } from 'grapesjs'
import newsletterPreset from 'grapesjs-preset-newsletter'
import 'grapesjs/dist/css/grapes.min.css'
import { templatesApi } from '../../api/templates'
import { apiErrorMessage } from '../../api/client'
import type { Template, TemplateLocalization, TemplateVersion } from '../../api/types'
import { useNotificationStore } from '../../stores/notification'
import { useConfirm } from '../../composables/useConfirm'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'

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
const localization = ref<TemplateLocalization | null>(null)

const canvas = useTemplateRef<HTMLElement>('canvas')
let editor: Editor | null = null

/** Everything needed to edit arrived, so the canvas can be mounted. */
const ready = computed(() => template.value !== null && localization.value !== null)

/**
 * Widths the preview switches between.
 *
 * `widthMedia` is what the emitted media queries are written against
 * and is deliberately WIDER than the frame itself: a phone reports a
 * viewport near 480px through its mail client's chrome, so a query cut
 * at exactly 375px would miss the devices it was drawn for.
 */
const devices = [
  { name: 'Desktop', width: '' },
  { name: 'Tablet', width: '768px', widthMedia: '992px' },
  // The preset's toolbar switches by NAME, and these are the three
  // names its commands ask for. Called Mobile, the third button did
  // nothing at all.
  { name: 'Mobile portrait', width: '375px', widthMedia: '480px' },
]

/**
 * The font menu.
 *
 * Mail clients fetch no fonts, so a name here is a promise that the
 * recipient's machine has it. These ship with both Windows and macOS,
 * each with the generic family it falls back to elsewhere. GrapesJS'
 * own list is replaced rather than extended: it carried Comic Sans and
 * Brush Script, which nobody puts in a transactional message, and
 * lacked Palatino, which is the one serif both platforms ship besides
 * Georgia and Times.
 */
const fonts = [
  'Arial, Helvetica, sans-serif',
  'Helvetica, Arial, sans-serif',
  'Verdana, Geneva, sans-serif',
  'Tahoma, Geneva, sans-serif',
  'Trebuchet MS, Helvetica, sans-serif',
  'Lucida Sans Unicode, Lucida Grande, sans-serif',
  'Arial Black, Gadget, sans-serif',
  'Impact, Charcoal, sans-serif',
  'Georgia, serif',
  'Times New Roman, Times, serif',
  'Palatino Linotype, Book Antiqua, Palatino, serif',
  'Courier New, Courier, monospace',
].map((font) => ({ id: font, label: font.split(',')[0] }))

/** Words for the typography toggles the preset draws as icons. */
const radioLabels: Record<string, Record<string, string>> = {
  'text-decoration': { none: 'None', underline: 'Underline', 'line-through': 'Strike' },
  'font-style': { normal: 'Normal', italic: 'Italic' },
}

function startEditor(html: string) {
  if (!canvas.value) return

  editor = grapesjs.init({
    container: canvas.value,
    height: '100%',
    width: 'auto',
    fromElement: false,
    // Persistence is ours: the document belongs to a template version
    // behind the API, and GrapesJS' own storage would write it to
    // local storage where nothing else can see it.
    storageManager: false,
    // The default pulls icons from a font-awesome CDN, which the
    // console's CSP refuses outright - the toolbar ships inline SVG
    // regardless, so the stylesheet is pure breakage.
    cssIcons: '',
    // Phones home to grapesjs.com on load. The console's CSP refuses
    // the connection anyway, so all it did was log two errors.
    telemetry: false,
    plugins: [newsletterPreset],
    deviceManager: { devices },
  })

  if (html) editor.setComponents(html)

  // Preview hides every panel and leaves one way back, a span GrapesJS
  // creates on first use and styles below. It is given a tooltip here
  // because the stylesheet cannot.
  editor.on('command:run:preview', () => {
    editor?.getEl()?.querySelector('.gjs-off-prv')?.setAttribute('title', 'Exit preview')
  })

  // On load, not right after init: the preset installs its sectors
  // while the editor is still coming up, and a list written before
  // that is written over.
  editor.on('load', () => {
    const sm = editor?.StyleManager
    if (!sm) return

    const fontProp = sm.getProperty('typography', 'font-family')
    if (fontProp && 'setOptions' in fontProp) (fontProp as PropertySelect).setOptions(fonts)

    // Two of the typography toggles are drawn by the preset as Font
    // Awesome glyphs, which the console does not ship, so they rendered
    // as blank bars with nothing to say which was which. Worded like
    // the text-align row beside them.
    for (const [property, labels] of Object.entries(radioLabels)) {
      const prop = sm.getProperty('typography', property)
      if (!prop || !('setOptions' in prop)) continue

      const select = prop as PropertySelect
      select.setOptions(
        select.getOptions().map((o) => {
          const id = select.getOptionId(o)
          return { id, label: labels[id] ?? id }
        }),
      )
    }
  })

  // Fired on every model change, including the ones setComponents
  // above triggers, so the flag is armed only after the initial load
  // has settled - otherwise the page opens already claiming edits.
  editor.on('change:changesCount', () => {
    dirty.value = true
  })
  dirty.value = false
}

/**
 * The document as one string.
 *
 * GrapesJS keeps markup and styling apart, so the CSS has to be put
 * back inline before this can be mailed: a linked stylesheet is not
 * fetched by any mail client, and a rule that does not travel with the
 * message may as well not exist.
 */
function inline(html: string, css: string): string {
  if (!css) return html

  const style = `<style>${css}</style>`
  if (html.includes('</head>')) return html.replace('</head>', `${style}</head>`)
  if (html.includes('<body')) return html.replace('<body', `${style}<body`)

  return style + html
}

async function save() {
  const current = localization.value
  if (!editor || !current || saving.value) return

  saving.value = true
  try {
    // The endpoint upserts on (version, language), so the fields this
    // screen does not edit are sent back as they came - omitting them
    // would blank the subject and the text part.
    const res = await templatesApi.putLocalization(templateId, versionId, {
      language: current.language,
      subject: current.subject,
      html: inline(editor.getHtml(), editor.getCss() ?? ''),
      text: current.text ?? '',
    })
    localization.value = res.data.localization
    dirty.value = false
    notify.success('Template saved')
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to save the template'))
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

// Work drawn here exists nowhere else until it is saved, and the two
// ways out of the page are a click and a closed tab. The browser
// prompt is the only thing that can interrupt the second, and it
// cannot be worded by us - hence two guards rather than one.
function onBeforeUnload(e: BeforeUnloadEvent) {
  if (dirty.value) e.preventDefault()
}

onBeforeRouteLeave(async () => {
  if (!dirty.value) return true

  return await confirm({
    title: 'Leave the builder',
    message: 'This template has unsaved changes. Leave and discard them?',
    confirmText: 'Discard',
    variant: 'warning',
  })
})

function openCodeEditor() {
  router.push({
    name: 'template-editor',
    params: { id: templateId, versionId },
    query: { lang: localization.value?.language },
  })
}

function goBack() {
  router.push({ name: 'template-detail', params: { id: templateId } })
}

onMounted(async () => {
  window.addEventListener('keydown', onKeydown)
  window.addEventListener('beforeunload', onBeforeUnload)

  try {
    const [templateRes, localeRes] = await Promise.all([
      templatesApi.get(templateId),
      templatesApi.listLocalizations(templateId, versionId),
    ])

    template.value = templateRes.data.template ?? null
    version.value = (templateRes.data.versions ?? []).find((v) => v.id === versionId) ?? null

    // The requested language, else the template's default, else
    // whatever exists - a version always has at least one.
    const wanted = String(route.query.lang ?? '')
    const locales = localeRes.data.localizations ?? []
    localization.value =
      locales.find((l) => l.language === wanted) ??
      locales.find((l) => l.language === template.value?.default_language) ??
      locales[0] ??
      null
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load the template'))
  } finally {
    loading.value = false
  }

  // After loading and after the canvas has actually rendered: GrapesJS
  // measures its container on init, and a container that is still
  // behind v-if has no size to measure.
  if (ready.value) {
    await new Promise(requestAnimationFrame)
    startEditor(localization.value?.html ?? '')
  }
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeydown)
  window.removeEventListener('beforeunload', onBeforeUnload)
  editor?.destroy()
  editor = null
})
</script>

<template>
  <div class="builder">
    <header class="builder-bar">
      <div class="builder-bar-group">
        <button class="btn btn-secondary btn-sm" @click="goBack">Back</button>
        <h2 class="builder-name">{{ template?.name ?? 'Template' }}</h2>
      </div>

      <div class="builder-bar-group">
        <!-- State reads beside the actions rather than under the
             name: stacked, four badges crowd the title, and Unsaved
             next to Save is where it actually means something. -->
        <div class="builder-tags">
          <span class="badge badge-info">Visual Builder</span>
          <span v-if="localization" class="badge badge-neutral">{{ localization.language }}</span>
          <span v-if="version" class="badge badge-neutral">v{{ version.version }}</span>
          <span v-if="dirty" class="badge badge-warning">Unsaved</span>
        </div>

        <button class="btn btn-secondary btn-sm" @click="openCodeEditor">Code Editor</button>
        <button class="btn btn-primary btn-sm" :disabled="saving || !dirty" @click="save">
          {{ saving ? 'Saving...' : 'Save' }}
        </button>
      </div>
    </header>

    <LoadingBlock v-if="loading" />

    <!-- GrapesJS takes this element over entirely and manages its
         contents itself, so nothing of ours may render inside it. -->
    <div v-else-if="ready" ref="canvas" class="builder-canvas"></div>

    <EmptyState v-else title="Not found">
      <p>This template, version or language no longer exists.</p>
      <button class="btn btn-secondary" @click="goBack">Back to the template</button>
    </EmptyState>
  </div>
</template>

<style scoped>
/* The builder owns the window: its toolbars are fixed rows and the
   canvas takes the rest, so the page itself never scrolls. The
   negative margin escapes the layout's content padding, which would
   otherwise leave a gutter around a full-bleed tool. */
.builder {
  display: flex;
  flex-direction: column;
  height: calc(100dvh - 52px);
  margin: calc(-1 * var(--gutter));
  overflow: hidden;
}

.builder-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-shrink: 0;
  gap: 16px;
  padding: 10px 16px;
  border-bottom: 1px solid var(--border-primary);
  background: var(--bg-primary);
  /* Above the canvas, which GrapesJS gives stacking contexts of its
     own for its panels. */
  z-index: 10;
}

.builder-bar-group {
  display: flex;
  align-items: center;
  gap: 12px;
}

.builder-name {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
  color: var(--text-primary);
  /* See the same rule in TemplateEditor: overflow:hidden makes a flex
     item shrinkable to nothing, so the name has to opt out. */
  flex-shrink: 0;
  max-width: 24ch;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}

.builder-tags {
  display: flex;
  gap: 6px;
  /* A little air before the buttons, so status and actions read as
     two groups rather than one row of pills. */
  margin-right: 4px;
}

.builder-canvas {
  flex: 1;
  min-height: 0;
  overflow: hidden;
  /* The right-hand panel. GrapesJS sizes it as 15% of the canvas,
     which is 180px on a laptop, and the style manager's label and
     input then share a column too narrow for either. */
  --gjs-left-width: clamp(260px, 20%, 320px);
}

/* The way out of preview. GrapesJS mounts a span carrying Font Awesome
   classes for its icon, and the console ships no Font Awesome, so it
   rendered as a bare 10px square at the top left - over the message
   itself, since the top left is where the content sits. A real button
   at the top right, where preview has just emptied the panel. */
.builder-canvas :deep(.gjs-off-prv) {
  position: absolute;
  top: 12px;
  right: 12px;
  z-index: 20;
  width: 36px;
  height: 36px;
  padding: 0;
  border: 1px solid var(--border-primary);
  border-radius: 8px;
  background-color: var(--bg-primary);
  box-shadow: var(--shadow-md);
  color: var(--text-primary);
}

.builder-canvas :deep(.gjs-off-prv:hover) {
  background-color: var(--bg-tertiary);
}

/* A crossed eye, drawn on the console's 18px grid at stroke 1.5. The
   glyph is a mask so it takes currentColor like the toolbar's. */
.builder-canvas :deep(.gjs-off-prv)::before {
  content: '';
  position: absolute;
  inset: 9px;
  background-color: currentColor;
  -webkit-mask: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 18 18' fill='none' stroke='black' stroke-width='1.5' stroke-linecap='round' stroke-linejoin='round'%3E%3Cpath d='M2 2l14 14'/%3E%3Cpath d='M7.2 7.3a2.5 2.5 0 0 0 3.5 3.5'/%3E%3Cpath d='M4.3 4.7C2.7 5.8 1.5 7.4 1.5 9c0 0 2.5 4.5 7.5 4.5 1.3 0 2.4-.3 3.4-.8'/%3E%3Cpath d='M7.4 3.7c.5-.1 1-.2 1.6-.2 5 0 7.5 5.5 7.5 5.5-.4.8-1 1.7-1.8 2.5'/%3E%3C/svg%3E")
    center / contain no-repeat;
  mask: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 18 18' fill='none' stroke='black' stroke-width='1.5' stroke-linecap='round' stroke-linejoin='round'%3E%3Cpath d='M2 2l14 14'/%3E%3Cpath d='M7.2 7.3a2.5 2.5 0 0 0 3.5 3.5'/%3E%3Cpath d='M4.3 4.7C2.7 5.8 1.5 7.4 1.5 9c0 0 2.5 4.5 7.5 4.5 1.3 0 2.4-.3 3.4-.8'/%3E%3Cpath d='M7.4 3.7c.5-.1 1-.2 1.6-.2 5 0 7.5 5.5 7.5 5.5-.4.8-1 1.7-1.8 2.5'/%3E%3C/svg%3E")
    center / contain no-repeat;
}

/* GrapesJS paints its chrome from four theme classes. Mapping them to
   our tokens is what keeps the builder from being a light-mode island
   in a dark console. */
.builder-canvas :deep(.gjs-one-bg) {
  background-color: var(--bg-secondary);
}

.builder-canvas :deep(.gjs-two-color) {
  color: var(--text-primary);
}

.builder-canvas :deep(.gjs-three-bg) {
  background-color: var(--bg-tertiary);
}

.builder-canvas :deep(.gjs-four-color),
.builder-canvas :deep(.gjs-four-color-h:hover) {
  color: var(--accent-fg);
}
</style>
