<script setup lang="ts">
// The fields of a campaign, wherever they are being filled in.
//
// ONE component because creating and editing a campaign ask the same
// eleven questions, and they were two copies of the markup with two sets
// of pickers behind them. The copies had already drifted - the create
// dialog offered a Server group and the edit form did not, so saving an
// edit dropped the pool the campaign sent through.
//
// It also owns the four fetches the fields need. Nothing outside a
// campaign form wants the sender list, the template list, the subscriber
// lists or a template's languages.
import { computed, ref, watch } from 'vue'
import { subscriberListsApi } from '../../api/subscriberLists'
import { templatesApi } from '../../api/templates'
import { sendersApi, type Sender } from '../../api/senders'
import { smtpGroupApi } from '../../api/smtpGroups'
import { apiErrorMessage } from '../../api/client'
import type { CampaignVariant, SMTPServerGroup, SubscriberList, Template } from '../../api/types'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import { draftIsReady, type CampaignDraft } from './campaignDraft'
import FormField from '../../components/FormField.vue'
import SenderSelect from '../../components/SenderSelect.vue'

const props = defineProps<{
  /** Field errors from the last refused save, keyed by json name. */
  errors: Record<string, string>
  /**
   * The group the campaign already sends through, by ID. Only the group
   * list can turn it into the slug the form holds, so it is resolved
   * here once that list arrives.
   */
  groupId?: string
}>()

const draft = defineModel<CampaignDraft>({ required: true })
const variants = defineModel<CampaignVariant[]>('variants', { required: true })

const emit = defineEmits<{ (e: 'update:ready', ready: boolean): void }>()

const notify = useNotificationStore()
const projects = useProjectStore()

const lists = ref<SubscriberList[]>([])
const templates = ref<Template[]>([])
const groups = ref<SMTPServerGroup[]>([])
const senders = ref<Sender[]>([])

// Languages the chosen template actually has content for. Empty when the
// fetch fails or it has none, and the field falls back to free text -
// not knowing must not block anybody.
const languages = ref<string[]>([])
const defaultLanguage = ref('')
const languagesLoading = ref(false)

const ready = computed(() => draftIsReady(draft.value, variants.value))
watch(ready, (v) => emit('update:ready', v), { immediate: true })

const splitTotal = computed(() =>
  variants.value.reduce((sum, v) => sum + (Number(v.split_percentage) || 0), 0),
)

async function loadPickers() {
  try {
    const [l, t, g] = await Promise.all([
      subscriberListsApi.list(),
      templatesApi.list(),
      smtpGroupApi.list(),
    ])
    lists.value = l.data.subscriber_lists ?? []
    templates.value = t.data.templates ?? []
    groups.value = g.data.smtp_server_groups ?? []

    if (props.groupId) {
      draft.value.smtp_group = groups.value.find((x) => x.id === props.groupId)?.slug ?? ''
    }
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load the templates, lists and server groups'))
  }
}

/**
 * The project's approved From addresses.
 *
 * Senders is its own resource and a campaign belongs to another, so a
 * role that may draft mail without seeing the approved addresses is
 * ordinary. Asking anyway is a 403 in the console for an endpoint this
 * person is not meant to know exists - the field stays free text.
 */
async function loadSenders() {
  if (!projects.can('senders:read')) return

  try {
    senders.value = (await sendersApi.list()).data.senders ?? []
  } catch {
    senders.value = []
  }
}

/** Fill the display name from the sender, never over one already typed. */
function onSenderPicked(s: Sender | null) {
  if (s?.name && !draft.value.from_name.trim()) draft.value.from_name = s.name
}

async function loadLanguages(templateId: string) {
  languages.value = []
  defaultLanguage.value = ''
  if (!templateId) return

  languagesLoading.value = true
  try {
    const res = await templatesApi.get(templateId)
    defaultLanguage.value = res.data.template.default_language ?? ''

    // The active version if there is one, else the newest: a draft
    // pointed at a template nobody has activated yet should still offer
    // the languages that template has been written in.
    const versions = res.data.versions ?? []
    const versionId =
      res.data.template.active_version_id ||
      (versions.length ? versions.reduce((a, b) => (b.version > a.version ? b : a)).id : '')
    if (!versionId) return

    const locs = await templatesApi.listLocalizations(templateId, versionId)
    languages.value = (locs.data.localizations ?? []).map((l) => l.language)
  } catch {
    // Falls back to the free-text input. Nothing is blocked.
    languages.value = []
  } finally {
    languagesLoading.value = false
  }
}

watch(() => draft.value.template_id, loadLanguages, { immediate: true })

function addVariant() {
  variants.value = [
    ...variants.value,
    { name: '', subject: '', template_id: '', split_percentage: 50 },
  ]
}

function removeVariant(index: number) {
  variants.value = variants.value.filter((_, i) => i !== index)
}

void loadPickers()
void loadSenders()
</script>

