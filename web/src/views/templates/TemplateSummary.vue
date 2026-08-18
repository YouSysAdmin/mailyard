<script setup lang="ts">
// What this template IS - the facts that do not change as you move
// between its versions.
//
// Read-only on purpose: editing lives in the settings dialog, so this
// card never has to know whether a save is in flight.
import { computed } from 'vue'
import type { Template, TemplateVersion } from '../../api/types'
import { formatDate } from '../../composables/formatDate'
import CopyButton from '../../components/CopyButton.vue'

const props = defineProps<{
  template: Template
  versions: TemplateVersion[]
}>()

// The number a person reads, not the id they never see. Unknown while
// the version list is still arriving, which is why it is a dash rather
// than an empty badge.
const activeNumber = computed(() => {
  const active = props.versions.find((v) => v.id === props.template.active_version_id)

  return active ? `v${active.version}` : '-'
})
</script>

<template>
  <div class="card">
    <dl class="facts">
      <dt>Template ID</dt>
      <dd>
        <code class="id">{{ template.id }}</code>
        <CopyButton :value="template.id" copied-label="Copied!" variant="id-copy" />
      </dd>

      <template v-if="template.description">
        <dt>Description</dt>
        <dd>{{ template.description }}</dd>
      </template>

      <dt>Default language</dt>
      <dd>
        <span class="badge badge-info">{{ template.default_language }}</span>
      </dd>

      <dt>Active version</dt>
      <dd>
        <span v-if="template.active_version_id" class="badge badge-success">
          {{ activeNumber }}
        </span>
        <span v-else class="text-muted">None</span>
      </dd>

      <dt>Created</dt>
      <dd>{{ formatDate(template.created_at) }}</dd>

      <template v-if="template.updated_at">
        <dt>Updated</dt>
        <dd>{{ formatDate(template.updated_at) }}</dd>
      </template>
    </dl>
  </div>
</template>

<style scoped>
/* Placement only - the list itself is the stylesheet's. This one IS the
   card body rather than sitting inside one, so it carries the padding a
   .card-body would have given it. */
.facts {
  padding: 20px;
}

.id {
  font-family: var(--font-mono);
  font-size: 13px;
  padding: 2px 8px;
  border: 1px solid var(--border-primary);
  border-radius: var(--radius);
  background: var(--bg-tertiary);
  /* One click selects the whole id, which is what it is copied for. */
  user-select: all;
}

/* The copy control beside the id is a hint, not an action worth a
   filled button - it sits at the size of the text it follows. */
.id-copy {
  margin-left: 6px;
  padding: 2px 6px;
  border: 1px solid var(--border-primary);
  border-radius: var(--radius);
  background: none;
  color: var(--text-secondary);
  font-size: 12px;
  vertical-align: middle;
  cursor: pointer;
}

.id-copy:hover {
  background: var(--bg-tertiary);
  color: var(--text-primary);
}
</style>
