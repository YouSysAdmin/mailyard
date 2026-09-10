<script setup lang="ts">
// Inboxes: named sender filters over the one capture list.
//
// A DIALOG, like the connection details, and for the same reason: an
// inbox is set up once, when a second application starts sending into
// the sandbox, and the page behind it is what people keep open.
//
// An inbox holds no mail. It is a saved filter over the envelope
// sender, decided when the list is read, so editing the addresses
// changes what it shows - old captures included - and deleting it
// deletes nothing. The copy in here says so, because "delete inbox"
// reads like it would empty one.
import { ref } from 'vue'
import { sandboxApi, type SandboxInbox } from '../../api/sandbox'
import { apiErrorMessage } from '../../api/client'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import { useConfirm } from '../../composables/useConfirm'
import { useFieldErrors } from '../../composables/fieldErrors'
import BaseModal from '../../components/BaseModal.vue'
import FormField from '../../components/FormField.vue'

defineProps<{
  inboxes: SandboxInbox[]
}>()

const emit = defineEmits<{
  /** An inbox was created, edited or deleted - the list needs rereading. */
  (e: 'changed'): void
  (e: 'close'): void
}>()

const notify = useNotificationStore()
const projStore = useProjectStore()
const { confirm } = useConfirm()
const { errors: fieldErrors, capture, clear } = useFieldErrors()

const showForm = ref(false)
const editing = ref<SandboxInbox | null>(null)
const saving = ref(false)
const name = ref('')
const description = ref('')
// One address per line. Split, trimmed and emptied of blanks on save -
// the server lowercases and dedupes what is left.
const addressLines = ref('')

function openCreate() {
  editing.value = null
  name.value = ''
  description.value = ''
  addressLines.value = ''
  clear()
  showForm.value = true
}

function openEdit(box: SandboxInbox) {
  editing.value = box
  name.value = box.name
  description.value = box.description ?? ''
  addressLines.value = box.addresses.join('\n')
  clear()
  showForm.value = true
}

function addresses(): string[] {
  return addressLines.value
    .split(/[\n,;]+/)
    .map((a) => a.trim())
    .filter((a) => a !== '')
}

async function save() {
  clear()
  const payload = {
    name: name.value.trim(),
    description: description.value.trim(),
    addresses: addresses(),
  }
  if (!payload.name || payload.addresses.length === 0) return
  saving.value = true
  try {
    if (editing.value) {
      await sandboxApi.updateInbox(editing.value.id, payload)
      notify.success('Inbox updated')
    } else {
      await sandboxApi.createInbox(payload)
      notify.success('Inbox created')
    }
    showForm.value = false
    emit('changed')
  } catch (e) {
    if (!capture(e)) notify.error(apiErrorMessage(e, 'Failed to save the inbox'))
  } finally {
    saving.value = false
  }
}

async function remove(box: SandboxInbox) {
  const ok = await confirm({
    title: 'Delete inbox',
    message: `Delete "${box.name}"? It is a filter, not a container - every capture it showed stays in the sandbox.`,
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!ok) return
  try {
    await sandboxApi.removeInbox(box.id)
    notify.success('Inbox deleted')
    emit('changed')
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to delete the inbox'))
  }
}
</script>

<template>
  <div>
    <BaseModal title="Inboxes" size="modal-w760" @close="emit('close')">
      <p class="lead">
        An inbox is a saved filter.
        Name the addresses one application sends from and the sandbox can filter that application's
        mail on its own.
      </p>

      <div class="inboxes-head">
        <h2>Inboxes</h2>
        <button
          v-if="projStore.can('sandbox:write')"
          class="btn btn-primary btn-sm"
          @click="openCreate"
        >
          New inbox
        </button>
      </div>

      <p v-if="inboxes.length === 0" class="muted">
        None yet. The sandbox shows every capture until an inbox narrows it.
      </p>
      <div v-else class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th>Name</th>
              <th>Senders</th>
              <th class="text-right"></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="box in inboxes" :key="box.id">
              <td class="cell-title">
                {{ box.name }}
                <p v-if="box.description" class="row-desc">{{ box.description }}</p>
              </td>
              <td>
                <span v-for="a in box.addresses" :key="a" class="sender-chip">
                  <code>{{ a }}</code>
                </span>
              </td>
              <td>
                <div class="table-actions">
                  <button
                    v-if="projStore.can('sandbox:write')"
                    class="btn btn-secondary btn-sm"
                    @click="openEdit(box)"
                  >
                    Edit
                  </button>
                  <button
                    v-if="projStore.can('sandbox:delete')"
                    class="btn btn-danger btn-sm"
                    @click="remove(box)"
                  >
                    Delete
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <template #footer>
        <button class="btn btn-primary" @click="emit('close')">Done</button>
      </template>
    </BaseModal>

    <BaseModal
      v-if="showForm"
      :title="editing ? 'Edit inbox' : 'New inbox'"
      form
      @submit="save"
      @close="showForm = false"
    >
      <FormField label="Name" :error="fieldErrors.name" hint="Unique in this project.">
        <input
          v-model="name"
          type="text"
          class="form-input"
          placeholder="checkout service"
          required
        />
      </FormField>
      <FormField :error="fieldErrors.description">
        <template #label>Description <span class="text-muted">(optional)</span></template>
        <input v-model="description" type="text" class="form-input" />
      </FormField>
      <FormField
        label="Sender addresses"
        :error="fieldErrors.addresses"
        hint="One per line. A capture belongs to this inbox when its envelope sender is one of these, whatever the case. Up to 50."
      >
        <textarea
          v-model="addressLines"
          class="form-textarea"
          rows="5"
          placeholder="orders@staging.example.com"
          required
        ></textarea>
      </FormField>
      <template #footer>
        <button type="button" class="btn btn-secondary" @click="showForm = false">Cancel</button>
        <button
          type="submit"
          class="btn btn-primary"
          :disabled="saving || !name.trim() || addresses().length === 0"
        >
          {{ saving ? 'Saving...' : 'Save' }}
        </button>
      </template>
    </BaseModal>
  </div>
</template>

<style scoped>
.lead {
  margin: 0 0 16px;
  color: var(--text-secondary);
}

.inboxes-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin: 8px 0 12px;
}

.inboxes-head h2 {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
}

.row-desc {
  margin: 2px 0 0;
  font-size: 12px;
  color: var(--text-muted);
  font-weight: normal;
}

/* Addresses wrap as chips rather than one comma-joined line: an inbox
   with a dozen senders is the normal case, not the wide one. */
.sender-chip {
  display: inline-block;
  margin: 0 6px 4px 0;
}

.muted {
  margin: 0;
  color: var(--text-tertiary);
}
</style>
