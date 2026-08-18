import { defineStore } from 'pinia'
import { ref, computed, watch } from 'vue'
import { PROJECT_KEY } from '../api/client'
import { projectApi } from '../api/projects'
import { useAuthStore } from './auth'
import type { Project } from '../api/types'

export const useProjectStore = defineStore('project', () => {
  const projects = ref<Project[]>([])
  const currentProjectId = ref<string | null>(localStorage.getItem(PROJECT_KEY))
  // Whether the current user OWNS the active project, fetched lazily
  // from GET /projects/:id. Not a role - it is the one thing outside
  // the permission catalogue, carrying the acts no permission names.
  const isOwner = ref(false)
  // What the server says this member may do here, as `resource:action`
  // strings, from the same call.
  //
  // Sent by the server rather than derived from the role in here. A
  // second copy of the presets in TypeScript would be a second thing
  // to keep true, and the copy that drifts is always the one that
  // enforces nothing - so the menu would start offering pages the API
  // refuses, which is the failure this replaced.
  //
  // It decides what is SHOWN and nothing else. Every request is
  // checked again server-side, so editing this in a browser changes a
  // menu and no access.
  const permissions = ref<string[]>([])
  // Which project the permission set above was resolved for.
  //
  // Not "is the set non-empty": a member of a project that names no
  // default role legitimately holds nothing, and keying on emptiness
  // re-fetched their access on every navigation forever.
  const accessFor = ref<string | null>(null)
  // What the caller may do in every project they can see, from the list
  // response, keyed by project id.
  //
  // `permissions` above is the ACTIVE project's, which is the only one
  // most pages need. The projects page is the exception: it lists every
  // project and offers per-row actions, and judging those by the active
  // project's role is how a member without members:read got a Members
  // button that answered 403.
  const access = ref<Record<string, { owner: boolean; permissions: string[] }>>({})
  // Whether this account may create a project at all.
  //
  // Not a permission and not per project: it is the platform's
  // `user_project_creation` setting, off by default, with platform
  // admins exempt. The server decides it and sends it with the list -
  // see project.MayCreate, which the create route is gated on - so
  // there is one rule rather than a copy of it in here.
  //
  // Starts FALSE, which matters: the four controls it gates would
  // otherwise flash on for the moment between boot and the list
  // arriving, on exactly the installations that turned this off.
  const canCreateProjects = ref(false)

  const currentProject = computed(
    () => projects.value.find((w) => w.id === currentProjectId.value) ?? null,
  )
  // Loaded tracks whether the list has actually been fetched, so the
  // views can tell "no projects" apart from "not asked yet".
  const loaded = ref(false)
  // An account can genuinely belong to nothing: nothing is minted for
  // a new user, so the projects page with its create button is the
  // answer rather than a project nobody asked for.
  const hasNoProjects = computed(() => loaded.value && projects.value.length === 0)
  const contextLabel = computed(() => currentProject.value?.name ?? 'No project')

  // can is the one predicate everything else is built from.
  //
  // A platform admin is owner-equivalent everywhere, exactly as the
  // server treats them, so they short-circuit before the set is
  // consulted at all.
  function can(perm: string): boolean {
    const auth = useAuthStore()
    if (auth.isAdmin) return true
    if (permissions.value.includes('*')) return true
    return permissions.value.includes(perm)
  }

  // canIn is can() for a NAMED project rather than the active one.
  //
  // Used by the projects page and by the router guard for the routes that
  // address a project by path id - /projects/:id and its children, where
  // the id is frequently not the active project at all.
  function canIn(projId: string, perm: string): boolean {
    const auth = useAuthStore()
    if (auth.isAdmin) return true
    const held = access.value[projId]?.permissions ?? []
    return held.includes('*') || held.includes(perm)
  }

  // isProjectOwner gates the two controls no permission can express:
  // deleting the project and rewriting its single sign-on policy.
  //
  // It replaced isProjectAdmin, which also gated the destructive
  // DELETE buttons back when read/write could not say "may edit but
  // not remove". Those ask can() for the resource's delete action
  // now, and reaching for ownership instead would put a control
  // behind a tier the server happily lets a role hold.
  const isProjectOwner = computed(() => {
    const auth = useAuthStore()
    if (auth.isAdmin) return true
    return isOwner.value
  })

  async function setProject(projId: string | null) {
    currentProjectId.value = projId
    isOwner.value = false
    permissions.value = []
    accessFor.value = null
    if (projId) {
      localStorage.setItem(PROJECT_KEY, projId)
      await fetchAccess()
    } else {
      localStorage.removeItem(PROJECT_KEY)
    }
  }

  async function fetchAccess(force = false) {
    const projId = currentProjectId.value
    if (!projId) return
    if (!force && accessFor.value === projId) return
    try {
      const res = await projectApi.get(projId)
      isOwner.value = res.data.owner ?? false
      permissions.value = res.data.permissions ?? []
      accessFor.value = projId
    } catch {
      // A failed fetch leaves the caller holding nothing, which hides
      // every gated item rather than showing links that will 403.
      isOwner.value = false
      permissions.value = []
      accessFor.value = null
    }
  }

  // force is for the paths that CHANGED the list - creating a
  // project, accepting an invitation. Everything else asks for the
  // list it may already have: the router guard fetches it before the
  // first authenticated view mounts, and the layout and the projects
  // page both asked again a moment later, so one page load made three
  // identical round trips.
  async function fetchProjects(force = false) {
    if (loaded.value && !force) {
      await fetchAccess()
      return
    }
    try {
      const res = await projectApi.list()
      projects.value = res.data.projects ?? []
      access.value = res.data.access ?? {}
      canCreateProjects.value = res.data.can_create === true
      loaded.value = true
      const valid =
        currentProjectId.value && projects.value.some((w) => w.id === currentProjectId.value)
      if (!valid) {
        // The stored id names a project that is gone, or one this
        // account was removed from. Fall to the first it does have,
        // else to none - the server answers a stale id by resolving
        // no project rather than refusing, so this list is reachable
        // to be reconciled against.
        await setProject(projects.value[0]?.id ?? null)
      } else {
        await fetchAccess()
      }
    } catch {
      projects.value = []
    }
  }

  // Everything above belongs to one account, so it goes when the account
  // does.
  //
  // clear() existed from the beginning and NOTHING CALLED IT. Signing out
  // dropped the user and the stored project id and left this store whole:
  // `loaded` stayed true so the next person never fetched their own list,
  // `accessFor` stayed so their permissions were never resolved, and
  // `currentProjectId` stayed in memory so pages asked for a project they
  // were not a member of. An administrator signing out and a member
  // signing in got the administrator's menu and a 400 per page.
  //
  // Watched here rather than called from the auth store: this store
  // already imports that one, and the reverse edge would be a cycle
  // between two stores that both initialise at boot.
  watch(
    () => useAuthStore().user?.id ?? null,
    (id, was) => {
      if (id !== was) clear()
    },
  )

  function clear() {
    projects.value = []
    access.value = {}
    canCreateProjects.value = false
    currentProjectId.value = null
    isOwner.value = false
    permissions.value = []
    accessFor.value = null
    loaded.value = false
    localStorage.removeItem(PROJECT_KEY)
  }

  return {
    projects,
    currentProjectId,
    currentProject,
    isOwner,
    loaded,
    hasNoProjects,
    contextLabel,
    permissions,
    access,
    canCreateProjects,
    can,
    canIn,
    isProjectOwner,
    setProject,
    fetchProjects,
    fetchAccess,
    clear,
  }
})
