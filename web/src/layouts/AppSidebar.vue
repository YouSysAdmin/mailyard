<script setup lang="ts">
// The navigation rail: the brand, the grouped menu, and a slot in
// between where the project switcher goes.
//
// ONE component for both places it appears - the fixed rail on a wide
// screen and the drawer that slides in on a narrow one. It used to be
// two copies of the same 130 lines in the layout, differing only in
// whether labels were hidden, whether the header button collapsed or
// closed, and whether following a link dismissed anything. Those three
// are props and events now, which is what stops the copies drifting -
// and they had: a change to one was regularly a change to one.
//
// The switcher is not navigation - it does not link anywhere, it changes
// what every link here MEANS - so it is its own component and this file
// only says where it sits and how wide it is.
import { useRouter } from 'vue-router'
import { useNavigation } from './navigation'
import { getIcon } from './icons'
import BrandMark from '../components/BrandMark.vue'
import ProjectSwitcher from './ProjectSwitcher.vue'

const props = defineProps<{
  /** Icons only, no labels. Ignored in a drawer, which is never narrow. */
  collapsed?: boolean

  /** Rendered as the slide-in drawer rather than the fixed rail. */
  drawer?: boolean
}>()

const emit = defineEmits<{
  /** The collapse control was used - the rail only. */
  (e: 'toggle'): void
  /** The close control was used - the drawer only. */
  (e: 'close'): void
  /** Somewhere was navigated to, so a drawer should dismiss itself. */
  (e: 'navigate'): void
}>()

const router = useRouter()
const { groups, open, toggleGroup, hrefFor, isCurrent } = useNavigation()

// A drawer covers the screen, so there is nothing to save by hiding
// labels - collapsing is a property of the rail alone.
const narrow = () => props.collapsed && !props.drawer

const switcherOpen = defineModel<boolean>('switcherOpen', { default: false })

function goHome() {
  router.push('/')
}
</script>

<template>
  <aside class="rail" :class="{ narrow: narrow(), drawer }">
    <div class="rail-head">
      <BrandMark class="mark" :size="34" @click="goHome" />
      <span class="wordmark" @click="goHome">Mailyard</span>

      <!-- One button, two jobs: a drawer is dismissed, a rail is
           narrowed. The glyph says which. -->
      <button
        v-if="drawer"
        class="head-btn"
        title="Close menu"
        aria-label="Close menu"
        @click="emit('close')"
      >
        <span class="glyph" aria-hidden="true" v-html="getIcon('x')"></span>
      </button>
      <button
        v-else
        class="head-btn"
        :title="collapsed ? 'Expand sidebar' : 'Collapse sidebar'"
        :aria-label="collapsed ? 'Expand sidebar' : 'Collapse sidebar'"
        @click="emit('toggle')"
      >
        <span
          class="glyph"
          aria-hidden="true"
          v-html="getIcon(collapsed ? 'panel-open' : 'panel-shut')"
        ></span>
      </button>
    </div>

    <ProjectSwitcher v-model:open="switcherOpen" :narrow="narrow()" @navigate="emit('navigate')" />

    <nav class="menu">
      <div v-for="group in groups" :key="group.id" class="menu-group">
        <!-- A collapsed rail has no room for a heading, and its groups
             are always expanded - there is nothing left to fold. -->
        <button
          v-if="!narrow()"
          class="group-head"
          :aria-expanded="open[group.id]"
          @click="toggleGroup(group.id)"
        >
          <span>{{ group.title }}</span>
          <span
            class="group-caret"
            :class="{ shut: !open[group.id] }"
            aria-hidden="true"
            v-html="getIcon('chevron-down')"
          ></span>
        </button>

        <div v-show="narrow() || open[group.id]">
          <template v-for="entry in group.entries" :key="entry.label">
            <router-link
              class="link"
              :class="{ here: isCurrent(entry) }"
              :title="narrow() ? entry.label : ''"
              :to="hrefFor(entry)"
              @click="emit('navigate')"
            >
              <span class="glyph" v-html="getIcon(entry.icon)"></span>
              <span v-if="!narrow()" class="text">{{ entry.label }}</span>
            </router-link>

            <router-link
              v-for="child in entry.children"
              :key="child.path"
              class="link nested"
              :class="{ here: isCurrent(child) }"
              :title="narrow() ? child.label : ''"
              :to="hrefFor(child)"
              @click="emit('navigate')"
            >
              <span class="glyph" v-html="getIcon(child.icon)"></span>
              <span v-if="!narrow()" class="text">{{ child.label }}</span>
            </router-link>
          </template>
        </div>
      </div>
    </nav>
  </aside>
</template>

<style scoped>
.rail {
  position: fixed;
  inset: 0 auto 0 0;
  z-index: 40;
  display: flex;
  flex-direction: column;
  width: 240px;
  background: var(--bg-sidebar);
  /* In light mode --bg-sidebar equals the page background, so without
     this edge the rail has no boundary at all. */
  border-right: 1px solid var(--sidebar-border);
  transition: width var(--transition-slow);
}

/* Icons only. The width is the whole modifier - everything that has to
   disappear at this width says so beside its own rule, so reading one
   element tells you both of its states. */