<template>
  <div>
    <FormField label="Name" required :error="errors.name">
      <input v-model="draft.name" class="form-input" />
    </FormField>

    <FormField
      label="Subject"
      :error="errors.subject"
      hint="Overridden per variant when the list is split."
    >
      <input v-model="draft.subject" class="form-input" />
    </FormField>

    <FormField label="From address" required :error="errors.from_email">
      <SenderSelect v-model="draft.from_email" :senders="senders" @sender="onSenderPicked" />
    </FormField>

    <FormField label="From name" :error="errors.from_name">
      <input v-model="draft.from_name" class="form-input" />
    </FormField>

    <FormField label="Template" required :error="errors.template_id">
      <select v-model="draft.template_id" class="form-select">
        <option value="" disabled>Pick a template</option>
        <option v-for="t in templates" :key="t.id" :value="t.id">{{ t.name }}</option>
      </select>
    </FormField>

    <FormField label="Language">
      <select v-if="languagesLoading" class="form-select" disabled>
        <option>Reading the template...</option>
      </select>

      <select v-else-if="languages.length" v-model="draft.language" class="form-select">
        <option value="">Template default</option>
        <option v-for="lang in languages" :key="lang" :value="lang">
          {{ lang }}{{ lang === defaultLanguage ? ' (default)' : '' }}
        </option>
        <!-- A language the campaign already names and the template no
             longer has. Listed so the control shows what is stored
             rather than snapping to something else. -->
        <option
          v-if="draft.language && !languages.includes(draft.language)"
          :value="draft.language"
        >
          {{ draft.language }} (not in this template)
        </option>
      </select>

      <input v-else v-model="draft.language" class="form-input" placeholder="en" />
    </FormField>

    <FormField label="Subscriber list" required :error="errors.list_id">
      <select v-model="draft.list_id" class="form-select">
        <option value="" disabled>Pick a list</option>
        <option v-for="l in lists" :key="l.id" :value="l.id">{{ l.name }} ({{ l.type }})</option>
      </select>
    </FormField>

    <!-- Only worth a control when there is a choice. -->
    <FormField
      v-if="groups.length > 1"
      label="Server group"
      :error="errors.smtp_group"
      hint="Which SMTP pool this campaign sends through. Bulk on its own pool keeps a bad campaign from taking transactional mail with it."
    >
      <select v-model="draft.smtp_group" class="form-select">
        <option value="">The project's default group</option>
        <option v-for="g in groups" :key="g.id" :value="g.slug">
          {{ g.name }}{{ g.is_default ? ' (default)' : '' }}
        </option>
      </select>
    </FormField>

    <FormField
      label="Send rate"
      :error="errors.send_rate"
      hint="Emails per minute. 0 sends as fast as the queue allows."
    >
      <input v-model.number="draft.send_rate" type="number" class="form-input" min="0" />
    </FormField>

    <FormField hint="Needs a timezone on the subscriber, or they send immediately.">
      <label class="checkbox-label">
        <input v-model="draft.send_at_local_time" type="checkbox" />
        Send at each subscriber's local time
      </label>
    </FormField>

    <FormField
      label="Template data (JSON)"
      :error="errors.template_data"
      hint="Merged under each subscriber's own fields."
    >
      <textarea v-model="draft.template_data" class="form-textarea code-font" rows="4"></textarea>
    </FormField>

    <FormField>
      <label class="checkbox-label">
        <input v-model="draft.ab_test_enabled" type="checkbox" />
        Split the list between variants
      </label>
    </FormField>

    <FormField v-if="draft.ab_test_enabled" label="Variants">
      <div v-for="(v, i) in variants" :key="i" class="variant">
        <input v-model="v.name" class="form-input" placeholder="Name" />
        <input v-model="v.subject" class="form-input wide" placeholder="Subject (optional)" />
        <select v-model="v.template_id" class="form-select wide">
          <option value="">The campaign's template</option>
          <option v-for="t in templates" :key="t.id" :value="t.id">{{ t.name }}</option>
        </select>
        <input
          v-model.number="v.split_percentage"
          type="number"
          class="form-input share"
          min="1"
          max="100"
        />
        <button class="btn btn-danger btn-sm" @click="removeVariant(i)">Remove</button>
      </div>

      <button class="btn btn-secondary btn-sm" @click="addVariant">Add a variant</button>

      <p v-if="variants.length < 2" class="form-hint mt-2">A split needs at least two variants.</p>
      <p v-else-if="splitTotal !== 100" class="form-error mt-2">
        The shares total {{ splitTotal }}. They have to total 100, or some of the list is addressed
        by nothing.
      </p>
    </FormField>
  </div>
</template>

<style scoped>
/* One variant per line, wrapping rather than squeezing: five controls
   need about 700px and neither the dialog nor the card is always that
   wide. */
.variant {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}

.variant .form-input,
.variant .form-select {
  flex: 1;
  min-width: 120px;
}

/* A subject and a template name are longer than a variant label, so they
   take twice the share of what is left. */
.variant .wide {
  flex: 2;
}

/* Two or three digits, and it must not stretch to fill the row. */
.variant .share {
  flex: 0 0 80px;
  min-width: 80px;
}
</style>
