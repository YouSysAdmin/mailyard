<script setup lang="ts">
// What a campaign looks like in somebody's inbox.
//
// Rendered by the SERVER, through the same function the runner sends
// with, so the variant overrides, the subscriber's language and their
// custom fields all land the way they will on the day. Opening with
// nobody picked renders the campaign's own template data and nothing
// else. NOT the template's sample data: a send never reads it, and a
// preview that filled the blanks from it looked fine until the mail
// went out empty. Picking a subscriber re-renders for them.
import { computed, ref, watch } from 'vue'
import { campaignsApi } from '../../api/campaigns'
import type { RenderedPreview } from '../../api/templates'
import { apiErrorMessage } from '../../api/client'
import type { Campaign, Subscriber } from '../../api/types'
import { useNotificationStore } from '../../stores/notification'
import { formatMailbox } from '../../composables/mailbox'
import BaseModal from '../../components/BaseModal.vue'
import FormField from '../../components/FormField.vue'
import HtmlPreview from '../../components/HtmlPreview.vue'
import LoadingBlock from '../../components/LoadingBlock.vue'
import SubscriberPicker from '../../components/SubscriberPicker.vue'

const props = defineProps<{ campaign: Campaign }>()
const emit = defineEmits<{ (e: 'close'): void }>()

const notify = useNotificationStore()

const address = ref('')
const subscriber = ref<Subscriber | null>(null)
const variant = ref(props.campaign.ab_variants?.[0]?.name ?? '')
const variants = computed(() =>
  props.campaign.ab_test_enabled ? (props.campaign.ab_variants ?? []) : [],
)

const rendered = ref<RenderedPreview | null>(null)
const loading = ref(true)
const pane = ref<'html' | 'text'>('html')

// Picking a subscriber and switching the variant a moment later is two
// renders in flight, and the slower one must not land on top.
let serial = 0

async function render() {
  const mine = ++serial
  loading.value = true
  try {
    const res = await campaignsApi.preview(props.campaign.id, {
      subscriber_id: subscriber.value?.id || undefined,
      variant: variant.value || undefined,
    })
    if (mine !== serial) return

    rendered.value = res.data.preview
    if (!rendered.value.html && rendered.value.text) pane.value = 'text'
  } catch (e) {
    if (mine === serial) notify.error(apiErrorMessage(e, 'Failed to render the campaign'))
  } finally {
    if (mine === serial) loading.value = false
  }
}

// A picked row re-renders, and so does clearing one: the text no longer
// names a subscriber, so the sample comes back.
function onPick(s: Subscriber | null) {
  if (s === subscriber.value) return

  subscriber.value = s
  void render()
}

watch(variant, () => void render())

void render()
</script>

<template>
  <BaseModal title="Preview" size="modal-w860" @close="emit('close')">
    <div class="preview-controls">
      <FormField label="As subscriber" for="preview-subscriber" class="preview-picker">
        <SubscriberPicker
          id="preview-subscriber"
          v-model="address"
          placeholder="Type an address to search"
          @pick="onPick"
        />
      </FormField>

      <FormField v-if="variants.length > 0" label="Variant" for="preview-variant">
        <select id="preview-variant" v-model="variant" class="form-select">
          <option v-for="v in variants" :key="v.name" :value="v.name">{{ v.name }}</option>
        </select>
      </FormField>
    </div>

    <p class="form-hint preview-note">
      <template v-if="subscriber">
        Rendered for {{ formatMailbox(subscriber.email, subscriber.name) }}, with their language and
        custom fields.
      </template>
      <template v-else>
        Rendered with the campaign's template data only, as a subscriber with no custom fields would
        get it. Pick a subscriber to see their message.
      </template>
      Web view and unsubscribe links are left out here, since no message exists for them to open.
    </p>

    <LoadingBlock v-if="loading && !rendered" />

    <template v-else-if="rendered">
      <dl class="field-grid">
        <dt>Subject</dt>
        <dd>{{ rendered.subject || '-' }}</dd>

        <dt>From</dt>
        <dd>{{ formatMailbox(campaign.from_email, campaign.from_name) }}</dd>
      </dl>

      <div v-if="rendered.html && rendered.text" class="tabs preview-tabs">
        <button
          type="button"
          class="tab"
          :class="{ active: pane === 'html' }"
          @click="pane = 'html'"
        >
          HTML
        </button>
        <button
          type="button"
          class="tab"
          :class="{ active: pane === 'text' }"
          @click="pane = 'text'"
        >
          Text
        </button>
      </div>

      <HtmlPreview
        v-if="pane === 'html' && rendered.html"
        :html="rendered.html"
        min-height="480px"
        title="Campaign preview"
      />
      <pre v-else-if="rendered.text" class="preview-text">{{ rendered.text }}</pre>
      <p v-else class="text-sm text-muted">The template renders an empty body.</p>
    </template>

    <template #footer>
      <button class="btn btn-primary" @click="emit('close')">Close</button>
    </template>
  </BaseModal>
</template>

<style scoped>
/* The picker takes the room, the variant select what it needs. */
.preview-controls {
  display: flex;
  gap: 16px;
  align-items: flex-start;
  flex-wrap: wrap;
}

.preview-picker {
  flex: 1 1 280px;
}

.preview-note {
  margin: -8px 0 16px;
}

.preview-tabs {
  margin-bottom: 12px;
}

.field-grid {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: 6px 16px;
  margin: 0 0 16px;
}

.field-grid dt {
  font-size: 13px;
  color: var(--text-muted);
}

.field-grid dd {
  margin: 0;
  font-size: 13px;
  word-break: break-word;
}

.preview-text {
  white-space: pre-wrap;
  word-wrap: break-word;
  font-size: 13px;
  color: var(--text-secondary);
  line-height: 1.6;
  margin: 0;
}
</style>
