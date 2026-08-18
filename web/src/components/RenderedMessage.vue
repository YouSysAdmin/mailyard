<script setup lang="ts">
// What the server made of a template: the subject line, and the HTML or
// text part behind a switch.
//
// ONE component because there were three. The editor's preview pane, the
// preview dialog on the template page and the standalone preview route
// each built their own tab strip, their own "no HTML part" sentence and
// their own monospace block - so the same rendered message looked like
// three different features depending on which door you came in by.
//
// It renders and nothing else. WHICH message this is, and when to ask
// for it again, belong to the caller: one previews an unsaved draft,
// one a stored version, and they reach different endpoints.
import { computed, ref, watch } from 'vue'
import type { RenderedPreview } from '../api/templates'
import HtmlPreview from './HtmlPreview.vue'

const props = withDefaults(
  defineProps<{
    /** The render, or null before the first one arrives. */
    preview: RenderedPreview | null
    /** A render is in flight, so an empty part is not yet news. */
    busy?: boolean
    /** Why the last render did not happen. */
    error?: string
    /**
     * Stretch to the height of the container instead of sitting at the
     * height of the content. The editor's pane is a column of a fixed
     * split; a dialog is not.
     */
    fill?: boolean
  }>(),
  { busy: false, error: '', fill: false },
)

const part = ref<'html' | 'text'>('html')

// A message with only a text part opens on it rather than on an empty
// HTML tab saying there is nothing there.
watch(
  () => props.preview,
  (p) => {
    if (p && !p.html && p.text) part.value = 'text'
  },
  { immediate: true },
)

/** What the chosen tab has to show, if anything. */
const missing = computed(() => {
  if (!props.preview) return props.busy ? 'Rendering...' : ''

  const has = part.value === 'html' ? props.preview.html : props.preview.text
  if (has) return ''

  return props.busy ? 'Rendering...' : `This message has no ${part.value} part.`
})
</script>

<template>
  <div class="rendered" :class="{ fill }">
    <div class="strip">
      <div class="tabs">
        <button class="tab" :class="{ active: part === 'html' }" @click="part = 'html'">
          HTML
        </button>
        <button class="tab" :class="{ active: part === 'text' }" @click="part = 'text'">
          Text
        </button>
      </div>
      <slot name="actions" />
    </div>

    <p v-if="error" class="failed">{{ error }}</p>

    <p v-if="preview?.subject" class="subject">
      <span class="subject-label">Subject</span>{{ preview.subject }}
    </p>

    <div class="body">
      <p v-if="missing" class="nothing">{{ missing }}</p>

      <!-- HtmlPreview is the only thing in the console that renders a
           sender's markup: it strips our own tracking pixel, so looking
           at a message does not count as opening it, and holds the
           sender's images back until asked. -->
      <HtmlPreview
        v-else-if="part === 'html' && preview?.html"
        :html="preview.html"
        :fill="fill"
        :min-height="fill ? undefined : '300px'"
        frameless
      />

      <pre v-else-if="preview?.text" class="text">{{ preview.text }}</pre>
    </div>
  </div>
</template>

<style scoped>
.rendered {
  display: flex;
  flex-direction: column;
  overflow: hidden;
  border: 1px solid var(--border-primary);
  border-radius: var(--radius);
}

/* Given the container's height rather than the content's, and its own
   border dropped - the pane it fills already has one. */
.rendered.fill {
  height: 100%;
  border: none;
  border-radius: 0;
}

.subject {
  flex-shrink: 0;
  margin: 0;
  padding: 8px 12px;
  border-bottom: 1px solid var(--border-primary);
  background: var(--bg-tertiary);
  color: var(--text-primary);
  font-size: 13px;
}

.subject-label {
  margin-right: 8px;
  color: var(--text-secondary);
  font-weight: 600;
}

.failed {
  flex-shrink: 0;
  margin: 0;
  padding: 8px 12px;
  border-bottom: 1px solid var(--border-primary);
  background: var(--danger-50);
  color: var(--danger-fg);
  font-size: 13px;
}

.body {
  display: flex;
  flex: 1;
  flex-direction: column;
  min-height: 0;
  overflow: auto;
}

/* Line breaks are content in a text part, and a URL in one has no
   spaces to break at. */
.text {
  margin: 0;
  padding: 16px;
  color: var(--text-secondary);
  font-family: var(--font-mono);
  font-size: 13px;
  line-height: 1.6;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.nothing {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 160px;
  margin: 0;
  padding: 16px;
  color: var(--text-muted);
  font-size: 14px;
}
</style>
