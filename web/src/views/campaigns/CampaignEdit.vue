<script setup lang="ts">
// Editing a campaign that has not been sent.
//
// Only a DRAFT reaches this: once a campaign starts sending, its list,
// template and variants are what the messages already in flight were
// built from, and changing them under those would make the record a lie.
//
// The fields are CampaignFields, shared with the create dialog, and the
// body is built by toPayload. Both are shared on purpose: the endpoint
// rebuilds the whole record from what it is sent, so a field one form
// has and the other lacks is a field that editing silently clears -
// which is what used to happen to the server group.
import { ref } from 'vue'
import { campaignsApi } from '../../api/campaigns'
import { apiErrorMessage } from '../../api/client'
import type { Campaign, CampaignVariant } from '../../api/types'
import { useNotificationStore } from '../../stores/notification'
import { useFieldErrors } from '../../composables/fieldErrors'
import { fromCampaign, toPayload } from './campaignDraft'
import CampaignFields from './CampaignFields.vue'

const props = defineProps<{ campaign: Campaign }>()

const emit = defineEmits<{
  (e: 'saved'): void
  (e: 'close'): void
}>()

const notify = useNotificationStore()
const { errors, capture, clear } = useFieldErrors()

const draft = ref(fromCampaign(props.campaign))
const variants = ref<CampaignVariant[]>((props.campaign.ab_variants ?? []).map((v) => ({ ...v })))
const ready = ref(false)
const saving = ref(false)

async function save() {
  if (!ready.value) return

  clear()
  saving.value = true
  try {
    await campaignsApi.update(props.campaign.id, toPayload(draft.value, variants.value))
    notify.success('Campaign updated')
    emit('saved')
  } catch (e) {
    if (e instanceof SyntaxError) notify.error('The template data is not valid JSON')
    else if (!capture(e)) notify.error(apiErrorMessage(e, 'Failed to update the campaign'))
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="card">
    <div class="card-header">
      <h2>Edit</h2>
    </div>

    <div class="card-body">
      <CampaignFields
        v-model="draft"
        v-model:variants="variants"
        :errors="errors"
        :group-id="campaign.smtp_group_id"
        @update:ready="ready = $event"
      />

      <div class="actions">
        <button class="btn btn-primary" :disabled="saving || !ready" @click="save">
          {{ saving ? 'Saving...' : 'Save' }}
        </button>
        <button class="btn btn-secondary" @click="emit('close')">Close</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.actions {
  display: flex;
  gap: 8px;
  margin-top: 16px;
}
</style>
