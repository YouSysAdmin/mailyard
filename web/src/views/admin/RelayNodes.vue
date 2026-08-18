<script setup lang="ts">
// Every relay node on the installation.
//
// Nothing here creates one. A node enrols with the shared token and
// appears pending, and what an admin does on this page is decide whether
// it may carry mail - a decision worth making, since a node in the pool
// receives the content of real messages.
//
// Two states matter and they are not the same. Status is approval. Alive
// is whether the node has reported in recently, judged by the same
// window the delivery path uses, so this page cannot say a node is fine
// while the pool is skipping it.
//
// The table and the three writes are shared with the project listing
// under Infrastructure. What is this page's alone sits above them: the
// SPF fragment, the auto-approval notice, and the fact that a tenant's
// node appears here at all.
import { computed, onMounted, ref } from 'vue'
import { relayNodesApi } from '../../api/relayNodes'
import { apiErrorMessage } from '../../api/client'
import type { RelayNode } from '../../api/types'
import { useNotificationStore } from '../../stores/notification'
import { mxRecordFor, useRelayNodeActions } from '../../composables/relayNodes'
import LoadingBlock from '../../components/LoadingBlock.vue'
import EmptyState from '../../components/EmptyState.vue'
import PageHeader from '../../components/PageHeader.vue'
import RelayNodeTable from '../../components/RelayNodeTable.vue'
import DnsRecordCard from '../../components/DnsRecordCard.vue'

const notify = useNotificationStore()

const nodes = ref<RelayNode[]>([])
const spfInclude = ref('')
const mxHosts = ref<string[]>([])
const autoApprove = ref(false)
const enabled = ref(true)
// Whether this build carries relay nodes, which is a different question
// from whether the operator switched them on. Optimistic until the
// server answers, like enabled above.
const available = ref(true)
const loading = ref(true)

// Only the platform's own nodes are this page's decision. A tenant's
// node is approved by an admin of that project, so counting theirs in
// the banner would prompt an action this page cannot take.
const pending = computed(() => nodes.value.filter((n) => n.status === 'pending' && !n.project_id))

async function load() {
  loading.value = true
  try {
    const res = await relayNodesApi.list()
    nodes.value = res.data.relay_nodes ?? []
    enabled.value = res.data.enabled ?? false
    available.value = res.data.available ?? false
    spfInclude.value = res.data.spf_include ?? ''
    mxHosts.value = res.data.mx_hosts ?? []
    autoApprove.value = res.data.auto_approve ?? false
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load relay nodes'))
  } finally {
    loading.value = false
  }
}

const { busy, approve, suspend, remove } = useRelayNodeActions(relayNodesApi, load, 'mail')

const mxRecord = computed(() => mxRecordFor(mxHosts.value))

onMounted(load)
</script>

<template>
  <div>
    <PageHeader title="Relay Nodes">
      <button class="btn" :disabled="loading" @click="load">Refresh</button>
    </PageHeader>

    <DnsRecordCard v-if="spfInclude" title="SPF" :value="spfInclude" announce="SPF fragment copied">
      Mail leaving through the pool carries your bounce address as its return path, and a receiver
      checks that domain's SPF against the address that connected. A node missing from the record
      fails SPF on your own domain, so add this to it.
    </DnsRecordCard>

    <DnsRecordCard
      v-if="mxHosts.length > 0"
      title="MX"
      :value="mxRecord"
      announce="MX records copied"
    >
      These nodes are running a mail exchanger. Publish this on the domain whose mail should reach
      them - your bounce domain above all. Until DNS points here nothing arrives, and the symptom is
      bounces that simply stop appearing, which looks exactly like nothing bouncing.
    </DnsRecordCard>

    <div v-if="autoApprove" class="card">
      <div class="card-body">
        <p class="text-sm">
          <strong>Automatic approval is on.</strong> Any machine holding the enrolment token starts
          carrying mail as soon as it enrols, without appearing here first.
        </p>
      </div>
    </div>

    <div v-else-if="pending.length > 0" class="card">
      <div class="card-body">
        <p class="text-sm">
          <strong
            >{{ pending.length }} node<span v-if="pending.length > 1">s</span> waiting for
            approval.</strong
          >
          A node in the pool receives the content of real messages to deliver. Approve only machines
          you recognise.
        </p>
      </div>
    </div>

    <div class="card">
      <div class="card-header">
        <div>
          <h2>Nodes</h2>
          <!-- What a node is, in both editions. The how-to naming the
               enrolment token moved into the empty state below, which
               only renders once the server has answered - up here it
               rendered optimistically and then vanished, explaining
               enrolment to a reader whose build has none. -->
          <p class="text-sm text-muted">
            Machines that enrolled themselves and deliver straight to recipient mail exchangers.
            They are not created here.
          </p>
        </div>
      </div>

      <LoadingBlock v-if="loading" />

      <!-- Before the not-enabled state: both answer an empty table and
           only one of them can be true. The keys named below are a boot
           failure on a community build, not a step to take. -->
      <EmptyState v-else-if="!available" title="Enterprise edition">
        <p>
          Relay nodes are not available in the community edition, which this installation runs. Mail
          goes out through the SMTP servers a project configures and through the shared pool.
        </p>
        <p>
          Setting <code>relay_nodes.enabled</code> will not turn them on - this binary refuses to
          start with it set.
        </p>
      </EmptyState>

      <!-- With the feature off no node can enrol, so an empty list is
           not "none yet" and must not read as one. -->
      <EmptyState v-else-if="!enabled" title="Relay nodes are not enabled">
        <p>
          Set <code>relay_nodes.enabled</code> and <code>relay_nodes.auto_register_token</code> in
          the server configuration, then restart. Until then no machine can enrol.
        </p>
      </EmptyState>

      <EmptyState v-else-if="nodes.length === 0" title="No relay nodes">
        <p>
          Run <code>mailyard relay</code> on a machine with outbound port 25 open and a PTR record
          matching its hostname.
        </p>
      </EmptyState>

      <RelayNodeTable
        v-else
        :nodes="nodes"
        :busy="busy"
        can-edit
        can-delete
        shows-other-projects
        @approve="approve"
        @suspend="suspend"
        @remove="remove"
      />
    </div>
  </div>
</template>
