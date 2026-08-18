<script setup lang="ts">
// A DNS record the operator has to publish, with the reason it matters.
//
// Three of these were written out - the SPF fragment and the MX set on
// the platform relay page, the MX set again on a project's own - as the
// same card with the same copy control and the same code block, differing
// only in the words. The record itself is the one thing that varies and
// it comes in as a value.
//
// The prose is a SLOT rather than a prop: every one of these explains
// what breaks when the record is missing, and those explanations carry
// markup and are the whole reason a reader stops to publish it.
import CopyButton from './CopyButton.vue'

defineProps<{
  /** The record type, which is what a reader scans this page for. */
  title: string

  /** The record itself, shown verbatim and copied verbatim. */
  value: string

  /** What the copy control announces, since "Copied" alone says nothing. */
  announce: string
}>()
</script>

<template>
  <div class="card">
    <div class="card-header">
      <div>
        <h2>{{ title }}</h2>
        <p class="text-sm text-muted"><slot /></p>
      </div>
      <CopyButton :value="value" variant="btn btn-sm" :announce="announce" />
    </div>

    <div class="card-body">
      <code class="code-block">{{ value }}</code>
    </div>
  </div>
</template>
