// What the sidebar offers, and to whom.
//
// The menu is DATA plus one rule: an entry appears when the permission
// its page needs has been granted. Nothing is hidden by a separate
// visibility flag, so the menu cannot advertise a page that answers
// 403, nor hide one that would have worked. That is why every entry
// names a permission rather than a role - a role flag could say
// "project admin" or "safe for a developer" and nothing between, and
// everything that fitted neither was shown to everyone.
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import { useProjectStore } from '../stores/project'

export interface NavEntry {
  label: string

  /** Absolute path, or '' when the target is derived from the project. */
  path: string

  /** Key into the icon map - see icons.ts. */
  icon: string

  /**
   * Segment under /projects/:id for entries that live inside whichever
   * project is current. 'settings' is the project root itself.
   */
  projectSegment?: string

  /**
   * Rendered under the entry, and NOT filtered separately: children
   * ride with their parent because they are the same resource seen
   * from another angle.
   */
  children?: NavEntry[]

  /** `resource:action` the caller must hold. Absent means any member. */
  permission?: string

  /**
   * Enterprise-only, so the community build leaves it out of the menu.
   * The ROUTE stays registered in both editions - the page explains
   * the edition to anyone arriving by bookmark or from the docs - so
   * this hides the advertisement and nothing else.
   */
  enterprise?: boolean
}

export interface NavGroup {
  id: string
  title: string
  entries: NavEntry[]

  /** Platform administrators only. */
  admin?: boolean

  /** Collapsed on first visit unless this says otherwise. */
  openByDefault?: boolean
}

export const NAV_GROUPS: NavGroup[] = [
  {
    id: 'overview',
    title: 'Overview',
    entries: [{ label: 'Dashboard', path: '/', icon: 'grid', permission: 'analytics:read' }],
  },
  {
    id: 'sending',
    title: 'Sending',
    entries: [
      { label: 'Emails', path: '/emails', icon: 'mail', permission: 'emails:read' },
      { label: 'Campaigns', path: '/campaigns', icon: 'send', permission: 'campaigns:read' },
      {
        label: 'Templates',
        path: '/templates',
        icon: 'file-text',
        permission: 'templates:read',
        children: [
          { label: 'Stylesheets', path: '/stylesheets', icon: 'type' },
          { label: 'Languages', path: '/languages', icon: 'languages' },
        ],
      },
    ],
  },
  {
    id: 'inbound',
    title: 'Inbound',
    entries: [
      {
        label: 'Inbound Emails',
        path: '/inbound-emails',
        icon: 'inbox',
        permission: 'inbound:read',
      },
      { label: 'Inbound Sandbox', path: '/sandbox', icon: 'beaker', permission: 'sandbox:read' },
    ],
  },
  {
    id: 'audience',
    title: 'Audience',
    entries: [
      { label: 'Contacts', path: '/contacts', icon: 'users', permission: 'contacts:read' },
      {
        label: 'Subscribers',
        path: '/subscribers',
        icon: 'user-check',
        permission: 'subscribers:read',
      },
      {
        label: 'Subscriber Lists',
        path: '/subscriber-lists',
        icon: 'list',
        permission: 'subscribers:read',
      },
    ],
  },
  {
    id: 'deliverability',
    title: 'Deliverability',
    entries: [
      {
        label: 'Unsubscribe Lists',
        path: '/unsubscribe-lists',
        icon: 'bell-off',
        permission: 'suppressions:read',
      },
      {
        label: 'Suppressions',
        path: '/suppressions',
        icon: 'mail-x',
        permission: 'suppressions:read',
      },
      { label: 'Bounces', path: '/bounces', icon: 'alert-triangle', permission: 'bounces:read' },
    ],
  },
  {
    id: 'developers',
    title: 'Developers',
    openByDefault: true,
    entries: [
      { label: 'API Keys', path: '/api-keys', icon: 'key', permission: 'apikeys:read' },
      {
        label: 'SMTP Submission',
        path: '/smtp-submission',
        icon: 'plug',
        permission: 'apikeys:read',
      },
      { label: 'Webhooks', path: '/webhooks', icon: 'link', permission: 'webhooks:read' },
      // Deliberately ungated. The page carries two trails: the project
      // one, which needs audit:read and gates its own tab, and the
      // caller's own security trail - sign-ins, 2FA changes - which is
      // served to any authenticated user with no project gate at all.
      // Hiding the entry behind audit:read took every non-admin's
      // sign-in history away with it.
      { label: 'Audit Log', path: '/audit-log', icon: 'history' },
    ],
  },
  {
    id: 'infrastructure',
    title: 'Infrastructure',
    openByDefault: true,
    entries: [
      { label: 'Domains', path: '/domains', icon: 'globe', permission: 'domains:read' },
      { label: 'SMTP Servers', path: '/smtp-servers', icon: 'server', permission: 'smtp:read' },
      { label: 'Server Groups', path: '/smtp-groups', icon: 'layers', permission: 'smtp:read' },
      {
        label: 'Relay Nodes',
        path: '/relay-nodes',
        icon: 'radio-tower',
        permission: 'smtp:read',
        enterprise: true,
      },
    ],
  },
  {
    id: 'project',
    title: 'Project',
    openByDefault: true,
    entries: [
      // Ungated: changing project is not a project-scoped act, and a
      // member of one project has to be able to leave for another
      // whatever they hold in this one. The briefcase repeats the
      // switcher's Manage projects glyph on purpose - two routes to
      // the same page should not look like two features.
      { label: 'All Projects', path: '/projects', icon: 'briefcase' },
      // There is no "hide in a personal project" flag, and these are
      // why it went: it dropped Members and Roles as a tidiness
      // measure, while the server accepted members and roles there
      // like anywhere else. Somebody added to a personal project was
      // invisible and unremovable, and the bootstrap admin could not
      // reach Roles at all. An entry is hidden for a missing
      // permission and for no other reason.
      {
        label: 'Members',
        path: '',
        icon: 'users',
        projectSegment: 'members',
        permission: 'members:read',
      },
      {
        label: 'Roles',
        path: '',
        icon: 'lock',
        projectSegment: 'roles',
        permission: 'members:read',
      },
      {
        label: 'Settings',
        path: '',
        icon: 'settings',
        projectSegment: 'settings',
        // settings:read rather than project-admin. The page has always
        // carried a read-only card written for editors and viewers,
        // and the nav hid the link - so its one audience could reach
        // it only by typing the URL.
        permission: 'settings:read',
      },
    ],
  },
  {
    id: 'admin',
    title: 'Admin',
    admin: true,
    openByDefault: false,
    entries: [
      { label: 'Users', path: '/admin/users', icon: 'users' },
      { label: 'Plans', path: '/admin/plans', icon: 'package' },
      { label: 'Identity Providers', path: '/admin/oauth-providers', icon: 'id-card' },
      { label: 'Shared SMTP', path: '/admin/shared-smtp', icon: 'server' },
      { label: 'Certificates', path: '/admin/certificates', icon: 'shield' },
      { label: 'Relay Nodes', path: '/admin/relay-nodes', icon: 'radio-tower', enterprise: true },
      { label: 'Platform Credentials', path: '/admin/api-keys', icon: 'key' },
      { label: 'Platform Settings', path: '/admin/settings', icon: 'settings' },
    ],
  },
]

