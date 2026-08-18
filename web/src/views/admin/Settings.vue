<script setup lang="ts">
// Every setting the installation holds, and one Save for all of them.
//
// Edits are staged locally rather than written per field: the endpoint
// takes a batch, and a page that saved each control as it lost focus
// would leave the installation half-applied while somebody worked down
// the list.
//
// What this file is left with is that staging. Which control a setting
// gets is SettingRow's problem, and the scheduled jobs below share
// nothing with any of it - not a value, not a request, not a save.
import { computed, onMounted, ref } from 'vue'
import { settingsApi, type PlatformSetting } from '../../api/settings'
import { apiErrorMessage } from '../../api/client'
import { useNotificationStore } from '../../stores/notification'
import LoadingBlock from '../../components/LoadingBlock.vue'
import PageHeader from '../../components/PageHeader.vue'
import SettingRow from './settings/SettingRow.vue'
import ScheduledJobs from './settings/ScheduledJobs.vue'
import { encodeLines, isList, linesOf } from './settings/settingValue'

const notify = useNotificationStore()

const settings = ref<PlatformSetting[]>([])
const loading = ref(true)
const saving = ref(false)

// Keyed by setting key, in the shape the CONTROL edits - a list is
// staged as lines, not as the JSON array it is stored in.
const edits = ref<Record<string, string | number>>({})

/**
 * A staged value back in the shape the server stores.
 *
 * The dirty check compares in the WIRE shape, not the on-screen one:
 * comparing a textarea full of lines against a stored JSON array would
 * call every list setting dirty the moment the page loaded.
 */
function wireValue(s: PlatformSetting): string {
  const value = edits.value[s.key]
  if (value === undefined) return ''

  return isList(s) ? encodeLines(String(value)) : String(value)
}

const dirty = computed(() =>
  settings.value.filter((s) => edits.value[s.key] !== undefined && wireValue(s) !== s.value),
)

/** Load the editable form of every value. Also what Discard restores. */
function stage() {
  edits.value = {}
  for (const s of settings.value) {
    edits.value[s.key] = isList(s) ? linesOf(s.value) : s.value
  }
}

async function load() {
  loading.value = true
  try {
    settings.value = (await settingsApi.list()).data.settings ?? []
    stage()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load platform settings'))
  } finally {
    loading.value = false
  }
}

async function save() {
  if (dirty.value.length === 0) return

  saving.value = true
  try {
    // Everything goes as a string: v-model on a number input yields a
    // number and the setting wire format is textual. A missing edit
    // becomes "" rather than "undefined", which is what String(undefined)
    // gives - and "" is how the server is told to fall back to the
    // default, which is the intended way to clear a setting.
    const res = await settingsApi.update(
      dirty.value.map((s) => ({ key: s.key, value: wireValue(s) })),
    )
    settings.value = res.data.settings ?? []
    stage()
    notify.success('Settings saved')
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to save settings'))
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <div>
    <PageHeader title="Platform Settings">
      <button class="btn btn-secondary" :disabled="dirty.length === 0 || saving" @click="stage">
        Discard
      </button>
      <button class="btn btn-primary" :disabled="dirty.length === 0 || saving" @click="save">
        {{ saving ? 'Saving...' : `Save${dirty.length ? ` (${dirty.length})` : ''}` }}
      </button>
    </PageHeader>

    <LoadingBlock v-if="loading" />

    <template v-else>
      <div class="card">
        <div class="card-header">
          <h2>Settings</h2>
        </div>
        <div class="card-body">
          <p class="text-sm text-muted mb-4">
            These apply to the whole installation and take effect immediately. A retention window of
            0 means data is kept forever.
          </p>

          <SettingRow v-for="s in settings" :key="s.key" v-model="edits[s.key]" :setting="s" />
        </div>
      </div>

      <ScheduledJobs />
    </template>
  </div>
</template>
