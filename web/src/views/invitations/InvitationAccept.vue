<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { projectApi } from '../../api/projects'
import { apiErrorMessage } from '../../api/client'
import type { Project } from '../../api/types'
import { useProjectStore } from '../../stores/project'
import { useAuthStore } from '../../stores/auth'

const route = useRoute()
const router = useRouter()
const projStore = useProjectStore()
const auth = useAuthStore()

const token = computed(() => (route.query.token as string) || '')
const status = ref<'idle' | 'accepting' | 'declining' | 'success' | 'declined' | 'error'>('idle')
const message = ref('')
const joined = ref<Project | null>(null)

// Accepting requires a session, so somebody arriving with no account is
// sent to sign in first, carrying the token.
//
// Leaving it to the 401 sends them to the login page with the token
// dropped on the way, so they have to go back to their email and click
// the link again. That is tolerable while
// auto-provisioning existed. It is the entire first run now, because an
// invitation is the only way into a project.
async function accept() {
  status.value = 'accepting'
  try {
    const res = await projectApi.acceptInvitation(token.value)
    joined.value = res.data.project
    message.value = joined.value
      ? `You joined the project "${joined.value.name}".`
      : 'Invitation accepted.'
    status.value = 'success'
    if (joined.value) {
      await projStore.fetchProjects(true)
      await projStore.setProject(joined.value.id)
    }
  } catch (err) {
    // The backend answers 404 for unknown or already used tokens, 410
    // for expired ones, and 403 when the signed-in email does not match.
    message.value = apiErrorMessage(err, 'Failed to accept invitation')
    status.value = 'error'
  }
}

// To the login page, carrying the token so whichever leg the person uses
// comes back here. A router push rather than a document boundary: no
// session exists yet, so there is no state to shed.
function signInToAccept() {
  router.push({ name: 'login', query: { invite: token.value } })
}

async function decline() {
  status.value = 'declining'
  try {
    await projectApi.declineInvitation(token.value)
    message.value = 'Invitation declined.'
    status.value = 'declined'
  } catch (err) {
    message.value = apiErrorMessage(err, 'Failed to decline invitation')
    status.value = 'error'
  }
}

function goToDashboard() {
  router.push('/')
}
</script>

<template>
  <div class="invitation-page">
    <div class="invitation-card">
      <h1>Project Invitation</h1>

      <template v-if="!token">
        <p class="error">Missing invitation token.</p>
        <router-link to="/" class="btn btn-secondary">Go to dashboard</router-link>
      </template>

      <template v-else-if="status === 'idle' && !auth.isAuthenticated">
        <p>You have been invited to join a project. Sign in to accept it.</p>
        <p class="muted">
          The invitation is bound to the email address it was issued to, so sign in with that
          account - through your identity provider if that is how you sign in.
        </p>
        <div class="actions">
          <button class="btn btn-primary" @click="signInToAccept">Sign in to accept</button>
        </div>
      </template>

      <template v-else-if="status === 'idle'">
        <p>You have been invited to join a project. Accept the invitation below.</p>
        <p class="muted">
          The invitation is bound to the email address it was issued to - make sure you are signed
          in with that account.
        </p>
        <div class="actions">
          <button class="btn btn-primary" @click="accept">Accept Invitation</button>
          <button class="btn btn-secondary" @click="decline">Decline</button>
        </div>
      </template>

      <template v-else-if="status === 'accepting' || status === 'declining'">
        <div class="spinner centered"></div>
        <p class="muted">{{ status === 'accepting' ? 'Accepting' : 'Declining' }} invitation...</p>
      </template>

      <template v-else-if="status === 'success'">
        <p class="success">{{ message }}</p>
        <p v-if="joined" class="muted">It is now your active project.</p>
        <button class="btn btn-primary" @click="goToDashboard">Go to dashboard</button>
      </template>

      <template v-else-if="status === 'declined'">
        <p>{{ message }}</p>
        <button class="btn btn-secondary" @click="goToDashboard">Go to dashboard</button>
      </template>

      <template v-else>
        <p class="error">{{ message }}</p>
        <div class="actions">
          <button class="btn btn-secondary" @click="accept">Try again</button>
          <router-link to="/" class="btn btn-secondary">Go to dashboard</router-link>
        </div>
      </template>
    </div>
  </div>
</template>

<style scoped>
.centered {
  margin: 12px auto;
}

.invitation-page {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 100vh;
  background: var(--bg-secondary);
  padding: 24px;
}
.invitation-card {
  background: var(--bg-primary);
  border: 1px solid var(--border-primary);
  border-radius: 8px;
  padding: 32px;
  max-width: 440px;
  width: 100%;
  text-align: center;
}
.invitation-card h1 {
  font-size: 20px;
  margin: 0 0 16px;
  color: var(--text-primary);
}
.invitation-card p {
  font-size: 14px;
  color: var(--text-secondary);
  margin: 8px 0 20px;
}
.muted {
  color: var(--text-muted);
}
.success {
  color: var(--success-fg);
  font-weight: 500;
}
.error {
  color: var(--danger-fg);
}
.actions {
  display: flex;
  gap: 8px;
  justify-content: center;
}
</style>
