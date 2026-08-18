<script setup lang="ts">
// Files that ride along with every email this template sends.
//
// Its own component because it is self-contained: attachments hang off
// the TEMPLATE rather than off a version or a language, so nothing
// here depends on which of those is selected, and the detail page was
// carrying six unrelated concerns in one file.
import { ref } from 'vue'
import { templatesApi, type TemplateAttachment } from '../../api/templates'
import { apiErrorMessage } from '../../api/client'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import { useConfirm } from '../../composables/useConfirm'
import { formatDate } from '../../composables/formatDate'
import { humanSize } from '../../composables/humanSize'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'

const props = defineProps<{ templateId: string }>()

const notify = useNotificationStore()
const projects = useProjectStore()
const { confirm } = useConfirm()

const files = ref<TemplateAttachment[]>([])
const loading = ref(true)
const uploading = ref(false)
const picker = ref<HTMLInputElement | null>(null)

async function load() {
  loading.value = true
  try {
    files.value = (await templatesApi.listAttachments(props.templateId)).data.attachments ?? []
  } catch (e) {
    files.value = []
    notify.error(apiErrorMessage(e, 'Failed to load the attachments'))
  } finally {
    loading.value = false
  }
}

/**
 * The file's bytes as base64, which is what the endpoint takes.
 *
 * Through a data URL because that is the only reader API that yields
 * base64 directly - the prefix it adds is cut back off. The alternative
 * is reading an ArrayBuffer and encoding by hand, which is more code
 * for the same answer.
 */
function asBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => {
      const url = String(reader.result ?? '')
      const comma = url.indexOf(',')
      resolve(comma >= 0 ? url.slice(comma + 1) : url)
    }
    reader.onerror = () => reject(reader.error)
    reader.readAsDataURL(file)
  })
}

async function onFileChosen(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return

  uploading.value = true
  try {
    const res = await templatesApi.uploadAttachment(props.templateId, {
      filename: file.name,
      // An empty type is what the browser reports for an extension it
      // does not know. Send nothing rather than an empty string and
      // let the server decide.
      content_type: file.type || undefined,
      content: await asBase64(file),
    })
    files.value = [...files.value, res.data.attachment]
    notify.success(`"${file.name}" attached`)
  } catch (e) {
    // Said plainly rather than placed on a field. The server refuses
    // `filename` and `content`, and neither is an input a person can see
    // here - the control is a button over a hidden file picker, so a
    // captured message would land nowhere and the upload would look like
    // it had simply done nothing.
    notify.error(apiErrorMessage(e, 'Failed to attach the file'))
  } finally {
    uploading.value = false
    // Cleared so choosing the SAME file again still fires a change.
    input.value = ''
  }
}

function download(a: TemplateAttachment) {
  window.location.href = templatesApi.attachmentDownloadURL(props.templateId, a.id)
}

async function remove(a: TemplateAttachment) {
  const confirmed = await confirm({
    title: 'Delete attachment',
    message: `Delete "${a.filename}"? It will stop being attached to emails sent with this template.`,
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return

  try {
    await templatesApi.deleteAttachment(props.templateId, a.id)
    files.value = files.value.filter((f) => f.id !== a.id)
    notify.success('Attachment deleted')
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to delete the attachment'))
  }
}

void load()
</script>

<template>
  <div class="card">
    <div class="card-header">
      <h2>Attachments</h2>

      <div v-if="projects.can('templates:write')" class="flex gap-2 align-center">
        <!-- The real input is hidden and driven by the button: a
             styled file input is not possible, and the browser's own
             control says "No file chosen" beside a page that has a
             list of them. -->
        <input ref="picker" class="hidden" type="file" @change="onFileChosen" />
        <button class="btn btn-primary btn-sm" :disabled="uploading" @click="picker?.click()">
          {{ uploading ? 'Uploading...' : 'Add file' }}
        </button>
      </div>
    </div>

    <LoadingBlock v-if="loading" />

    <EmptyState
      v-else-if="files.length === 0"
      text="No attachments. A file added here is sent with every email this template produces."
    />

    <div v-else class="table-wrapper">
      <table>
        <thead>
          <tr>
            <th>Filename</th>
            <th>Content Type</th>
            <th>Size</th>
            <th>Added</th>
            <th class="text-right"></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="file in files" :key="file.id">
            <td>
              <strong>{{ file.filename }}</strong>
            </td>
            <td>
              <span v-if="file.content_type" class="badge badge-neutral">
                {{ file.content_type }}
              </span>
              <span v-else class="text-muted">-</span>
            </td>
            <td>{{ humanSize(file.size) }}</td>
            <td>{{ formatDate(file.created_at) }}</td>
            <td class="text-right">
              <div class="flex gap-2">
                <button class="btn btn-secondary btn-sm" @click="download(file)">Download</button>
                <button
                  v-if="projects.can('templates:delete')"
                  class="btn btn-danger btn-sm"
                  @click="remove(file)"
                >
                  Delete
                </button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>

      <p class="attachments-note">Every email sent with this template carries these files.</p>
    </div>
  </div>
</template>

<style scoped>
/* The header puts a button beside a heading, so its row centers.
   Local rather than global: it came with the markup out of the detail
   page, where the parent's scoped rule could not follow it. */
.align-center {
  align-items: center;
}

.attachments-note {
  margin: 0;
  padding: 12px 16px;
  border-top: 1px solid var(--border-primary);
  color: var(--text-secondary);
  font-size: 13px;
}
</style>
