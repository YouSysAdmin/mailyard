<script setup lang="ts">
// Who is signed in, and the three things they can do about it: open
// their profile, choose a colour mode, sign out.
//
// Its own component for the same reason as the bell beside it - a
// popover owning its own open state. It also owns the sign-out, which
// is a SESSION boundary and not an ordinary navigation, so keeping it
// here puts the whole of that act in one place.
import { computed, ref } from 'vue'
import { useAuthStore } from '../stores/auth'
import { useThemeStore, type ThemeMode } from '../stores/theme'
import { beginLeaving, leaveConsole } from '../composables/session'

const emit = defineEmits<{
  /** A menu entry wants a page. */
  (e: 'follow', path: string): void
  /** Opened, so whatever else is open in the bar should close. */
  (e: 'opened'): void
}>()

const auth = useAuthStore()
const theme = useThemeStore()

const root = ref<HTMLElement | null>(null)
const open = ref(false)

const MODES: ThemeMode[] = ['light', 'dark', 'system']

/**
 * What to call the person.
 *
 * With auth disabled there is no account to name, and "User" over an
 * empty avatar reads as a broken profile rather than as the local
 * single-operator mode it actually is.
 */
const label = computed(() => (auth.authDisabled ? 'Local admin' : (auth.user?.email ?? 'User')))
const initial = computed(() => label.value.charAt(0).toUpperCase())

function toggle() {
  open.value = !open.value
  if (open.value) emit('opened')
}

function go(path: string) {
  open.value = false
  emit('follow', path)
}

async function signOut() {
  // Before the request, not after: clearing the cookie makes every
  // in-flight call answer 401, and the interceptor turns a 401 into
  // "your session has expired" - which is how pressing Logout used to
  // land on an error page. What the interceptor lacks is the INTENT.
  beginLeaving()
  await auth.logout()
  leaveConsole()
}

/** Close when the click landed outside this menu. */
function closeIfOutside(e: MouseEvent) {
  if (!root.value?.contains(e.target as Node)) open.value = false
}

defineExpose({ close: () => (open.value = false), closeIfOutside })
</script>

<template>
  <div ref="root" class="account" @click="toggle">
    <div class="face">{{ initial }}</div>
    <div class="who">
      <div class="who-name">{{ label }}</div>
    </div>
    <svg
      width="14"
      height="14"
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      stroke-width="1.5"
      stroke-linecap="round"
      stroke-linejoin="round"
    >
      <path d="M4 6l4 4 4-4" />
    </svg>

    <!-- Every entry stops the click: the wrapper toggles the menu, so
         without this choosing something also reopens it. -->
    <div v-if="open" class="sheet">
      <header class="sheet-head">
        <span class="face face-lg">{{ initial }}</span>
        <div class="who">
          <div class="sheet-name">{{ label }}</div>
          <div v-if="auth.user?.admin" class="sheet-role">administrator</div>
        </div>
      </header>

      <div class="rule" />

      <!-- Nothing account-shaped when auth is off: there is no account
           to edit and no session to end. -->
      <a v-if="!auth.authDisabled" class="entry" @click.stop="go('/profile')">
        <svg
          width="15"
          height="15"
          viewBox="0 0 16 16"
          fill="none"
          stroke="currentColor"
          stroke-width="1.5"
          stroke-linecap="round"
          stroke-linejoin="round"
        >
          <path d="M12.67 14v-1.33A2.67 2.67 0 0010 10H6a2.67 2.67 0 00-2.67 2.67V14" />
          <circle cx="8" cy="5.33" r="2.67" />
        </svg>
        My profile
      </a>

      <div class="mode">
        <span class="mode-label">Theme</span>
        <div class="modes">
          <button
            v-for="m in MODES"
            :key="m"
            class="mode-btn"
            :class="{ on: theme.mode === m }"
            :title="m.charAt(0).toUpperCase() + m.slice(1)"
            @click.stop="theme.setMode(m)"
          >
            <svg
              v-if="m === 'light'"
              width="14"
              height="14"
              viewBox="0 0 16 16"
              fill="none"
              stroke="currentColor"
              stroke-width="1.5"
              stroke-linecap="round"
            >
              <circle cx="8" cy="8" r="3" />
              <path
                d="M8 1v2M8 13v2M1 8h2M13 8h2M3.05 3.05l1.41 1.41M11.54 11.54l1.41 1.41M3.05 12.95l1.41-1.41M11.54 4.46l1.41-1.41"
              />
            </svg>
            <svg
              v-else-if="m === 'dark'"
              width="14"
              height="14"
              viewBox="0 0 16 16"
              fill="none"
              stroke="currentColor"
              stroke-width="1.5"
              stroke-linecap="round"
              stroke-linejoin="round"
            >
              <path d="M14 9.5A6.5 6.5 0 016.5 2 6.5 6.5 0 1014 9.5z" />
            </svg>
            <svg
              v-else
              width="14"
              height="14"
              viewBox="0 0 16 16"
              fill="none"
              stroke="currentColor"
              stroke-width="1.5"
            >
              <rect x="2" y="3" width="12" height="10" rx="1.5" />
              <path d="M2 5.5h12" />
            </svg>
          </button>
        </div>
      </div>

      <template v-if="!auth.authDisabled">
        <div class="rule" />
        <a class="entry leaving" @click.stop="signOut">
          <svg
            width="15"
            height="15"
            viewBox="0 0 16 16"
            fill="none"
            stroke="currentColor"
            stroke-width="1.5"
            stroke-linecap="round"
            stroke-linejoin="round"
          >
            <path d="M6 14H3.33A1.33 1.33 0 012 12.67V3.33A1.33 1.33 0 013.33 2H6" />
            <path d="M10.67 11.33L14 8l-3.33-3.33M14 8H6" />
          </svg>
          Sign out
        </a>
      </template>
    </div>
  </div>
