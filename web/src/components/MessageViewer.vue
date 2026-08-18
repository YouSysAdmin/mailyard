<script setup lang="ts">
// The one message-view form: a tab bar (HTML, Text, Headers,
// Attachments, optionally Raw) over a body pane, with the device-width
// switcher on the HTML tab.
//
// A component because it exists twice - the sandbox reader and the
// email log detail - and two hand-written copies of the same tab bar
// is exactly the drift the design-system components were extracted to
// stop. The two callers differ in shape, not in form: the sandbox
// fills a fixed pane and carries a Raw tab (it stores the bytes, the
// log does not), the log is ordinary page content inside a card. fill
// and the raw slot carry that difference so the form itself stays one.
import { computed, ref } from 'vue'
import HtmlPreview from './HtmlPreview.vue'
import { humanSize } from '../composables/humanSize'

type Tab = 'html' | 'text' | 'headers' | 'attachments' | 'raw'

// Device widths for the HTML preview. Narrowing the CONTAINER is a
// real emulation, not a cosmetic one: media queries inside the preview
// frame resolve against the frame's own viewport, so a responsive
// email actually switches to its phone layout at 375px - which is what
// a developer opens either page to check.
type Device = 'phone' | 'tablet' | 'desktop'

const deviceWidths: Record<Device, string> = { phone: '375px', tablet: '768px', desktop: '' }

// One attachment row: whatever the caller knows about the file, plus
// the download URL only it can build.
export interface ViewerAttachment {
  filename?: string
  content_type?: string
  size?: number
  url: string
}

const props = defineProps<{
  html?: string
  text?: string
  /** Frame title, for the reader the preview cannot see. */
  title?: string
  headers?: Record<string, string>
  attachments?: ViewerAttachment[]
  /** Adds a Raw tab, rendered by the caller through the raw slot. */
  raw?: boolean
  /**
   * Fill the parent pane instead of flowing as page content. The
   * sandbox reader hands the viewer a fixed pane and nothing but the
   * body may scroll - without fill the page scrolls and the frame is
   * sized by minHeight, which is what a card on the email detail page
   * wants.
   */
  fill?: boolean
  /** Frame height without fill, passed through to the preview. */
  minHeight?: string

  /** Original destinations behind our click redirects, passed through
   * to the preview - see HtmlPreview.trackedLinks. */
  trackedLinks?: Record<string, string>
}>()

const emit = defineEmits<{ (e: 'tab', tab: Tab): void }>()

// Remembered like the refresh toggle is, because checking the phone
// layout is a mode somebody works in across many messages, not a
// per-message choice. One key across every page carrying this control,
// for the same reason. An unknown stored value falls back to desktop.
const deviceKey = 'mailyard_preview_device'
const storedDevice = localStorage.getItem(deviceKey)
const device = ref<Device>(
  storedDevice === 'phone' || storedDevice === 'tablet' ? storedDevice : 'desktop',
)

function setDevice(d: Device) {
  device.value = d
  localStorage.setItem(deviceKey, d)
}

const headerRows = computed(() => {
  const h = props.headers ?? {}
  return Object.keys(h)
    .sort((a, b) => a.localeCompare(b))
    .map((k) => ({ name: k, value: h[k] }))
})

// Open on whichever body actually exists. A default of HTML on a
// plain-text message shows an empty frame and reads as a bug. A
// message with neither body opens on Raw where there is one - the
// emit tells the caller to start loading it.
const tab = ref<Tab>(props.html ? 'html' : props.text ? 'text' : props.raw ? 'raw' : 'headers')
if (tab.value === 'raw') emit('tab', 'raw')

function selectTab(next: Tab) {
  tab.value = next
  emit('tab', next)
}
</script>

