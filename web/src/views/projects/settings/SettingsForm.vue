<script setup lang="ts">
// What a project is set to, and the form for changing it.
//
// ONE component for both the editable and the read-only case, because
// they answer the same question and a reader who cannot edit should see
// the same facts in the same order. Which one renders is settings:write,
// checked by the page against its own permission set.
import { computed, ref, watch } from 'vue'
import { projectApi } from '../../../api/projects'
import { apiErrorMessage } from '../../../api/client'
import type { Project } from '../../../api/types'
import { useNotificationStore } from '../../../stores/notification'
import { useProjectStore } from '../../../stores/project'
import { useFieldErrors } from '../../../composables/fieldErrors'
import FormField from '../../../components/FormField.vue'

const props = defineProps<{
  project: Project
  editable: boolean
  /**
   * The plan's ceiling on the sandbox window, or 0 for none.
   *
   * From the usage report the page already loads, so this costs no
   * second request - and the field can say what the plan allows instead
   * of letting the save come back with a different number.
   */
  sandboxCeiling: number
}>()

const emit = defineEmits<{ (e: 'saved', project: Project): void }>()

const notify = useNotificationStore()
const projStore = useProjectStore()
const { errors, capture, clear } = useFieldErrors()

const form = ref(blank())
const saving = ref(false)

function blank() {
  return {
    name: '',
    description: '',
    default_language: '',
    strict_senders: false,
    track_opens: false,
    track_clicks: false,
    bounce_address: '',
    alert_email: '',
    sandbox_retention_days: 0,
  }
}

function fill(p: Project) {
  form.value = {
    name: p.name,
    description: p.description || '',
    default_language: p.default_language,
    strict_senders: p.strict_senders ?? false,
    track_opens: p.track_opens ?? false,
    track_clicks: p.track_clicks ?? false,
    bounce_address: p.bounce_address ?? '',
    alert_email: p.alert_email ?? '',
    sandbox_retention_days: p.sandbox_retention_days ?? 0,
  }
}

watch(() => props.project, fill, { immediate: true })

/**
 * What is wrong with the sandbox window, in words.
 *
 * THE FIELD SAYS SO INSTEAD OF REWRITING WHAT YOU TYPED. Clamping as you
 * type - enter 30 against a 7 day plan and watch it become 7 under your
 * hands - is its own kind of lie, and so is letting the server clamp
 * quietly and returning a different number after the save. The server
 * clamps regardless, which is what makes refusing here safe: nothing is
 * lost if this page is wrong about the ceiling.
 *
 * The empty case is not pedantry. Type letters into a number input and
 * the browser SANITISES them away - Chrome drops the text, leaves value
 * "" and never sets validity.badInput - so `v-model.number` hands over an
 * empty string and the field just looks blank. Nothing native reports
 * it, and sending it would store the installation default under a number
 * the reader believes they typed.
 */
const sandboxError = computed(() => {
  const days = form.value.sandbox_retention_days
  if (typeof days !== 'number' || Number.isNaN(days)) return 'Enter a number of days'
  if (days < 0) return 'Cannot be negative'

  if (props.sandboxCeiling > 0 && days > props.sandboxCeiling) {
    return `Your plan allows a sandbox window of at most ${props.sandboxCeiling} days`
  }

  return ''
})

