<script setup lang="ts">
// Which project the console is looking at, and how to change it.
//
// Its own component because it is the one thing in the rail that is not
// navigation: it does not link anywhere, it changes what every other
// link MEANS. The rail was carrying its markup, its menu, its dropdown
// styling and the switch itself alongside the menu it has nothing to do
// with.
//
// `narrow` is a PROP rather than something read off the rail, and it has
// to be: a scoped rule reaches a child component's root element and
// nothing deeper, so `.rail.narrow .picker-open` written in the parent
// would silently stop matching the moment this markup moved in here.
import { useRouter } from 'vue-router'
import { useProjectStore } from '../stores/project'
import { getIcon } from './icons'

const props = defineProps<{
  /** Icons only - the tile stands in for the whole control. */
  narrow?: boolean
}>()

const emit = defineEmits<{
  /** Somewhere was navigated to, so a drawer should dismiss itself. */
  (e: 'navigate'): void
}>()

const router = useRouter()
const projects = useProjectStore()

const open = defineModel<boolean>('open', { default: false })

// Every action here does the same three things, so they say so once.
// Named rather than written inline: prettier reflows a multi-statement
// handler attribute across lines and drops the separator, and the
// template compiler then refuses the file.
function leaveFor(path: string) {
  open.value = false
  emit('navigate')
  router.push(path)
}

async function choose(id: string) {
  // The picker closes at once rather than after the round trip, since
  // it is the press being acknowledged.
  open.value = false

  // AWAITED, which the projects page already did and this did not.
  // setProject empties the permission list before it fetches the new
  // one, so navigating on the synchronous half lands on a dashboard
  // where can() answers false for everything - the menu loses most of
  // its rows and the cards gated on a permission appear a moment later.
  //
  // Home, not the current route: the page being viewed belongs to the
  // project being left, and half of them would 404 against the new one.
  await projects.setProject(id)
  leaveFor('/')
}
</script>

<template>
  <!-- The attribute, not the class, is what the layout's click-outside
       asks about. A class is styling and gets renamed on a styling
       change, which would silently leave the menu unable to close. -->
  <div class="picker" :class="{ narrow: props.narrow }" data-project-picker>
    <div class="picker-open" @click="open = !open">
      <div class="picker-who">
        <div class="initial">
          {{ projects.currentProject?.name?.charAt(0)?.toUpperCase() || 'P' }}
        </div>
        <span v-if="!props.narrow" class="picker-name">{{ projects.contextLabel }}</span>
      </div>
      <span
        v-if="!props.narrow"
        class="picker-caret"
        aria-hidden="true"
        v-html="getIcon('chevron-down')"
      ></span>
    </div>

    <div v-if="open" class="picker-menu">
      <div
        v-for="proj in projects.projects"
        :key="proj.id"
        class="picker-row"
        :class="{ on: projects.currentProjectId === proj.id }"
        @click="choose(proj.id)"
      >
        <div class="initial initial-sm">{{ proj.name.charAt(0).toUpperCase() }}</div>
        <span>{{ proj.name }}</span>
      </div>

      <div class="picker-rule"></div>

      <div
        v-if="projects.canCreateProjects"
        class="picker-row"
        @click="leaveFor('/projects?create=1')"
      >
        <span class="picker-plus">+</span>
        <span>Create project</span>
      </div>
      <div class="picker-row" @click="leaveFor('/projects')">
        <span class="picker-glyph" v-html="getIcon('briefcase')"></span>
        <span>Manage projects</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.picker {
  position: relative;
  padding: 0 8px 8px;
}

.picker-open {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 10px;
  border: 1px solid var(--sidebar-border);
  border-radius: var(--radius);
  color: var(--sidebar-text);
  cursor: pointer;
  transition:
    background var(--transition),
    color var(--transition);
}

.picker-open:hover {
  background: var(--sidebar-hover);
  color: var(--sidebar-text-active);
}

/* Narrowed there is only the tile left, so the frame around it would be
   a box drawn around a box. */
.picker.narrow .picker-open {
  justify-content: center;
  padding: 8px;
  border: none;
}

.picker-who {
  display: flex;
  overflow: hidden;
  align-items: center;
  gap: 8px;
}

.picker-name {
  overflow: hidden;
  font-size: 13px;
  font-weight: 500;
  white-space: nowrap;
  text-overflow: ellipsis;
}

.picker-caret,
.picker-glyph {
  display: flex;
  flex-shrink: 0;
  width: 15px;
  height: 15px;
}

/* The project's first letter, standing in for a logo nobody uploads.
   Sized through a variable so the smaller one in the menu is a single
   override rather than a second copy of the rule. */
.initial {
  --tile: 24px;
  display: flex;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  width: var(--tile);
  height: var(--tile);
  border-radius: 6px;
  background: var(--primary-600);
  color: var(--text-on-primary);
  font-size: 12px;
  font-weight: 600;
}

.initial-sm {
  --tile: 20px;
  border-radius: 5px;
  font-size: 10px;
}

.picker-menu {
  position: absolute;
  top: calc(100% + 2px);
  right: 8px;
  left: 8px;
  z-index: 200;
  min-width: 200px;
  padding: 4px;
  border: 1px solid var(--border-primary);
  border-radius: var(--radius);
  background: var(--bg-popover);
  box-shadow: var(--shadow-lg);
}

/* Every line in the menu is one of these, whether it picks a project or
   does something else. They were two identical rules under two names,
   which is two places to change a padding. */
.picker-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 10px;
  border-radius: var(--radius-sm);
  color: var(--text-secondary);
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  transition:
    background var(--transition),
    color var(--transition);
}

.picker-row:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.picker-row.on {
  background: var(--bg-active);
  color: var(--accent-fg);
}

.picker-rule {
  height: 1px;
  margin: 4px 6px;
  background: var(--border-primary);
}

/* A plus sign standing in the column the other rows put a glyph in, so
   it is boxed to the same 18px rather than sized as text. */
.picker-plus {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 18px;
  height: 18px;
  font-size: 18px;
  font-weight: 400;
  line-height: 1;
}
</style>
