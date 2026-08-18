<script setup lang="ts">
// The account page: who you are, and every way of getting in.
//
// Assembly. Each way in owns its own card - password, authenticator,
// passkeys, sessions - because each has its own loading, its own dialog
// and its own failure modes, and holding all four here was 780 lines
// where the only thing they shared was the grid they sit in.
//
// The one edge between them is real and stays: changing a password
// signs every other session out, so the session card is told to reload.
import { computed, ref } from 'vue'
import { authApi } from '../../api/auth'
import { useAuthStore } from '../../stores/auth'
import { formatDate } from '../../composables/formatDate'
import PageHeader from '../../components/PageHeader.vue'
import AccountPassword from './AccountPassword.vue'
import AccountTotp from './AccountTotp.vue'
import AccountPasskeys from './AccountPasskeys.vue'
import AccountSessions from './AccountSessions.vue'
import Notice from '../../components/Notice.vue'

const auth = useAuthStore()

const user = computed(() => auth.user)

// Everything below - password, 2FA, passkeys - belongs to the account
// unless an identity provider owns it.
const local = computed(() => user.value?.account_type !== 2)

const sessionCard = ref<InstanceType<typeof AccountSessions> | null>(null)

const facts = computed(() => {
  const u = user.value
  if (!u) return []

  return [
    { label: 'Created', value: formatDate(u.created_at) },
    { label: 'Last signed in', value: formatDate(u.last_login_at) },
  ]
})

// The cached profile renders first, then this corrects it - the account
// type and the last login are both things the server knows better.
async function refresh() {
  try {
    const res = await authApi.me()
    if (res.data.user) auth.setUser(res.data.user)
  } catch {
    // The cached profile is still on screen and still broadly right.
  }
}

void refresh()
</script>

<template>
  <div>
    <PageHeader title="Your account" />

    <div class="grid">
      <div class="card">
        <div class="card-header">
          <h2>{{ user?.email ?? 'Account' }}</h2>
          <span v-if="user?.admin" class="badge badge-info">administrator</span>
        </div>

        <div class="card-body">
          <template v-if="user">
            <dl class="facts">
              <dt>Signs in with</dt>
              <dd>{{ local ? 'A password on this installation' : 'An identity provider' }}</dd>

              <template v-for="f in facts" :key="f.label">
                <dt>{{ f.label }}</dt>
                <dd>{{ f.value }}</dd>
              </template>
            </dl>
          </template>

          <p v-else class="text-muted">
            Authentication is off on this server, so there is no account to show.
          </p>
        </div>
      </div>

      <AccountTotp v-if="user && local" />

      <AccountPassword v-if="user && local" :email="user.email" @changed="sessionCard?.reload()" />

      <!-- OIDC only. Every other account is an ordinary one, however it
           was created, and manages its own credentials below. -->
      <Notice v-if="user && !local" class="wide">
        <p>
          This account signs in through an identity provider. Its password, two-factor
          authentication and passkeys are managed there.
        </p>
      </Notice>

      <AccountPasskeys v-if="user && local" :email="user.email" class="wide" />

      <AccountSessions ref="sessionCard" class="wide" />
    </div>
  </div>
</template>

<style scoped>
/* The small cards pair up on a wide screen and the two tables get the
   full width - at a single 560px column the session table's six columns
   wrapped every timestamp onto three lines while a third of the
   viewport sat empty. auto-fit collapses to one column below ~740px. */
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(340px, 1fr));
  /* Stretch, so two cards in a row share a height and their bottom
     edges line up. */
  align-items: stretch;
  gap: 16px;
  max-width: 1100px;
}

/* Grid items default to min-width: auto, so they refuse to shrink below
   their content. Without this a table's intrinsic width props the page
   open and the viewport scrolls sideways, instead of the table
   scrolling inside its own wrapper. */
.grid > * {
  min-width: 0;
}

.wide {
  grid-column: 1 / -1;
}

.facts {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 10px 16px;
  margin: 0;
  font-size: 14px;
}

.facts dt {
  color: var(--text-tertiary);
  font-size: 13px;
  font-weight: 500;
}

.facts dd {
  margin: 0;
  color: var(--text-primary);
}
</style>