async function save() {
  if (!form.value.name.trim()) {
    notify.error('Name is required')

    return
  }

  // What this page can tell without asking. The server checks the same
  // things and more, so this is about answering sooner rather than about
  // being the authority.
  if (sandboxError.value) {
    notify.error(
      `Sandbox retention: ${sandboxError.value.charAt(0).toLowerCase()}${sandboxError.value.slice(1)}`,
    )

    return
  }

  clear()
  saving.value = true
  try {
    const res = await projectApi.update(props.project.id, {
      ...form.value,
      name: form.value.name.trim(),
      description: form.value.description.trim(),
      default_language: form.value.default_language.trim(),
      bounce_address: form.value.bounce_address.trim(),
      alert_email: form.value.alert_email.trim(),
    })
    notify.success('Project updated')
    emit('saved', res.data.project)
    // The switcher and the nav read the name from the store.
    await projStore.fetchProjects(true)
  } catch (err) {
    // A refused field says so under the field. Only a failure the server
    // could not attribute to one becomes a toast, or the same sentence
    // appears twice.
    if (!capture(err)) notify.error(apiErrorMessage(err, 'Failed to update project'))
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="card">
    <div class="card-header">
      <h2>{{ editable ? 'Project Settings' : 'Project Details' }}</h2>
    </div>

    <div v-if="!editable" class="card-body">
      <FormField label="Name">
        <div>{{ project.name }}</div>
      </FormField>
      <FormField label="Description">
        <div>{{ project.description || '-' }}</div>
      </FormField>
      <FormField label="Default Language">
        <div>{{ project.default_language }}</div>
      </FormField>
      <p class="form-hint">The settings:write permission is required to edit these.</p>
    </div>

    <div v-else class="card-body">
      <FormField label="Name" :error="errors.name">
        <input v-model="form.name" class="form-input" required />
      </FormField>

      <FormField label="Description" :error="errors.description">
        <input v-model="form.description" class="form-input" placeholder="Optional description" />
      </FormField>

      <FormField
        label="Default Language"
        :error="errors.default_language"
        hint="Language code such as en, de, or fr."
      >
        <input v-model="form.default_language" class="form-input" placeholder="en" maxlength="10" />
      </FormField>

      <FormField
        hint="When on, emails can only be sent from addresses registered under Domains - Sender Addresses. Applies to the API and SMTP relay too."
      >
        <label class="checkbox-label">
          <input v-model="form.strict_senders" type="checkbox" />
          <span>Require registered sender addresses</span>
        </label>
      </FormField>

      <FormField
        hint="Adds a 1x1 pixel to HTML mail sent through the API or the SMTP relay. Campaigns are tracked regardless of this."
      >
        <label class="checkbox-label">
          <input v-model="form.track_opens" type="checkbox" />
          <span>Track opens</span>
        </label>
      </FormField>

      <FormField>
        <label class="checkbox-label">
          <input v-model="form.track_clicks" type="checkbox" />
          <span>Track clicks</span>
        </label>
        <template #hint
          >Rewrites links in HTML mail to redirect through this server. A single send can opt out
          with <code>"disable_tracking": true</code>, or the
          <code>X-Mailyard-Disable-Tracking</code> header over the relay.</template
        >
      </FormField>

      <!-- ONE hint, not two. Both these fields carried a `hint` prop AND
           a #hint slot, and the slot wins - so the prop's sentence never
           rendered at all. The SES caveat here and the paragraph about
           owners below are both text somebody wrote deliberately and
           nobody has ever seen. Merged rather than dropped. -->
      <FormField label="Bounce Address" :error="errors.bounce_address">
        <input
          v-model="form.bounce_address"
          type="email"
          class="form-input"
          placeholder="bounces@bounce.yourdomain.com"
        />
        <template #hint
          >Return-Path for mail sent through this project's own SMTP servers, so delivery failures
          come back here instead of to the From mailbox. Must be on a domain verified under Domains,
          and a subdomain of one counts. That subdomain needs two records of its own: an
          <code>MX</code> pointing at this server, and an <code>SPF</code> record authorizing the IP
          your SMTP server sends from. Leave empty to use the From address. No effect on a provider
          that replaces the Return-Path, such as Amazon SES - those report through their own
          channel.</template
        >
      </FormField>

      <FormField label="Alert Address" :error="errors.alert_email">
        <input
          v-model="form.alert_email"
          type="email"
          class="form-input"
          placeholder="ops@yourcompany.com"
        />
        <template #hint
          >Where this project's alerts go BESIDE its owners - a ticket queue or a shared mailbox
          rather than one person's inbox. Owners always get them: redirecting every warning to an
          address nobody reads, while the people accountable for the project never hear, is not
          something this should let you do quietly. Leave empty for owners only. Alerts cover access
          changes (API keys, members, ownership, sign-in policy), data erasure and the bounce rate
          warning. They need platform mail configured, and an administrator can turn the whole
          channel off with <code>security_alerts_enabled</code>.</template
        >
      </FormField>

      <FormField
        label="Sandbox retention"
        :error="errors.sandbox_retention_days || sandboxError"
        :hint="
          sandboxCeiling > 0
            ? `Days a captured message is kept before it is swept. 0 uses the installation default. Your plan allows at most ${sandboxCeiling}.`
            : 'Days a captured message is kept before it is swept. 0 uses the installation default.'
        "
      >
        <input
          v-model.number="form.sandbox_retention_days"
          type="number"
          class="form-input"
          min="0"
          :max="sandboxCeiling > 0 ? sandboxCeiling : undefined"
        />
      </FormField>

      <FormField label="Slug" hint="The slug cannot be changed after creation.">
        <input :value="project.slug" class="form-input" disabled />
      </FormField>

      <button class="btn btn-primary" :disabled="saving" @click="save">
        {{ saving ? 'Saving...' : 'Save Changes' }}
      </button>
    </div>
  </div>
</template>
