<script setup lang="ts">
import CopyButton from './CopyButton.vue'
import type { DNSRecord } from '../api/domains'

// Renders every DNS record a domain needs, with its current state.
//
// A component rather than markup repeated in each modal: the add-domain
// flow and the manage-records flow show the same thing, and they had
// already been copy-pasted once when there was a single record to show.
defineProps<{ records: DNSRecord[] }>()

const LABELS: Record<string, string> = {
  ownership: 'Ownership',
  spf: 'SPF',
  dkim: 'DKIM',
  dmarc: 'DMARC',
}

function label(kind: string): string {
  return LABELS[kind] ?? kind.toUpperCase()
}
</script>

<template>
  <div class="dns-records">
    <div v-for="rec in records" :key="rec.kind">
      <div class="dns-group-head">
        <span class="dns-group-name">{{ label(rec.kind) }}</span>
        <span v-if="rec.required" class="badge badge-neutral">Required</span>
        <span v-else class="badge badge-neutral">Recommended</span>
        <span :class="rec.verified ? 'badge badge-success' : 'badge badge-warning'">
          {{ rec.verified ? 'Found' : 'Not found' }}
        </span>
      </div>
      <p v-if="rec.detail" class="dns-group-detail">{{ rec.detail }}</p>

      <!-- A record with no value is one we cannot build yet, for
           example DKIM before a key exists. Showing empty Host and
           Value boxes would invite somebody to publish them. -->
      <div v-if="rec.value" class="dns-record">
        <div class="dns-record-row">
          <span class="dns-record-label">Type</span>
          <code class="dns-record-value">{{ rec.type }}</code>
          <CopyButton :value="rec.type" />
        </div>
        <div class="dns-record-row">
          <span class="dns-record-label">Host</span>
          <code class="dns-record-value">{{ rec.host }}</code>
          <CopyButton :value="rec.host" />
        </div>
        <div class="dns-record-row">
          <span class="dns-record-label">Value</span>
          <code class="dns-record-value">{{ rec.value }}</code>
          <CopyButton :value="rec.value" />
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.dns-records {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.dns-group-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
}

.dns-group-name {
  font-size: 13px;
  font-weight: 500;
  color: var(--text-primary);
}

.dns-record {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.dns-record-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  border: 1px solid var(--border-primary);
  border-radius: var(--radius);
  background: var(--bg-secondary);
}

.dns-record-label {
  width: 48px;
  flex-shrink: 0;
  font-size: 12px;
  font-weight: 600;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.05em;
}

.dns-record-value {
  flex: 1;
  font-size: 12px;
  word-break: break-all;
}

.dns-group-detail {
  margin: 0 0 8px;
  font-size: 12px;
  line-height: 1.5;
  color: var(--text-muted);
}
</style>
