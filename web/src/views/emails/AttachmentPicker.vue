<script setup lang="ts">
// Files to send with a message, chosen and held in the browser.
//
// THERE IS NO STAGING UPLOAD. Each file is read to base64 and rides
// along in the JSON body, so abandoning the form leaves nothing behind
// on the server - and the caps below are the only thing standing
// between a person and a request the send would refuse anyway.
//
// The caps come FROM THE SERVER. Refusing a file here is about answering
// sooner, not about being the authority: the send checks the same limits
// and its answer is the one that counts. The defaults are conservative
// and apply only until the real values arrive.
import { computed, ref } from 'vue'
import type { SendLimits } from '../../api/emails'
import type { EmailAttachment } from '../../api/types'
import { useNotificationStore } from '../../stores/notification'
import FormField from '../../components/FormField.vue'

/** What the form holds: the wire shape plus the decoded byte length. */
export interface PendingAttachment extends EmailAttachment {
  filename: string
  content: string
  // Kept so the running total does not re-measure base64 every render.
  size: number
}

const props = defineProps<{
  limits: SendLimits
  /** False for a reader who may not send, which greys the controls. */
  canSend: boolean
}>()

const files = defineModel<PendingAttachment[]>({ required: true })

const notify = useNotificationStore()

const input = ref<HTMLInputElement | null>(null)
const reading = ref(false)

const attachedBytes = computed(() => files.value.reduce((sum, a) => sum + a.size, 0))

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`

  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

function pick() {
  input.value?.click()
}

function readAsBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => {
      const result = String(reader.result || '')
      // Strip the data URL prefix to get raw base64.
      const idx = result.indexOf(',')
      resolve(idx >= 0 ? result.slice(idx + 1) : result)
    }
    reader.onerror = () => reject(reader.error)
    reader.readAsDataURL(file)
  })
}

// Each refusal names the FILE and the limit it broke, and the loop goes
// on to the next one: picking five files where one is too big should
// attach the other four rather than failing the whole choice.
//
// ACCUMULATED LOCALLY AND ASSIGNED ONCE. `files` is a defineModel, so
// writing to it emits and the value only comes back on the next tick -
// assigning inside the loop meant the second file read a stale list and
// overwrote the first. Choosing two files attached one.
async function onChosen(e: Event) {
  const el = e.target as HTMLInputElement
  const chosen = Array.from(el.files ?? [])
  el.value = ''
  if (chosen.length === 0) return

  reading.value = true
  const added: PendingAttachment[] = []
  let bytes = attachedBytes.value
  try {
    for (const file of chosen) {
      if (files.value.length + added.length >= props.limits.max_attachments) {
        notify.error(`At most ${props.limits.max_attachments} attachments per email`)

        break
      }

      if (file.size > props.limits.max_attachment_size) {
        notify.error(
          `"${file.name}" is ${formatBytes(file.size)}, over the ${formatBytes(props.limits.max_attachment_size)} per-file limit`,
        )

        continue
      }

      if (bytes + file.size > props.limits.max_total_attachment_size) {
        notify.error(
          `Adding "${file.name}" would exceed the ${formatBytes(props.limits.max_total_attachment_size)} total attachment limit`,
        )

        continue
      }

      try {
        added.push({
          filename: file.name,
          content_type: file.type || undefined,
          content: await readAsBase64(file),
          size: file.size,
        })
        bytes += file.size
      } catch {
        notify.error(`Could not read "${file.name}"`)
      }
    }

    if (added.length) files.value = [...files.value, ...added]
  } finally {
    reading.value = false
  }
}

function remove(idx: number) {
  files.value = files.value.filter((_, i) => i !== idx)
}
</script>

<template>
  <FormField label="Attachments">
    <input class="hidden" ref="input" type="file" multiple @change="onChosen" />

    <div v-if="files.length" class="table-wrapper mb-3">
      <table>
        <thead>
          <tr>
            <th>File</th>
            <th>Type</th>
            <th>Size</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(a, i) in files" :key="i">
            <td>{{ a.filename }}</td>
            <!-- What a receiver assumes when the browser could not tell
                 us, which is what the server will store. -->
            <td>{{ a.content_type || 'application/octet-stream' }}</td>
            <td>{{ formatBytes(a.size) }}</td>
            <td class="text-right">
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                :disabled="!canSend"
                @click="remove(i)"
              >
                Remove
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <button
      type="button"
      class="btn btn-secondary btn-sm"
      :disabled="reading || !canSend || files.length >= limits.max_attachments"
      @click="pick"
    >
      {{ reading ? 'Reading...' : 'Add files' }}
    </button>

    <template #hint
      >Up to {{ limits.max_attachments }} files, {{ formatBytes(limits.max_attachment_size) }} each
      and {{ formatBytes(limits.max_total_attachment_size) }} in total.
      <span v-if="files.length">
        Currently {{ files.length }} attached, {{ formatBytes(attachedBytes) }}.
      </span></template
    >
  </FormField>
</template>
