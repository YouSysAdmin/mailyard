<script setup lang="ts">
// The one way this console renders an email body.
//
// A component rather than a helper each view calls, because what it
// prevents happens silently. Drop a stored body into an <iframe srcdoc>
// and the browser fetches our own tracking pixel, so the operator who
// opened the page to check whether a message was read ends up marking it
// read. A view that reaches for this component cannot make that mistake.
// One that hand-writes an iframe can, which is what
// TestEveryHTMLPreviewGoesThroughTheComponent is for.
//
// The frame stays sandbox="" with no allow-same-origin, so the markup
// inside it - written by whoever sent the message, not by us - cannot
// reach the session. That is also why the tracking fix cannot live on
// the server: an opaque origin makes the pixel request cross-site, so no
// cookie arrives to recognise it by, and the request looks exactly like
// a real webmail fetching the same image.
import { computed, ref } from 'vue'

const props = defineProps<{
  html: string
  title?: string
  /**
   * Frame height. The callers want anywhere from 300px to 420px, which
   * would otherwise be seven near-identical scoped rules.
   *
   * An inline style and not a class name, because the notice bar above
   * the frame means the iframe is no longer this component's ROOT
   * element - and a parent's scoped styles reach only the root. Passing a
   * class silently stopped working the moment the wrapper appeared, which
   * cost five previews their sizing before it was caught.
   */
  minHeight?: string
  /** Fill the parent instead of sizing to content - the editor pane. */
  fill?: boolean
  /** No border or radius, for a preview already inside a framed panel. */
  frameless?: boolean

  /**
   * Original destinations behind our click redirects, keyed by link
   * hash - the emails API answers this from tracked_links. With it a
   * stripped link gets its REAL href back, so the preview shows where
   * the link goes and a click leads straight there, counting nothing.
   * Without it the href is simply removed.
   */
  trackedLinks?: Record<string, string>
}>()