</template>

<style scoped>
.account {
  position: relative;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 6px 12px;
  border-radius: var(--radius);
  color: var(--text-secondary);
  cursor: pointer;
  user-select: none;
  transition: background var(--transition);
}

.account:hover {
  background: var(--bg-hover);
}

/* The initial, standing in for a picture nobody uploads. Round rather
   than the project tile's square, because one is a person and the other
   is a place. */
.face {
  --size: 32px;
  display: flex;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  width: var(--size);
  height: var(--size);
  border-radius: 50%;
  background: var(--primary-600);
  color: var(--text-on-primary);
  font-size: 13px;
  font-weight: 600;
}

.face-lg {
  --size: 36px;
  font-size: 15px;
}

.who {
  overflow: hidden;
}

.who-name {
  overflow: hidden;
  max-width: 180px;
  color: var(--text-primary);
  font-size: 13px;
  font-weight: 600;
  white-space: nowrap;
  text-overflow: ellipsis;
}

.sheet {
  position: absolute;
  top: calc(100% + 6px);
  right: 0;
  z-index: 50;
  overflow: hidden;
  width: 280px;
  border: 1px solid var(--border-primary);
  border-radius: var(--radius-lg);
  background: var(--bg-popover);
  box-shadow: var(--shadow-lg);
}

.sheet-head {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 14px 16px;
}

.sheet-name {
  overflow: hidden;
  color: var(--text-primary);
  font-size: 14px;
  font-weight: 600;
  white-space: nowrap;
  text-overflow: ellipsis;
}

.sheet-role {
  overflow: hidden;
  color: var(--text-muted);
  font-size: 12px;
  white-space: nowrap;
  text-overflow: ellipsis;
}

.rule {
  height: 1px;
  background: var(--border-primary);
}

.entry {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 16px;
  color: var(--text-secondary);
  font-size: 14px;
  text-decoration: none;
  cursor: pointer;
  transition:
    background var(--transition),
    color var(--transition);
}

.entry:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

/* Sign out is the one entry that ends something, so it is the one
   entry that carries a colour. */
.entry.leaving {
  color: var(--danger-fg);
}

.entry.leaving:hover {
  background: var(--danger-50);
  color: var(--danger-700);
}

.mode {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 16px;
}

.mode-label {
  color: var(--text-muted);
  font-size: 13px;
  font-weight: 500;
}

.modes {
  display: flex;
  gap: 2px;
  padding: 2px;
  /* Same carve-out as .tabs - the chosen chip was distinguished only by
     a shadow, which is now none. */
  border: 1px solid var(--border-primary);
  border-radius: var(--radius-sm);
  background: var(--bg-tertiary);
}

.mode-btn {
  padding: 4px 10px;
  border: 1px solid transparent;
  border-radius: 4px;
  background: transparent;
  color: var(--text-tertiary);
  font-family: inherit;
  cursor: pointer;
  transition:
    color var(--transition),
    background-color var(--transition),
    border-color var(--transition);
}

.mode-btn:hover {
  color: var(--text-primary);
}

.mode-btn.on {
  border-color: var(--border-primary);
  background: var(--bg-primary);
  color: var(--text-primary);
}

/* A phone has no room for an address beside the controls. The bar keeps
   the avatar and the caret, which is enough to find this menu. */
@media (max-width: 640px) {
  .who-name {
    display: none;
  }
}
</style>
