// One CodeMirror editor, wrapped so a view can declare four of them
// without four copies of the same twenty lines.
//
// What it owns is the LIFECYCLE - the element to mount into, the view
// itself, the current text, and tearing it down. CodeMirror keeps its
// document in its own state rather than in a Vue ref, so the two have
// to be reconciled somewhere; doing it once here is the reason this
// exists.
import { onBeforeUnmount, ref, shallowRef } from 'vue'
import { EditorState } from '@codemirror/state'
import { EditorView, placeholder as showPlaceholder } from '@codemirror/view'
import { html } from '@codemirror/lang-html'
import { json } from '@codemirror/lang-json'
import { oneDark } from '@codemirror/theme-one-dark'
import { basicSetup } from 'codemirror'

/** Syntax highlighting to load. Plain text gets none. */
export type CodeLanguage = 'html' | 'json' | 'text'

export interface CodeMirrorOptions {
  language?: CodeLanguage
  placeholder?: string

  /**
   * Called when the PERSON typed, never when the document was
   * replaced through `set`. That distinction is the whole reason this
   * is a callback rather than a watcher on the text: loading a
   * different language into the editor changes the document, and a
   * caller tracking unsaved work must not read that as an edit.
   */
  onEdit?: () => void
}

// The console is light or dark by preference, but code is always on
// dark here: syntax colours are designed against it, and an editor
// that restyles itself with the rest of the page makes the same
// template look like two different documents.
const surface = EditorView.theme({
  '&': { fontSize: '13px', height: '100%' },
  '.cm-scroller': {
    // A CodeMirror theme is a JS object handed to the library, not a
    // stylesheet, so it cannot resolve var(--font-mono) and the stack
    // has to be repeated here.
    fontFamily: '"Geist Mono Variable", ui-monospace, SFMono-Regular, Menlo, monospace',
  },
  '.cm-content': { minHeight: '100px' },
})

/**
 * useCodeMirror returns the element ref to bind, the live text, and
 * the three verbs a caller needs.
 *
 * `mount` is separate from creation because the element does not exist
 * until the view has rendered - a caller mounts after its data has
 * loaded and the container is really in the document.
 */
export function useCodeMirror(options: CodeMirrorOptions = {}) {
  const host = ref<HTMLElement | null>(null)
  const view = shallowRef<EditorView | null>(null)
  const text = ref('')

  // Raised while `set` is dispatching, so the update listener can tell
  // our own write apart from a keystroke.
  let replacing = false

  function mount(initial = '') {
    if (!host.value || view.value) return

    text.value = initial
    const extensions = [
      basicSetup,
      EditorView.lineWrapping,
      EditorView.updateListener.of((update) => {
        if (!update.docChanged) return

        text.value = update.state.doc.toString()
        if (!replacing) options.onEdit?.()
      }),
      oneDark,
      surface,
    ]

    if (options.language === 'html') extensions.push(html())
    if (options.language === 'json') extensions.push(json())
    if (options.placeholder) extensions.push(showPlaceholder(options.placeholder))

    view.value = new EditorView({
      state: EditorState.create({ doc: initial, extensions }),
      parent: host.value,
    })
  }

  /** Replace the whole document without it counting as an edit. */
  function set(next: string) {
    const editor = view.value
    if (!editor) {
      text.value = next

      return
    }

    replacing = true
    editor.dispatch({ changes: { from: 0, to: editor.state.doc.length, insert: next } })
    replacing = false
  }

  function destroy() {
    view.value?.destroy()
    view.value = null
  }

  // A CodeMirror view left behind keeps DOM listeners and its own
  // measurement observers alive, so tearing down is not optional.
  onBeforeUnmount(destroy)

  return { host, text, mount, set, destroy }
}