<template>
  <div :class="['viewer', { 'viewer--fill': fill }]">
    <div class="viewer-tabbar">
      <div class="tabs">
        <button :class="['tab', { active: tab === 'html' }]" @click="selectTab('html')">
          HTML
        </button>
        <button :class="['tab', { active: tab === 'text' }]" @click="selectTab('text')">
          Text
        </button>
        <button :class="['tab', { active: tab === 'headers' }]" @click="selectTab('headers')">
          Headers <span class="tab-count">{{ headerRows.length }}</span>
        </button>
        <button
          :class="['tab', { active: tab === 'attachments' }]"
          @click="selectTab('attachments')"
        >
          Attachments <span class="tab-count">{{ attachments?.length ?? 0 }}</span>
        </button>
        <button v-if="raw" :class="['tab', { active: tab === 'raw' }]" @click="selectTab('raw')">
          Raw
        </button>
      </div>

      <!-- Only where it does something: the other tabs are text and
           tables, and a width control over them reads as broken. -->
      <div
        v-if="tab === 'html' && html"
        class="device-switch"
        role="group"
        aria-label="Preview width"
      >
        <button
          type="button"
          :class="['device-btn', { active: device === 'phone' }]"
          title="Phone width (375px)"
          aria-label="Phone width"
          @click="setDevice('phone')"
        >
          <svg
            width="18"
            height="18"
            viewBox="0 0 18 18"
            fill="none"
            stroke="currentColor"
            stroke-width="1.5"
            stroke-linecap="round"
            stroke-linejoin="round"
          >
            <rect x="5.5" y="2" width="7" height="14" rx="1.8" />
            <path d="M9 13.4h.01" />
          </svg>
        </button>
        <button
          type="button"
          :class="['device-btn', { active: device === 'tablet' }]"
          title="Tablet width (768px)"
          aria-label="Tablet width"
          @click="setDevice('tablet')"
        >
          <svg
            width="18"
            height="18"
            viewBox="0 0 18 18"
            fill="none"
            stroke="currentColor"
            stroke-width="1.5"
            stroke-linecap="round"
            stroke-linejoin="round"
          >
            <rect x="3.5" y="2" width="11" height="14" rx="1.8" />
            <path d="M8 13.4h2" />
          </svg>
        </button>
        <button
          type="button"
          :class="['device-btn', { active: device === 'desktop' }]"
          title="Full width"
          aria-label="Full width"
          @click="setDevice('desktop')"
        >
          <svg
            width="18"
            height="18"
            viewBox="0 0 18 18"
            fill="none"
            stroke="currentColor"
            stroke-width="1.5"
            stroke-linecap="round"
            stroke-linejoin="round"
          >
            <rect x="2" y="3.5" width="14" height="9.5" rx="1.5" />
            <path d="M9 13v2.5" />
            <path d="M6.5 15.5h5" />
          </svg>
        </button>
      </div>
    </div>

    <!-- On the HTML tab the frame scrolls itself, so the pane must
         not. Every other tab is ordinary content and scrolls here. -->
    <div :class="['viewer-body', tab === 'html' ? 'viewer-body--frame' : 'viewer-body--scroll']">
      <template v-if="tab === 'html'">
        <div
          v-if="html"
          :class="['device-stage', { 'device-stage--framed': device !== 'desktop' }]"
        >
          <div
            class="device-viewport"
            :style="device === 'desktop' ? undefined : { width: deviceWidths[device] }"
          >
            <HtmlPreview
              :html="html"
              :title="title || 'Message preview'"
              :fill="fill"
              :frameless="fill || device !== 'desktop'"
              :min-height="minHeight"
              :tracked-links="trackedLinks"
            />
          </div>
        </div>
        <p v-else class="viewer-muted">This message has no HTML part.</p>
      </template>

      <template v-else-if="tab === 'text'">
        <pre v-if="text" class="viewer-pre">{{ text }}</pre>
        <p v-else class="viewer-muted">This message has no plain text part.</p>
      </template>

      <template v-else-if="tab === 'headers'">
        <div v-if="headerRows.length" class="table-wrapper">
          <table>
            <thead>
              <tr>
                <th class="col-header-name">Header</th>
                <th>Value</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="h in headerRows" :key="h.name">
                <td>
                  <code>{{ h.name }}</code>
                </td>
                <td class="header-value">{{ h.value }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <p v-else class="viewer-muted">No headers are stored for this message.</p>
      </template>

      <template v-else-if="tab === 'attachments'">
        <div v-if="attachments?.length" class="table-wrapper">
          <table>
            <thead>
              <tr>
                <th>Filename</th>
                <th>Type</th>
                <th>Size</th>
                <th class="text-right"></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(a, i) in attachments" :key="i">
                <td>{{ a.filename || '(unnamed)' }}</td>
                <td>
                  <code>{{ a.content_type || 'application/octet-stream' }}</code>
                </td>
                <td>{{ a.size != null ? humanSize(a.size) : '-' }}</td>
                <td class="text-right">
                  <a class="btn btn-secondary btn-sm" :href="a.url" download> Download </a>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <p v-else class="viewer-muted">This message has no attachments.</p>
      </template>

      <template v-else>
        <slot name="raw"></slot>
      </template>
    </div>
  </div>
</template>

<style scoped>
/* Block mode is the base: the viewer flows like any card content and
   the page scrolls. fill turns it into the sandbox pane, where the
   viewer takes the height it is given and only the body scrolls. */
/* No min-height: 0 here on purpose. The body's 12rem floor below is
   this root's min-content, and in the sandbox pane that min-content is
   the guarantee that the envelope region shrinks before the body does. */
.viewer--fill {
  display: flex;
  flex-direction: column;
}

/* The bar around the console's own segmented control. .tabs carries a
   24px bottom margin for the pages that stack content under it, which
   here would be a gap between the control and the body it switches. */
.viewer-tabbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 12px 16px;
  border-bottom: 1px solid var(--border-primary);
  /* Scrolls sideways rather than wrapping: five tabs in a narrow pane
     would otherwise take two rows and push the body down. */
  overflow-x: auto;
}

