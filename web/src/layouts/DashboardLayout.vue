<script setup lang="ts">
// The console shell: a navigation rail, a top bar, and the routed page
// between them.
//
// Assembly and nothing else. The rail and the bar own their own state
// and their own styling - what is left here is the three things only
// the shell can know: whether the rail is narrow, whether the drawer is
// open, and what to show when the caller belongs to no project at all.
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppSidebar from './AppSidebar.vue'
import AppTopbar from './AppTopbar.vue'
import { useProjectStore } from '../stores/project'
import EmptyState from '../components/EmptyState.vue'

const COLLAPSED_KEY = 'mailyard_sidebar_collapsed'

const route = useRoute()
const router = useRouter()
const projects = useProjectStore()

const collapsed = ref(localStorage.getItem(COLLAPSED_KEY) === 'true')
const drawerOpen = ref(false)

// Bound to BOTH sidebars: the rail and the drawer are one component
// rendered twice, and an open switcher must not survive as two.
const switcherOpen = ref(false)

const topbar = ref<InstanceType<typeof AppTopbar> | null>(null)

function toggleCollapsed() {
  collapsed.value = !collapsed.value
  localStorage.setItem(COLLAPSED_KEY, String(collapsed.value))
}

/**
 * Dismiss anything open when the click landed elsewhere.
 *
 * One document listener for the whole shell rather than one per menu:
 * they all close on the same event, and three listeners racing to
 * decide whether the click was theirs is how one of them ends up
 * closing the menu another just opened. The topbar is asked about its
 * own menus because it owns them.
 */
function onDocumentClick(e: MouseEvent) {
  if (switcherOpen.value && !(e.target as Element)?.closest?.('[data-project-picker]')) {
    switcherOpen.value = false
  }

  topbar.value?.onOutsideClick(e)
}

onMounted(() => {
  document.addEventListener('click', onDocumentClick)
  projects.fetchProjects()
})

onBeforeUnmount(() => document.removeEventListener('click', onDocumentClick))

// A route that declares a project permission is a route ABOUT a
// project. The rest - the projects list, a profile, the platform admin
// pages - are exactly what a caller with no project has to reach, and
// the server treats them the same way: permOn refuses, everything else
// carries on.
const routeNeedsProject = computed(() => typeof route.meta.permission === 'string')

function createProject() {
  drawerOpen.value = false
  router.push('/projects?create=1')
}
</script>

<template>
  <div class="layout" :class="{ 'sidebar-collapsed': collapsed }">
    <AppSidebar
      v-model:switcher-open="switcherOpen"
      :collapsed="collapsed"
      @toggle="toggleCollapsed"
    />

    <div class="main-wrapper">
      <AppTopbar ref="topbar" @open-menu="drawerOpen = true" />
      <main class="main-content">
        <!--
          An account can belong to nothing at all - nothing is minted
          for a new user. Without this the console rendered every
          project-scoped page against no project and filled with
          failed requests.

          It stands in only for the pages that NEED a project. The
          projects list is what answers this state, and swallowing it
          left the message above as the whole product: a dead end
          telling people to ask an administrator, on a page whose own
          button would have fixed it.
        -->
        <div v-if="projects.hasNoProjects && routeNeedsProject" class="card no-project">
          <EmptyState title="No project yet">
            <p>
              Everything in Mailyard belongs to a project, and your account is not a member of one
              yet.
            </p>
            <!-- Without the button this card is the dead end its own
                 comment above warns about, so when creation is closed
                 it has to say what DOES happen next rather than
                 leaving a paragraph with nothing under it. -->
            <button
              v-if="projects.canCreateProjects"
              class="btn btn-primary mt-4"
              @click="createProject"
            >
              New project
            </button>
            <p v-else class="mt-4">
              Projects are created by an administrator on this installation. Ask to be invited to
              one.
            </p>
          </EmptyState>
        </div>
        <router-view v-else />
      </main>
    </div>

    <!-- Mobile sidebar overlay -->
    <Transition name="overlay-fade">
      <div v-if="drawerOpen" class="sidebar-overlay" @click="drawerOpen = false" />
    </Transition>

    <!-- Mobile sidebar -->
    <Transition name="sidebar-slide">
      <AppSidebar
        v-if="drawerOpen"
        v-model:switcher-open="switcherOpen"
        drawer
        @close="drawerOpen = false"
        @navigate="drawerOpen = false"
      />
    </Transition>
  </div>
</template>

<style scoped>
.layout {
  display: flex;
  min-height: 100vh;
  background: var(--bg-secondary);
}

.main-wrapper {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-height: 100vh;
  /* A flex item defaults to min-width: auto, so it will not shrink
     below its content. Without this a table wider than the viewport
     props the whole layout open and the PAGE scrolls sideways -
     taking the rail and topbar with it - instead of the table
     scrolling inside its own .table-wrapper. */
  min-width: 0;
  margin-left: 240px;
  transition: margin-left var(--transition-slow);
}

.sidebar-collapsed .main-wrapper {
  margin-left: 64px;
}

.main-content {
  flex: 1;
  padding: var(--gutter);
}

/* Width and placement only. What is INSIDE is .empty-state, the same
   block every list uses when it has nothing - this card had its own
   headings, colours and no padding at all, which is why it read as
   cramped next to every other empty screen in the console. */
.no-project {
  max-width: 34rem;
  margin: 48px auto;
}

.sidebar-overlay {
  position: fixed;
  inset: 0;
  z-index: 35;
  background: var(--overlay);
  backdrop-filter: blur(4px);
}

/* Leaving is quicker than arriving in both of these: a dismissal should
   be out of the way by the time the eye follows it, where an arrival is
   worth watching. */
.overlay-fade-enter-active {
  transition: opacity 200ms ease;
}

.overlay-fade-leave-active {
  transition: opacity 150ms ease;
}

.overlay-fade-enter-from,
.overlay-fade-leave-to {
  opacity: 0;
}

.sidebar-slide-enter-active {
  transition: transform 200ms ease;
}

.sidebar-slide-leave-active {
  transition: transform 150ms ease;
}

/* These reach the drawer because a transition class lands on the child
   component's ROOT element, which carries this scope id as well as its
   own. Nothing deeper inside it can be styled from here. */
.sidebar-slide-enter-from,
.sidebar-slide-leave-to {
  transform: translateX(-100%);
}

/* The rail is gone from here down - it hides itself, and the topbar
   shows the button that opens the drawer instead. All this side of it
   is the gutter the rail was holding. */
@media (max-width: 1024px) {
  .main-wrapper {
    margin-left: 0 !important;
  }
}

@media (max-width: 640px) {
  .main-content {
    box-sizing: border-box;
    width: 100%;
    max-width: 100vw;
    min-width: 0;
    overflow-x: hidden;
    padding: 20px 16px;
  }
}
</style>
