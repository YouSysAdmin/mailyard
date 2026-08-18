<script setup lang="ts">
// The last few messages, as a way into the log.
//
// Deliberately not a filterable table: it is a doorway, and the row
// opens the message. Everything a person would want to narrow by lives
// on the email log itself.
import { useRouter } from 'vue-router'
import type { Email } from '../../api/types'
import { formatDate } from '../../composables/formatDate'
import EmptyState from '../../components/EmptyState.vue'
import StatusBadge from '../../components/StatusBadge.vue'

defineProps<{ emails: Email[] }>()

const router = useRouter()
</script>

<template>
  <div class="card">
    <div class="card-header">
      <h2>Recent Emails</h2>
      <button class="btn btn-secondary btn-sm" @click="router.push('/emails')">View All</button>
    </div>

    <EmptyState
      v-if="emails.length === 0"
      title="No emails yet"
      text="Emails sent through the API or console will appear here."
    />

    <div v-else class="table-wrapper">
      <table>
        <thead>
          <tr>
            <th>Subject</th>
            <th>Recipients</th>
            <th>Status</th>
            <th>Date</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="email in emails"
            :key="email.id"
            class="row-clickable"
            @click="router.push(`/emails/${email.id}`)"
          >
            <td>{{ email.subject }}</td>
            <td>{{ email.recipients.join(', ') }}</td>
            <td><StatusBadge :status="email.status" scope="email" /></td>
            <td>{{ formatDate(email.created_at) }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
