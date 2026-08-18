<script setup lang="ts">
// The right-hand pane of a mail client: one message, read.
//
// TWO of them exist - the sandbox and the inbound log - and they are the
// same shell: a subject with the actions beside it, From and To, an
// envelope behind a toggle, and the body taking everything left. The
// second was written by mirroring the first and 157 lines came across
// with it, most of them CSS.
//
// It fills the height it is given and NOTHING here scrolls except the
// two regions that are meant to: the address block, which is capped, and
// whatever the caller puts in the default slot. A page that scrolls with
// a frame that also scrolls is two scrollbars for one document.
//
// What the two readers actually differ in is slotted: which buttons the
// head carries, whether the sender is worth a verdict badge, and what
// the envelope holds - a captured message came from a credential we
// issued, so who sent it is never in question, and one that arrived from
// the internet is a different set of facts entirely.
import { computed, ref, watch } from 'vue'
import CopyButton from './CopyButton.vue'

const props = defineProps<{
  subject?: string
  sender?: string
  recipients: string[]
  /** Identifies the message being read, so the panes reset when it changes. */
  messageId?: string
}>()

// The recipient list is the one field with no upper bound: a single
// message can carry hundreds. Rendered in full it owns the pane and the
// body becomes a sliver, so the header shows ONE line and the list
// itself is opt-in.
const allRecipients = ref(false)

// The envelope, collapsed by default. It answers a question asked once
// per message at most, and open it costs the body a third of the pane.
const detailsOpen = ref(false)

const summary = computed(() => {
  const to = props.recipients ?? []
  if (to.length === 0) return '(none)'
  if (to.length <= 2) return to.join(', ')

  return `${to[0]} and ${to.length - 1} more`
})

const many = computed(() => (props.recipients?.length ?? 0) > 2)

// Both panes close on a new message. Carrying an expanded recipient list
// from one message to the next shows the new one's addresses under a
// control the reader never pressed for it.
watch(
  () => props.messageId,
  () => {
    allRecipients.value = false
    detailsOpen.value = false
  },
)
</script>

<template>
  <div class="reader">
    <div class="reader-head">
      <h2 class="reader-subject">{{ subject || '(no subject)' }}</h2>
      <div class="reader-actions"><slot name="actions" /></div>
    </div>

    <!-- Everything between the subject and the body is ONE capped,
         scrollable region, so a message addressed to fifty people cannot
         squeeze the body down to a sliver. -->
    <div class="reader-meta">
      <dl class="reader-addr">
        <dt>From</dt>
        <dd>
          {{ sender || '(empty)' }}
          <!-- Where the inbound reader puts its authentication verdict.
               The sandbox has none to give: a capture came from a
               credential we issued. -->
          <slot name="sender" />
        </dd>
        <dt>To</dt>
        <dd class="addr-to">
          <button v-if="many" class="addr-more" @click="allRecipients = !allRecipients">
            {{ allRecipients ? 'Hide the list' : summary }}
          </button>
          <span v-else>{{ summary }}</span>
          <CopyButton
            v-if="recipients.length"
            :value="recipients.join(', ')"
            label="Copy"
            variant="btn btn-secondary btn-sm"
          />
        </dd>
      </dl>

      <!-- One address per line and its own scroll. A comma-separated
           blob is what the Copy button is for - reading down a column is
           how somebody checks whether one address is in there. -->
      <ul v-if="allRecipients" class="addr-list">
        <li v-for="r in recipients" :key="r">{{ r }}</li>
      </ul>

      <button
        class="reader-toggle"
        :aria-expanded="detailsOpen"
        @click="detailsOpen = !detailsOpen"
      >
        {{ detailsOpen ? 'Hide envelope' : 'Show envelope' }}
      </button>

      <dl v-if="detailsOpen" class="reader-details"><slot name="envelope" /></dl>
    </div>

    <div class="reader-body"><slot /></div>
  </div>
</template>

