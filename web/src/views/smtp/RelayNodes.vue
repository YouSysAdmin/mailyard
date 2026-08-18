<script setup lang="ts">
// This project's own relay nodes.
//
// A node is a machine the project runs that delivers straight to
// recipient mail exchangers from its own address. Nothing here creates
// one: it enrols itself with an API key holding relay:write and appears
// below as pending, and what this page does is decide whether it may
// carry the project's mail.
//
// Two states and they are not the same. Status is approval. Alive is
// whether the node has reported recently, judged by the same window the
// delivery path uses - so this page cannot claim a node is fine while
// sending has already stopped using it.
//
// The table and the three writes are shared with the platform listing
// under Admin, which shows the same rows to a different reader. What is
// genuinely this page's is above them: the MX record for THIS project,
// and empty states worded for somebody who cannot edit the server
// configuration.
import { computed, onMounted, ref } from 'vue'
import { myRelayNodesApi } from '../../api/relayNodes'
import { apiErrorMessage } from '../../api/client'
import type { RelayNode } from '../../api/types'
import { useNotificationStore } from '../../stores/notification'
import { useProjectStore } from '../../stores/project'
import { mxRecordFor, useRelayNodeActions } from '../../composables/relayNodes'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import PageHeader from '../../components/PageHeader.vue'
import RelayNodeTable from '../../components/RelayNodeTable.vue'
import DnsRecordCard from '../../components/DnsRecordCard.vue'

const notify = useNotificationStore()
const projStore = useProjectStore()

const nodes = ref<RelayNode[]>([])
const mxHosts = ref<string[]>([])
const enabled = ref(true)
// Whether this build carries relay nodes, which is a different question
// from whether the operator switched them on. Optimistic until the
// server answers, like enabled above: claiming a feature is missing
// while the page is still loading is the one wrong answer here.
const available = ref(true)
const loading = ref(true)

// smtp:write, matching the server gate on these routes. Not a
// store-level "is project admin" flag - that shadows the local canEdit
// with a wider tier, and the same name then means two different things
// depending on the file.
const canEdit = computed(() => projStore.can('smtp:write'))
// Removing a node is smtp:delete on the server, unlike approving or
// suspending one.
const canDelete = computed(() => projStore.can('smtp:delete'))
const pending = computed(() => nodes.value.filter((n) => n.status === 'pending'))

async function load() {
  loading.value = true
  try {
    const res = await myRelayNodesApi.list()
    nodes.value = res.data.relay_nodes ?? []
    mxHosts.value = res.data.mx_hosts ?? []
    enabled.value = res.data.enabled ?? false
    available.value = res.data.available ?? false
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load relay nodes'))
  } finally {
    loading.value = false
  }
}

const { busy, approve, suspend, remove } = useRelayNodeActions(
  myRelayNodesApi,
  load,
  "this project's mail",
)

// A node that receives is inert until DNS points at it.
const mxRecord = computed(() => mxRecordFor(mxHosts.value))

onMounted(load)
</script>

<template>
  <div>
    <PageHeader title="Relay Nodes">
      <button class="btn" :disabled="loading" @click="load">Refresh</button>
    </PageHeader>

    <DnsRecordCard
      v-if="mxHosts.length > 0"
      title="MX"
      :value="mxRecord"
      announce="MX records copied"
    >
      These nodes are running a mail exchanger for this project. Publish this on the domain whose
      mail should reach them - your bounce domain above all. Until DNS points here nothing arrives,
      and the symptom is bounces that simply stop appearing, which looks exactly like nothing
      bouncing.
    </DnsRecordCard>

    <div v-if="pending.length > 0 && canEdit" class="card">
      <div class="card-body">
        <p class="text-sm">
          <strong
            >{{ pending.length }} node<span v-if="pending.length > 1">s</span> waiting for
            approval.</strong
          >
          A node receives the content of this project's outbound mail in order to deliver it.
          Approve only machines you recognise.
        </p>
      </div>
    </div>

    <div class="card">
      <div class="card-header">
        <div>
          <h2>Your sending machines</h2>
          <!-- What a node is, in both editions. The how-TO moved down
               into the empty state, which only renders once the server
               has answered - up here it would render optimistically and
               then vanish, so a community reader saw a command this
               binary does not have, for as long as the request took. -->
          <p class="text-sm text-muted">
            A relay node delivers straight to recipients from its own address, so SPF, reverse DNS
            and reputation are yours.
          </p>
        </div>
      </div>

      <LoadingBlock v-if="loading" />

      <!-- Before the not-enabled state: both answer an empty table and
           only one of them can be true. -->
      <EmptyState v-else-if="!available" title="Enterprise edition">
        <p>
          Relay nodes are not available in the community edition, which this installation runs. Your
          mail goes out through the SMTP servers configured for this project and through the
          platform pool.
        </p>
      </EmptyState>

      <!-- With the feature off no node can enrol, so an empty list is
           not "none yet" and must not read as one.

           Named as the OPERATOR's switch, not as a config key to go and
           set. This page belongs to a project member, who cannot edit
           the server configuration - and the key the admin page names
           here, relay_nodes.auto_register_token, is the platform's own
           enrolment secret and has nothing to do with a project node,
           which enrols with an API key. -->
      <EmptyState v-else-if="!enabled" title="Relay nodes are not enabled">
        <p>
          This installation has relay nodes turned off, so no machine can enrol. Ask whoever runs it
          to enable them.
        </p>
      </EmptyState>

      <EmptyState v-else-if="nodes.length === 0" title="No relay nodes">
        <p>
          This project delivers through its configured SMTP servers, or through the platform pool if
          it has none.
        </p>
        <!-- The how-to lives here, on the one branch that has both
             answered and said yes. -->
        <p>
          Run <code>mailyard relay</code> on a host with outbound port 25 open, pointing it at an
          API key holding <code>relay:write</code>. It appears here once it enrols.
        </p>
      </EmptyState>

      <RelayNodeTable
        v-else
        :nodes="nodes"
        :busy="busy"
        :can-edit="canEdit"
        :can-delete="canDelete"
        @approve="approve"
        @suspend="suspend"
        @remove="remove"
      />
    </div>
  </div>
</template>