const OPEN_GROUPS_KEY = 'mailyard_nav_sections'

/**
 * The menu as this caller sees it: groups they may open, entries they
 * may reach, and which one they are looking at.
 */
export function useNavigation() {
  const route = useRoute()
  const auth = useAuthStore()
  const projects = useProjectStore()

  /** Where an entry actually points, project-relative ones resolved. */
  function hrefFor(entry: NavEntry): string {
    if (!entry.projectSegment) return entry.path

    const base = `/projects/${projects.currentProjectId}`

    return entry.projectSegment === 'settings' ? base : `${base}/${entry.projectSegment}`
  }

  /**
   * Whether the current route is this entry.
   *
   * Derived from hrefFor rather than repeating its branching, which is
   * what let the two drift: a project entry matched EXACTLY while a
   * top-level one also matched its descendants, and the difference was
   * spelled out twice.
   */
  function isCurrent(entry: NavEntry): boolean {
    const href = hrefFor(entry)

    // A project page and the projects list are leaves - '/projects'
    // must not light up while a project's own settings are open.
    if (entry.projectSegment || href === '/projects' || href === '/') {
      return route.path === href
    }

    return route.path === href || route.path.startsWith(`${href}/`)
  }

  function allowed(entry: NavEntry): boolean {
    if (entry.permission && !projects.can(entry.permission)) return false

    // isCommunity is false while the edition is still unknown, so a
    // slow /auth/info shows the entry and then removes it, rather than
    // hiding features on an install that has them.
    if (entry.enterprise && auth.isCommunity) return false

    return true
  }

  /**
   * The groups to render, already filtered, with empty ones dropped.
   *
   * Filtered ONCE into a new shape rather than offering a predicate
   * the template calls again per group - running the permission filter
   * twice per render is both wasted and a chance for the two passes to
   * disagree about what is on screen.
   */
  const groups = computed(() =>
    NAV_GROUPS.filter((group) => !group.admin || auth.isAdmin)
      .map((group) => ({ ...group, entries: group.entries.filter(allowed) }))
      .filter((group) => group.entries.length > 0),
  )

  const open = ref<Record<string, boolean>>(readOpenGroups())

  function readOpenGroups(): Record<string, boolean> {
    const defaults: Record<string, boolean> = {}
    NAV_GROUPS.forEach((g) => {
      defaults[g.id] = g.openByDefault !== false
    })

    try {
      return { ...defaults, ...JSON.parse(localStorage.getItem(OPEN_GROUPS_KEY) ?? '{}') }
    } catch {
      return defaults
    }
  }

  function toggleGroup(id: string) {
    open.value[id] = !open.value[id]
    localStorage.setItem(OPEN_GROUPS_KEY, JSON.stringify(open.value))
  }

  return { groups, open, toggleGroup, hrefFor, isCurrent }
}