<style scoped>
.reader {
  display: flex;
  flex-direction: column;
  /* min-height:0 because a grid item defaults to min-content, which
     would let the body push this column taller than the pane and put the
     scrollbar back on the page. */
  height: 100%;
  min-height: 0;
}

.reader-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  padding: 16px var(--gutter) 8px;
}

.reader-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
}

.reader-subject {
  margin: 0;
  font-size: 18px;
  line-height: 1.35;
  /* A subject has no length limit, so it wraps rather than pushing the
     buttons off the pane - and stops at three lines, because a
     170-character one otherwise owns the top of the pane. The whole
     subject is one tab away in Headers. */
  overflow-wrap: anywhere;
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

/* No cap of its own. The BODY carries the floor instead and this shrinks
   into its own scroll to honour it, so the split is decided by what is
   being read rather than by a percentage somebody guessed. min-height:0
   is what permits the shrink at all - a flex item will not go below its
   content without it. */
.reader-meta {
  flex: 0 1 auto;
  min-height: 0;
  overflow-y: auto;
}

/* Basis ZERO, not auto - with auto the basis is the CONTENT height, so a
   long message claims all of it and squeezes the header above out of
   existence. No min-height here either: the viewer's own body floor is
   this item's min-content, which is what makes the meta region shrink
   first. */
.reader-body {
  display: flex;
  flex-direction: column;
  flex: 1 1 0;
  min-height: 0;
}

.addr-more {
  padding: 0;
  border: none;
  background: none;
  color: var(--primary-600);
  font-size: 13px;
  text-align: left;
  cursor: pointer;
}

.addr-more:hover {
  text-decoration: underline;
}

.addr-list {
  max-height: 9rem;
  margin: 0 0 12px;
  padding: 8px var(--gutter);
  overflow-y: auto;
  list-style: none;
  background: var(--bg-secondary);
  border-top: 1px solid var(--border-primary);
  border-bottom: 1px solid var(--border-primary);
  font-size: 13px;
}

.addr-list li {
  padding: 1px 0;
  overflow-wrap: anywhere;
}

/* Label and value on one line each, the labels sharing a column so the
   two addresses line up - which is the whole reason this is a dl and not
   two paragraphs. */
.reader-addr {
  display: grid;
  grid-template-columns: 3.5rem minmax(0, 1fr);
  align-items: baseline;
  gap: 6px 12px;
  margin: 0;
  padding: 0 var(--gutter) 12px;
}

.reader-addr dt {
  color: var(--text-tertiary);
  font-size: 13px;
}

.reader-addr dd {
  margin: 0;
  font-size: 13px;
  overflow-wrap: anywhere;
}

.addr-to {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
}

.reader-toggle {
  align-self: flex-start;
  margin: 0 var(--gutter) 12px;
  padding: 0;
  border: none;
  background: none;
  color: var(--primary-600);
  font-size: 13px;
  cursor: pointer;
}

.reader-toggle:hover {
  text-decoration: underline;
}

/* As many columns as fit. The two readers put different facts in here
   and neither knows how many. */
.reader-details {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 12px 16px;
  margin: 0;
  padding: 12px var(--gutter);
  border-top: 1px solid var(--border-primary);
  background: var(--bg-secondary);
}

/* Slotted content, so these reach one level deeper than a scoped rule
   normally would - :deep is what makes that legal rather than accidental.
   The facts themselves belong to the caller; how a fact LOOKS belongs
   here, or the two readers drift again. */
.reader-details :deep(dt) {
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--text-tertiary);
  margin-bottom: 2px;
}

.reader-details :deep(dd) {
  margin: 0;
  font-size: 13px;
  overflow-wrap: anywhere;
}

/* A note that spans the row rather than sitting in a column: an error
   message or a download link is not a labelled fact. */
.reader-details :deep(.details-note) {
  grid-column: 1 / -1;
  font-size: 12px;
  color: var(--text-secondary);
}
</style>