// Our own tracking markup, removed before the browser can act on it.
//
// Unconditional, and separate from the image question below. Our pixel is
// on our origin, which img-src has always permitted and still does, so
// nothing about showing or hiding the sender's images changes the fact
// that rendering this markup would count as a read of the project's own
// message.
//
// A regex over HTML is fine here and would not be in general: these two
// shapes are emitted by our own builder, never parsed from a stranger.
function withoutOurTracking(html: string): string {
  // A wrapped link. The redirect href goes either way: clicking it in
  // a preview would follow the redirect and count a click by whoever
  // is LOOKING. What replaces it depends on trackedLinks - the path
  // carries only a hash of the destination, so the caller has to ask
  // the server for the mapping. With it the anchor gets its REAL
  // destination back. Without it the href is removed and the anchor
  // is left as text, which is why the email detail page fetches the
  // mapping rather than living with that.
  const restore = (whole: string, url: string) => {
    const m = /\/tracking\/click\/[^/"'?]+\/([0-9a-f]{16})/.exec(url)
    const original = m ? props.trackedLinks?.[m[1]] : undefined
    if (!original) return 'data-tracked-link="1" title="Tracked link"'

    // Quotes HTML-escaped, not JSON-escaped: a backslash means
    // nothing inside an HTML attribute.
    return `href="${original.replace(/"/g, '&quot;')}" data-tracked-link="1"`
  }

  return (
    html
      // The open pixel: an img whose src is /tracking/open/<id>.gif.
      .replace(/<img\b[^>]*\/tracking\/open\/[^>]*>/gi, '')
      .replace(/href\s*=\s*"([^"]*\/tracking\/click\/[^"]*)"/gi, restore)
      .replace(/href\s*=\s*'([^']*\/tracking\/click\/[^']*)'/gi, restore)
  )
}

// Remote images, held back until the reader asks for them.
//
// Fetching a remote image tells whoever hosts it that this message was
// opened, when, and roughly from where. For mail a stranger sent us that
// is a read receipt nobody agreed to send, which is why every mail
// client defaults it off too.
//
// The CSP cannot do this for us. The console's policy is `img-src * data:
// blob:` and has to be, since an email's images are the email and a
// preview that renders none of them is not a preview. A srcdoc document
// inherits that policy and cannot narrow it per frame - give the frame
// its own `<meta http-equiv="Content-Security-Policy">` and the parent
// directive still decides.
//
// So we do it here instead. Every remote reference becomes a data-
// attribute, which no browser fetches. Nothing is destroyed and nothing
// needs undoing: the rendered body is recomputed from props.html, so
// showing images is just not running this pass.
//
// data: and cid: are left alone. A data URI is bytes already in the
// message and fetches nothing, and emails embed logos that way
// constantly, so blocking them would break the common case to prevent a
// request that never happens. cid: does not resolve here at all.
function withoutRemoteImages(html: string): string {
  const remote = (value: string) => !/^\s*(data:|cid:)/i.test(value)

  return (
    html
      // src on any element: img, and the rarer input type=image and
      // video poster shapes a mail builder can emit.
      .replace(/\ssrc\s*=\s*("([^"]*)"|'([^']*)')/gi, (whole, _q, dq, sq) => {
        const value = dq ?? sq ?? ''

        return remote(value) ? ` data-blocked-src=${JSON.stringify(value)}` : whole
      })
      // srcset is used in PREFERENCE to src, so blocking one without the
      // other blocks nothing at all.
      .replace(/\ssrcset\s*=\s*("([^"]*)"|'([^']*)')/gi, (whole, _q, dq, sq) => {
        const value = dq ?? sq ?? ''

        return remote(value) ? ` data-blocked-srcset=${JSON.stringify(value)}` : whole
      })
      // CSS url() in a style attribute or a <style> block. Background
      // images are how a great deal of real email art-directs itself, and
      // they fetch exactly as an <img> does.
      .replace(/url\(\s*(['"]?)([^)'"]+)\1\s*\)/gi, (whole, _q, value) =>
        remote(value) ? 'none /* blocked */' : whole,
      )
  )
}

// off by default, per frame, and not remembered. Deciding to load one
// sender's images is not a standing decision about every other sender.
const showImages = ref(false)

const stripped = computed(() => withoutOurTracking(props.html ?? ''))
const rendered = computed(() =>
  showImages.value ? stripped.value : withoutRemoteImages(stripped.value),
)

// Whether there is anything to offer. A body with no remote reference
// gets no notice bar: offering to load images that do not exist reads as
// a broken feature.
const hasRemote = computed(
  () => showImages.value || withoutRemoteImages(stripped.value) !== stripped.value,
)

// With fill the frame is a flex child that takes what is left, and the
// minimum goes to ZERO. A flex item will not shrink below its content
// otherwise, and a 400px floor inside a 300px pane pushes the frame out
// of it - which is the parent scrolling, the thing fill exists to
// avoid. Without fill the minimum is the whole sizing story.
const frameStyle = computed(() =>
  props.fill ? { flex: '1', minHeight: '0' } : { minHeight: props.minHeight ?? '400px' },
)
</script>

<template>
  <div :class="fill ? 'html-preview filling' : 'html-preview'">
    <div v-if="hasRemote" class="preview-notice">
      <template v-if="!showImages">
        <span>Images from external sources are not displayed.</span>
        <button type="button" class="btn btn-sm" @click="showImages = true">Load images</button>
      </template>
      <template v-else>
        <span>Images from external sources are displayed.</span>
        <button type="button" class="btn btn-sm" @click="showImages = false">Hide images</button>
      </template>
    </div>

    <iframe
      :class="frameless ? 'html-preview-frame frameless' : 'html-preview-frame'"
      :style="frameStyle"
      :srcdoc="rendered"
      sandbox=""
      :title="title ?? 'HTML preview'"
    ></iframe>
  </div>
</template>

<style scoped>
.html-preview {
  /* A plain block wrapper, so the notice can sit above the frame. It
     needs no sizing of its own - the frame is width:100% either way. */
  display: block;
}

/* Filling means the frame is the only thing that scrolls.
   height:100% rather than a flex:1 of its own, because the parent that
   asked for fill is what decides the height - and min-height:0 so this
   column can be shorter than the email inside it. */
.filling {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
}

.html-preview-frame {
  width: 100%;
  border: 1px solid var(--border-primary);
  border-radius: var(--radius);
  /* White, not the theme background: an email body assumes a white page
     and a dark console would render dark text on dark. */
  background: #fff;
}

.frameless {
  border: none;
  border-radius: 0;
}

.preview-notice {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  padding: 0.5rem 0.75rem;
  margin-bottom: 0.5rem;
  border: 1px solid var(--border-primary);
  border-radius: var(--radius);
  background: var(--bg-secondary);
  color: var(--text-secondary);
  font-size: 0.875rem;
}
</style>