.rail.narrow {
  width: 64px;
}

/* Over the rail rather than beside it, since at this width the rail is
   not on screen at all. */
.rail.drawer {
  z-index: 45;
}

/* Below this the rail is replaced by the drawer, and the topbar's menu
   button is what opens it. The rule lives HERE and not in the layout:
   a scoped rule reaches a child component's ROOT element and nothing
   deeper, so a parent hiding a child's insides compiles to a selector
   that matches nothing. */
@media (max-width: 1024px) {
  .rail:not(.drawer) {
    display: none;
  }
}

.rail-head {
  position: relative;
  display: flex;
  flex-shrink: 0;
  align-items: center;
  gap: 10px;
  padding: 16px 14px 12px;
}

.mark {
  flex-shrink: 0;
  /* The mark is a line drawing, so it takes the rail's text colour and
     the accent shows through only on the letter. No radius: that was
     for the raster tile this replaced. */
  color: var(--sidebar-text-active);
  cursor: pointer;
}

.wordmark {
  overflow: hidden;
  color: var(--sidebar-text-active);
  font-size: 14px;
  font-weight: 600;
  white-space: nowrap;
  cursor: pointer;
  transition: opacity var(--transition-slow);
}

.rail.narrow .wordmark {
  width: 0;
  opacity: 0;
}

/* Absolute so the brand row keeps its layout while the label beside it
   is collapsing to nothing. */
.head-btn {
  position: absolute;
  top: 18px;
  right: 10px;
  display: flex;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  padding: 4px;
  border: none;
  border-radius: var(--radius-sm);
  background: none;
  color: var(--sidebar-text);
  cursor: pointer;
  transition:
    color var(--transition),
    background var(--transition);
}

.head-btn:hover {
  background: var(--sidebar-hover);
  color: var(--sidebar-text-active);
}

/* Narrowed, the control moves OUT of the rail and sits against the
   page, because a 64px column has no room for a mark and a button
   both - and the one control that widens the rail again must not be
   the one that gets squeezed out. */
.rail.narrow .head-btn {
  position: fixed;
  top: 12px;
  right: auto;
  left: calc(64px + 12px);
  z-index: 999;
  border: 1px solid var(--border-primary);
  background: var(--bg-primary);
}

.menu {
  flex: 1;
  overflow-x: hidden;
  overflow-y: auto;
  padding: 8px;
  /* Room under the last item for the browser's own link tooltip. Hover
     any nav row and Chrome or Safari writes the target URL in a bar at
     the bottom left of the window - directly over Platform Settings,
     which is the row a person is reaching for when the Admin section is
     open. It is the browser's chrome, so nothing here can move it, and
     the rail scrolls to its own end: 20px of scrollable space below the
     last row is what lets that row be seen and clicked. */
  padding-bottom: 20px;
}

.menu-group + .menu-group {
  margin-top: 18px;
}

.group-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  padding: 4px 12px 6px;
  overflow: hidden;
  border: none;
  border-radius: var(--radius-sm);
  background: none;
  /* Was dimmed to 0.55, which was legible against the old dark rail but
     not against a light one. The muted token carries the de-emphasis
     now, at a readable contrast in both themes. */
  color: var(--text-muted);
  font-family: inherit;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  white-space: nowrap;
  cursor: pointer;
}

.group-head:hover {
  color: var(--sidebar-text-active);
}

/* Smaller than a nav glyph on purpose: it says which way the group is
   folded and is not a thing being pointed at. */
.group-caret {
  display: flex;
  flex-shrink: 0;
  width: 13px;
  height: 13px;
  opacity: 0.7;
  transition: transform var(--transition);
}

.group-caret svg {
  width: 100%;
  height: 100%;
}

.group-caret.shut {
  transform: rotate(-90deg);
}

.link {
  display: flex;
  overflow: hidden;
  align-items: center;
  gap: 10px;
  height: 30px;
  padding: 0 10px;
  border-radius: var(--radius-sm);
  color: var(--sidebar-text);
  font-size: 13px;
  font-weight: 400;
  white-space: nowrap;
  text-decoration: none;
  cursor: pointer;
  transition:
    background var(--transition),
    color var(--transition);
}

.link:hover {
  background: var(--sidebar-hover);
  color: var(--sidebar-text-active);
}

.link.here {
  background: var(--sidebar-active-bg);
  color: var(--sidebar-active-text);
  font-weight: 500;
}

/* Indented under the row it belongs to. Narrowed there is no row above
   it on screen, so the indent would just push the glyph off centre. */
.link.nested {
  padding-left: 34px;
  font-size: 13px;
}

.rail.narrow .link {
  justify-content: center;
  padding: 9px;
}

.rail.narrow .link.nested {
  padding-left: 9px;
}

/* The box every glyph sits in, whether it labels a nav row or a
   control in the header. Flex so the svg fills it rather than sitting on
   the text baseline. */
.glyph {
  display: flex;
  flex-shrink: 0;
  width: 17px;
  height: 17px;
}

.text {
  overflow: hidden;
  transition: opacity var(--transition-slow);
}

.rail.narrow .text {
  width: 0;
  opacity: 0;
}
</style>