.viewer--fill .viewer-tabbar {
  padding: 12px var(--gutter);
  border-top: 1px solid var(--border-primary);
}

.viewer-tabbar .tabs {
  margin-bottom: 0;
  /* max-content, NOT the stylesheet's fit-content. Inside a scrolling
     box fit-content resolves against the PARENT's width, so the pill
     container comes out narrower than the tabs in it and the last one
     renders outside its own border and rounding. max-content sizes to
     the tabs, and the bar scrolls the whole control. */
  width: max-content;
}

.tab-count {
  margin-left: 4px;
  padding: 0 5px;
  border-radius: 8px;
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  font-size: 11px;
}

/* The width switcher. Icon buttons rather than labeled ones, because
   the tab bar already spends its words - and the three glyphs are the
   convention every mail tool uses for this control. */
.device-switch {
  display: flex;
  flex: none;
  gap: 4px;
}

.device-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 30px;
  padding: 0;
  border: none;
  border-radius: var(--radius);
  background: none;
  color: var(--text-tertiary);
  cursor: pointer;
}

.device-btn:hover {
  color: var(--text-primary);
  background: var(--bg-secondary);
}

.device-btn.active {
  color: var(--primary-600);
  background: var(--primary-50);
}

.viewer-body {
  padding: 16px;
}

.viewer--fill .viewer-body {
  /* basis ZERO, not auto. With auto the basis is the CONTENT height,
     so a raw message of a few hundred lines claims all of it and
     squeezes whatever the caller renders above out of existence. Zero
     makes this "share what is left", which is what flex:1 is meant to
     say. The floor keeps the body readable under a tall envelope. */
  flex: 1 1 0;
  min-height: 12rem;
}

.viewer--fill .viewer-body--frame {
  display: flex;
  flex-direction: column;
  padding: 0;
  overflow: hidden;
}

.viewer--fill .viewer-body--scroll {
  padding: var(--gutter);
  overflow: auto;
}

/* The stage the emulated viewport stands on. At desktop width it is a
   pass-through column, so the full-width rendering is exactly what it
   was before the switcher existed. */
.device-stage {
  display: flex;
  flex-direction: column;
}

.viewer--fill .device-stage {
  flex: 1;
  min-height: 0;
}

.device-stage--framed {
  align-items: center;
  padding: 12px;
  border-radius: var(--radius);
  background: var(--bg-secondary);
}

/* The emulated viewport. Its width is what the message's own media
   queries resolve against, so 375px here is the phone breakpoint for
   real. max-width keeps a narrow pane honest - a 375px frame in a
   320px column clips instead of scrolling the page. */
.device-viewport {
  display: flex;
  flex-direction: column;
  width: 100%;
  max-width: 100%;
  min-height: 0;
}

.viewer--fill .device-viewport {
  flex: 1;
}

.device-stage--framed .device-viewport {
  border: 1px solid var(--border-primary);
  border-radius: var(--radius);
  overflow: hidden;
  background: #fff;
}

.viewer-pre {
  margin: 0;
  font-family: var(--font-mono);
  font-size: 12px;
  line-height: 1.6;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.viewer-muted {
  margin: 0;
  color: var(--text-secondary);
}

.col-header-name {
  width: 220px;
}

.header-value {
  font-family: var(--font-mono);
  font-size: 12px;
  overflow-wrap: anywhere;
}
</style>
