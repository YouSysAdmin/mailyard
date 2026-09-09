<script setup lang="ts">
// One platform setting: what it is, and the control for changing it.
//
// The control has five shapes and choosing between them is the whole
// difficulty of this page - a switch, a number with a unit, a list edited
// as lines, plain text, and the case with NO control at all. Its own
// component, so the view's job stays staging edits.
import { computed } from 'vue'
import type { PlatformSetting } from '../../../api/settings'
import { formatDate } from '../../../composables/formatDate'
import { displayValue, isBool, isInt, isList } from './settingValue'

const props = defineProps<{ setting: PlatformSetting }>()

/** The staged value, in the shape the control edits. */
const staged = defineModel<string | number>({ required: true })

/**
 * Whether the control goes full width UNDER the description rather than
 * in the column on the right.
 *
 * The right-hand column is 110px, which suits a retention window in days
 * and nothing else - an SES topic ARN, a public URL, a from address or a
 * certificate name would be typed into it four characters at a time.
 * Numbers and switches stay put, since widening those turns the page
 * into a column of near-empty boxes.
 */
const wide = computed(() => {
  // No control, just a value and a link, so it stays compact. Full width
  // pushed those two to opposite ends of the row.
  if (props.setting.managed_at) return false

  return !isBool(props.setting) && !isInt(props.setting)
})

const on = computed(() => staged.value === 'true')

function setBool(checked: boolean) {
  staged.value = checked ? 'true' : 'false'
}
</script>

<template>
  <div class="setting-row" :class="{ 'setting-row-wide': wide }">
    <div class="setting-label">
      <code>{{ setting.key }}</code>
      <span v-if="setting.overridden" class="badge badge-info">changed</span>
      <p class="setting-desc">{{ setting.description }}</p>
      <p v-if="setting.updated_by" class="setting-meta">
        Last changed by {{ setting.updated_by }} on {{ formatDate(setting.updated_at) }}
      </p>
    </div>

    <!-- Shown here, edited on its own page. Still shown, because this
         page is the inventory of what the installation is set to. -->
    <div v-if="setting.managed_at" class="setting-control">
      <span class="setting-managed">{{ displayValue(setting) }}</span>
      <router-link :to="`/${setting.managed_at}`" class="setting-managed-link">
        Managed in {{ setting.managed_in }}
      </router-link>
    </div>

    <div v-else class="setting-control">
      <label v-if="isBool(setting)" class="switch-label">
        <input
          type="checkbox"
          :checked="on"
          @change="setBool(($event.target as HTMLInputElement).checked)"
        />
        <span>{{ on ? 'On' : 'Off' }}</span>
      </label>

      <div v-else-if="isInt(setting)" class="input-with-unit">
        <input
          v-model="staged"
          type="number"
          min="0"
          class="form-input"
          :aria-label="setting.key"
        />
        <span v-if="setting.unit" class="unit">{{ setting.unit }}</span>
      </div>

      <textarea
        v-else-if="isList(setting)"
        v-model="staged"
        class="form-textarea setting-textarea"
        rows="3"
        spellcheck="false"
        placeholder="one value per line"
        :aria-label="setting.key"
      ></textarea>

      <div v-else class="input-with-unit">
        <input
          v-model="staged"
          type="text"
          class="form-input"
          :placeholder="setting.default || ''"
          :aria-label="setting.key"
          autocomplete="off"
          spellcheck="false"
        />
        <span v-if="setting.unit" class="unit">{{ setting.unit }}</span>
      </div>

      <span v-if="setting.default" class="setting-default">default {{ setting.default }}</span>
    </div>
  </div>
</template>

<style scoped>
.setting-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 24px;
  padding: 14px 0;
  border-bottom: 1px solid var(--border-primary);
}

.setting-row:last-child {
  border-bottom: none;
}

.setting-label {
  flex: 1;
  min-width: 0;
}

.setting-label code {
  font-size: 13px;
}

.setting-desc {
  margin: 4px 0 0;
  font-size: 13px;
  color: var(--text-secondary);
}

.setting-meta {
  margin: 4px 0 0;
  font-size: 12px;
  color: var(--text-muted);
}

.setting-control {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 4px;
  flex-shrink: 0;
}

/* The value as text plus a link, right-aligned like the controls. */
.setting-managed {
  font-size: 13px;
  color: var(--text-primary);
  text-align: right;
  word-break: break-word;
}

.setting-managed-link {
  font-size: 12px;
  color: var(--text-secondary);
  white-space: nowrap;
}

.setting-managed-link:hover {
  color: var(--primary-500);
}

/* Stacked: the label and description on one line, the control on its own
   beneath them at full width. */
.setting-row-wide {
  flex-direction: column;
  align-items: stretch;
  gap: 8px;
}

.setting-row-wide .setting-control {
  align-items: stretch;
  width: 100%;
}

.setting-row-wide .input-with-unit .form-input {
  width: 100%;
}

.setting-row-wide .setting-default {
  text-align: left;
}

.setting-textarea {
  width: 100%;
  /* .form-textarea is sized for one line - it sets height to the shared
     control height and zero vertical padding, which is right for the
     inputs beside it and wrong for a list. min-height wins over height,
     and the padding has to be put back by hand. */
  min-height: 84px;
  height: auto;
  padding: 8px 12px;
  line-height: 1.5;
  font-family: var(--font-mono);
  font-size: 13px;
  /* A hostname list is a column of short lines, so wrapping would only
     ever hide a typo mid-name. */
  white-space: pre;
  overflow-x: auto;
  resize: vertical;
}

.setting-default {
  font-size: 12px;
  color: var(--text-muted);
}

.input-with-unit {
  display: flex;
  align-items: center;
  gap: 6px;
}

.input-with-unit .form-input {
  width: 110px;
}

.unit {
  font-size: 13px;
  color: var(--text-muted);
}

.switch-label {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  cursor: pointer;
}
</style>
