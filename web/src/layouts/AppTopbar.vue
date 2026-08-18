<script setup lang="ts">
// The bar above the page: the drawer handle, the manual, the activity
// bell, and the account menu.
//
// Assembly, like the shell around it. The two popovers own their own
// data and their own open state - what is left here is placing them and
// forwarding the one thing neither can answer alone: a click that
// landed on the OTHER one, which has to close this one.
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { getIcon } from './icons'
import NotificationBell from '../components/NotificationBell.vue'
import AccountMenu from '../components/AccountMenu.vue'

const emit = defineEmits<{
  /** The drawer handle was used - only the layout knows what a drawer is. */
  (e: 'open-menu'): void
}>()

const router = useRouter()

const bell = ref<InstanceType<typeof NotificationBell> | null>(null)
const account = ref<InstanceType<typeof AccountMenu> | null>(null)

/**
 * Close both popovers when the click landed outside them.
 *
 * Called by the layout, which owns the one document listener for the
 * whole shell. Each popover decides for itself whether the click was
 * its own, by asking its own element rather than by matching a class
 * name - so restyling one cannot leave it unable to close.
 */
function onOutsideClick(e: MouseEvent) {
  bell.value?.closeIfOutside(e)
  account.value?.closeIfOutside(e)
}

/** Opening one closes the other - they overlap, and both sit right. */
function closeExcept(keep: 'bell' | 'account') {
  if (keep !== 'bell') bell.value?.close()
  if (keep !== 'account') account.value?.close()
}

defineExpose({ onOutsideClick })
</script>

<template>
  <header class="topbar">
    <div class="bar-left">
      <button class="drawer-handle" aria-label="Open menu" @click="emit('open-menu')">
        <span class="bar-glyph" aria-hidden="true" v-html="getIcon('menu')"></span>
      </button>
    </div>

    <div class="bar-right">
      <!-- The manual, as an icon with no label and no permission gate:
           there is no role that should be told to go read the docs and
           then not shown where they are. A plain anchor because /docs is
           served by the Go binary rather than by this SPA, and a new tab
           so looking something up costs nothing that was open. -->
      <a
        class="bar-link"
        href="/docs/"
        target="_blank"
        rel="noopener noreferrer"
        title="Documentation"
        aria-label="Documentation"
      >
        <span class="bar-glyph" v-html="getIcon('book')"></span>
      </a>

      <NotificationBell ref="bell" @opened="closeExcept('bell')" @follow="router.push($event)" />

      <AccountMenu ref="account" @opened="closeExcept('account')" @follow="router.push($event)" />
    </div>
  </header>
</template>

<style scoped>
.topbar {
  position: sticky;
  top: 0;
  z-index: 30;
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: 52px;
  padding: 0 var(--gutter);
  border-bottom: 1px solid var(--border-primary);
  background: var(--bg-primary);
  transition:
    background var(--transition-slow),
    border-color var(--transition-slow);
}

.bar-left {
  display: flex;
  align-items: center;
}

.bar-right {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-left: auto;
}

/* Absent while the rail is on screen, because then there is nothing to
   open - the whole menu is already visible beside the page. */
.drawer-handle {
  display: none;
  align-items: center;
  justify-content: center;
  padding: 6px;
  border: none;
  border-radius: var(--radius-sm);
  background: none;
  color: var(--text-tertiary);
  cursor: pointer;
  transition:
    color var(--transition),
    background var(--transition);
}

.drawer-handle:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

/* The width at which the rail stops being on screen, so from here the
   drawer is the only way to reach a page and this button is the only
   way to the drawer. The rule lives HERE and not in the layout: a
   scoped rule reaches a child component's ROOT element and nothing
   deeper, so the layout's copy of it compiled to a selector carrying
   the LAYOUT's scope id against a button carrying the topbar's, matched
   nothing, and left a narrow window with no navigation at all. */
@media (max-width: 1024px) {
  .drawer-handle {
    display: flex;
  }
}

/* The same 34px box the bell uses, so the row reads as one set of
   controls rather than as an anchor that happens to sit beside them. */
.bar-link {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 34px;
  height: 34px;
  border-radius: 8px;
  color: var(--text-secondary);
  text-decoration: none;
  transition:
    color var(--transition),
    background var(--transition);
}

.bar-link:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

/* It borrowed the rail's .nav-icon until that moved into its own
   component and took the rule with it - which is the right outcome: an
   element here should not depend on the navigation's styling to have a
   size. */
.bar-glyph {
  display: flex;
  width: 18px;
  height: 18px;
}

/* The bar gives back the page gutter on a phone. Written in the
   LAYOUT's media query once and never applied, for the reason above. */
@media (max-width: 640px) {
  .topbar {
    padding: 0 16px;
  }
}
</style>
