import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import { useProjectStore } from '../stores/project'
import { safeReturnPath } from '../composables/useReturnPath'

const APP_NAME = 'Mailyard'
const DEFAULT_TITLE = 'Mailyard - Self-Hosted Email Delivery'

const routes = [
  {
    path: '/login',
    name: 'login',
    component: () => import('../views/auth/Login.vue'),
    meta: { guest: true, title: 'Sign in' },
  },
  {
    path: '/register',
    name: 'register',
    component: () => import('../views/auth/Register.vue'),
    meta: { guest: true, title: 'Create account' },
  },
  {
    path: '/forgot-password',
    name: 'forgot-password',
    component: () => import('../views/auth/ForgotPassword.vue'),
    meta: { guest: true, title: 'Reset password' },
  },
  {
    // Reached from the emailed link. Not marked guest: a signed-in
    // user following a reset link should still land on the form
    // rather than be bounced to the dashboard.
    path: '/reset-password',
    name: 'reset-password',
    component: () => import('../views/auth/ResetPassword.vue'),
    meta: { title: 'Choose a new password' },
  },
  {
    // The signup confirmation link. Not guest for the same reason as
    // reset-password: redeeming must not depend on session state.
    path: '/verify-email',
    name: 'verify-email',
    component: () => import('../views/auth/VerifyEmail.vue'),
    meta: { title: 'Confirm your email' },
  },
  {
    path: '/invitations',
    name: 'invitation-accept',
    component: () => import('../views/invitations/InvitationAccept.vue'),
    meta: { title: 'Project Invitation' },
  },
  {
    path: '/',
    component: () => import('../layouts/DashboardLayout.vue'),
    meta: { auth: true },
    children: [
      {
        path: '',
        name: 'dashboard',
        component: () => import('../views/dashboard/Dashboard.vue'),
        meta: { title: 'Dashboard', permission: 'analytics:read' },
      },
      {
        path: 'emails',
        name: 'emails',
        component: () => import('../views/emails/Emails.vue'),
        meta: { title: 'Emails', permission: 'emails:read' },
      },
      {
        path: 'emails/send',
        name: 'email-send',
        component: () => import('../views/emails/EmailSend.vue'),
        meta: { title: 'Send Email', permission: 'emails:write' },
      },
      {
        path: 'emails/:id',
        name: 'email-detail',
        component: () => import('../views/emails/EmailDetail.vue'),
        meta: { title: 'Email', permission: 'emails:read' },
      },
      {
        path: 'inbound-emails',
        name: 'inbound-emails',
        component: () => import('../views/inbound/InboundEmails.vue'),
        meta: { title: 'Inbound Emails', permission: 'inbound:read' },
      },
      {
        // The same page. Inbound is a two-pane mail client, the way the
        // sandbox is, so an id selects a message inside it rather than
        // replacing it with a detail page - a link to one message still
        // opens straight at it, with the list beside it.
        path: 'inbound-emails/:id',
        name: 'inbound-email-detail',
        component: () => import('../views/inbound/InboundEmails.vue'),
        meta: { title: 'Inbound Email', permission: 'inbound:read' },
      },
      {
        path: 'sandbox',
        name: 'sandbox',
        component: () => import('../views/sandbox/Sandbox.vue'),
        meta: { title: 'Inbound Sandbox', permission: 'sandbox:read' },
      },
      {
        // The same page. The sandbox is a two-pane mail client, so an
        // id selects a capture inside it rather than replacing it with
        // a detail page - a link to one message still opens straight at
        // it, with the list beside it.
        path: 'sandbox/:id',
        name: 'sandbox-message',
        component: () => import('../views/sandbox/Sandbox.vue'),
        meta: { title: 'Sandbox Message', permission: 'sandbox:read' },
      },
      {
        path: 'domains',
        name: 'domains',
        component: () => import('../views/domains/Domains.vue'),
        meta: { title: 'Domains', permission: 'domains:read' },
      },
      {
        path: 'templates',
        name: 'templates',
        component: () => import('../views/templates/Templates.vue'),
        meta: { title: 'Templates', permission: 'templates:read' },
      },
      {
        path: 'templates/:id/versions',
        name: 'template-detail',
        component: () => import('../views/templates/TemplateDetail.vue'),
        meta: { title: 'Template Versions', permission: 'templates:read' },
      },
      {
        path: 'templates/:id/versions/:versionId/edit',
        name: 'template-editor',
        component: () => import('../views/templates/TemplateEditor.vue'),
        meta: { title: 'Template Editor', permission: 'templates:write' },
      },
      {
        path: 'templates/:id/versions/:versionId/builder',
        name: 'template-builder',
        component: () => import('../views/templates/EmailBuilder.vue'),
        meta: { title: 'Email Builder', permission: 'templates:write' },
      },
      {
        path: 'templates/:id/preview',
        name: 'template-preview',
        component: () => import('../views/templates/TemplatePreview.vue'),
        meta: { title: 'Template Preview', permission: 'templates:read' },
      },
      {
        path: 'languages',
        name: 'languages',
        component: () => import('../views/languages/Languages.vue'),
        meta: { title: 'Languages', permission: 'templates:read' },
      },
      {
        path: 'stylesheets',
        name: 'stylesheets',
        component: () => import('../views/stylesheets/Stylesheets.vue'),
        meta: { title: 'Stylesheets', permission: 'templates:read' },
      },
      {
        path: 'smtp-servers',
        name: 'smtp-servers',
        component: () => import('../views/smtp/SmtpServers.vue'),
        meta: { title: 'SMTP Servers', permission: 'smtp:read' },
      },
      {
        path: 'smtp-groups',
        name: 'smtp-groups',
        component: () => import('../views/smtp/SmtpGroups.vue'),
        meta: { title: 'Server Groups', permission: 'smtp:read' },
      },
      {
        path: 'relay-nodes',
        name: 'relay-nodes',
        component: () => import('../views/smtp/RelayNodes.vue'),
        meta: { title: 'Relay Nodes', permission: 'smtp:read' },
      },
      {
        path: 'smtp-servers/:id',
        name: 'smtp-server-detail',
        component: () => import('../views/smtp/SmtpServerDetail.vue'),
        meta: { title: 'SMTP Server', permission: 'smtp:read' },
      },
      {
        path: 'webhooks',
        name: 'webhooks',
        component: () => import('../views/webhooks/Webhooks.vue'),
        meta: { title: 'Webhooks', permission: 'webhooks:read' },
      },
      {
        path: 'unsubscribe-lists',
        name: 'unsubscribe-lists',
        component: () => import('../views/unsubscribe-lists/UnsubscribeLists.vue'),
        meta: { title: 'Unsubscribe Lists', permission: 'suppressions:read' },
      },
      {
        path: 'suppressions',
        name: 'suppressions',
        component: () => import('../views/suppressions/Suppressions.vue'),
        meta: { title: 'Suppressions', permission: 'suppressions:read' },
      },
      {
        path: 'bounces',
        name: 'bounces',
        component: () => import('../views/bounces/Bounces.vue'),
        meta: { title: 'Bounces', permission: 'bounces:read' },
      },
      {
        path: 'audit-log',
        name: 'audit-log',
        component: () => import('../views/audit/AuditLog.vue'),
        meta: { title: 'Audit Log' },
      },
      {
        path: 'api-keys',
        name: 'api-keys',
        component: () => import('../views/apikeys/ApiKeys.vue'),
        meta: { title: 'API Keys', permission: 'apikeys:read' },
      },
      {
        path: 'smtp-submission',
        name: 'smtp-submission',
        component: () => import('../views/smtp/SmtpSubmission.vue'),
        meta: { title: 'SMTP Submission', permission: 'apikeys:read' },
      },
      {
        path: 'contacts',
        name: 'contacts',
        component: () => import('../views/contacts/Contacts.vue'),
        meta: { title: 'Contacts', permission: 'contacts:read' },
      },
      {
        path: 'subscribers',
        name: 'subscribers',
        component: () => import('../views/subscribers/Subscribers.vue'),
        meta: { title: 'Subscribers', permission: 'subscribers:read' },
      },
      {
        path: 'subscribers/:id',
        name: 'subscriber-detail',
        component: () => import('../views/subscribers/SubscriberDetail.vue'),
        meta: { title: 'Subscriber', permission: 'subscribers:read' },
      },
      {
        path: 'subscriber-lists',
        name: 'subscriber-lists',
        component: () => import('../views/subscriber-lists/SubscriberLists.vue'),
        meta: { title: 'Subscriber Lists', permission: 'subscribers:read' },
      },
      {
        path: 'subscriber-lists/:id',
        name: 'subscriber-list-detail',
        component: () => import('../views/subscriber-lists/SubscriberListDetail.vue'),
        meta: { title: 'Subscriber List', permission: 'subscribers:read' },
      },
      {
        path: 'campaigns',
        name: 'campaigns',
        component: () => import('../views/campaigns/Campaigns.vue'),
        meta: { title: 'Campaigns', permission: 'campaigns:read' },
      },
      {
        path: 'campaigns/:id',
        name: 'campaign-detail',
        component: () => import('../views/campaigns/CampaignDetail.vue'),
        meta: { title: 'Campaign', permission: 'campaigns:read' },
      },
      {
        path: 'projects',
        name: 'projects',
        component: () => import('../views/projects/Projects.vue'),
        meta: { title: 'Projects' },
      },
      {
        path: 'projects/:id',
        name: 'project-detail',
        component: () => import('../views/projects/ProjectSettings.vue'),
        meta: { title: 'Project Settings', permission: 'settings:read' },
      },
      {
        path: 'projects/:id/members',
        name: 'project-members',
        component: () => import('../views/projects/ProjectMembers.vue'),
        meta: { title: 'Project Members', permission: 'members:read' },
      },
      {
        path: 'projects/:id/roles',
        name: 'project-roles',
        component: () => import('../views/projects/ProjectRoles.vue'),
        meta: { title: 'Project Roles', permission: 'members:read' },
      },
      {
        path: 'profile',
        name: 'profile',
        component: () => import('../views/auth/Profile.vue'),
        meta: { title: 'Profile' },
      },
      // Admin
      {
        path: 'admin/users',
        name: 'admin-users',
        component: () => import('../views/admin/Users.vue'),
        meta: { admin: true, title: 'Admin Users' },
      },
      {
        path: 'admin/plans',
        name: 'admin-plans',
        component: () => import('../views/admin/Plans.vue'),
        meta: { admin: true, title: 'Admin Plans' },
      },
      {
        path: 'admin/oauth-providers',
        name: 'admin-oauth-providers',
        component: () => import('../views/admin/OAuthProviders.vue'),
        meta: { admin: true, title: 'Identity Providers' },
      },
      {
        path: 'admin/api-keys',
        name: 'admin-api-keys',
        component: () => import('../views/admin/AdminKeys.vue'),
        meta: { admin: true, title: 'Platform Credentials' },
      },
      {
        path: 'admin/settings',
        name: 'admin-settings',
        component: () => import('../views/admin/Settings.vue'),
        meta: { admin: true, title: 'Platform Settings' },
      },
      {
        path: 'admin/shared-smtp',
        name: 'admin-shared-smtp',
        component: () => import('../views/admin/SharedSmtp.vue'),
        meta: { admin: true, title: 'Shared SMTP Pool' },
      },
      {
        path: 'admin/certificates',
        name: 'admin-certificates',
        component: () => import('../views/admin/Certificates.vue'),
        meta: { admin: true, title: 'Certificates' },
      },
      {
        path: 'admin/relay-nodes',
        name: 'admin-relay-nodes',
        component: () => import('../views/admin/RelayNodes.vue'),
        meta: { admin: true, title: 'Relay Nodes' },
      },
    ],
  },
  {
    path: '/:pathMatch(.*)*',
    name: 'not-found',
    component: () => import('../views/NotFound.vue'),
    meta: { title: 'Page not found' },
  },
]

const router = createRouter({
  // The SPA is served by the Go binary under /app (env.ConsolePath).
  //
  // Every route path above is relative to this base, so the admin/*
  // segments resolve to /app/admin/* on their own. They must not be
  // rewritten to /app/... - that would produce /app/app/admin/...
  history: createWebHistory('/app/'),
  routes,
})

// landingFor is where somebody goes when they may not go where they
// asked.
//
// Not always the dashboard. A developer holds the sandbox and nothing
// else, so sending them to a dashboard whose every request answers 403
// would replace one refusal with a screenful of them. The project list
// is the last resort because it is the one page that needs no project
// permission at all - it is how you leave for a project where you have
// some.
function landingFor(proj: ReturnType<typeof useProjectStore>) {
  if (proj.can('analytics:read')) return { name: 'dashboard' }
  if (proj.can('sandbox:read')) return { name: 'sandbox' }
  return { name: 'projects' }
}

router.beforeEach(async (to) => {
  const auth = useAuthStore()

  // First navigation with no cached user: ask the backend once. This
  // also detects auth.disabled mode (no login page at all).
  if (to.meta.auth && !auth.isAuthenticated) {
    await auth.fetchUser()
  }

  // With a cached one, ask ANYWAY - once per document, and without
  // waiting. The cache exists so a reload renders before /auth/me
  // resolves, which it still does; what it must not do is decide what
  // the console looks like for the rest of the session. A profile only
  // ever refilled when absent goes stale on anything the server changes
  // under it - a revoked administrator keeps the Admin section, and a
  // granted one never gets it.
  if (to.meta.auth) void auth.refreshOnce()

  if (to.meta.auth && !auth.isAuthenticated) {
    return { name: 'login' }
  }
  if (to.meta.guest && auth.isAuthenticated) {
    // An already-signed-in user who lands on the login page with a
    // return path (from a gated page outside the SPA) should be sent
    // there, not to the dashboard. Same-origin absolute paths only.
    const next = safeReturnPath(to.query.next)
    if (next) {
      window.location.href = next
      return false
    }
    return { name: 'dashboard' }
  }
  if (to.meta.admin && !auth.isAdmin) {
    return { name: 'dashboard' }
  }

  // Resolve the active project before any authenticated view mounts,
  // so the X-Mailyard-Project-Id header is set on its first scoped
  // request. Runs once - later navigations reuse the loaded store.
  if (to.meta.auth && auth.isAuthenticated) {
    const proj = useProjectStore()
    if (proj.projects.length === 0) {
      await proj.fetchProjects()
    }

    // Which build the server is, asked once per document. Not awaited:
    // it decides how a page EXPLAINS itself, never whether it renders,
    // so blocking every navigation on it would trade a badge for a
    // pause on an install where /auth/info is slow.
    auth.ensureEdition()

    // And then refuse a page this member has no permission for.
    //
    // The nav hides those items already, but hiding a link is not a
    // control: the URL is still typeable, and the page would mount and
    // fire requests that all answer 403. A screen of failed loads reads
    // as the product being broken rather than as "you do not have this".
    //
    // Presentation, not enforcement - every one of those requests is
    // checked server side regardless. This just delivers the answer
    // once, in words, instead of eight times as an error toast.
    //
    // A route that addresses a project by path id is asking about that
    // project, not the active one. /projects/:id and its children are
    // reached from a list of every project somebody belongs to, where
    // the two are routinely different.
    const need = to.meta.permission
    const named =
      typeof to.params.id === 'string' && to.matched.some((r) => r.path.startsWith('/projects/:id'))
    if (typeof need === 'string') {
      const allowed = named ? proj.canIn(to.params.id as string, need) : proj.can(need)
      if (!allowed) return landingFor(proj)
    }
  }
})

const CHUNK_RELOAD = 'mailyard:chunk-reload'

router.afterEach((to, _from, failure) => {
  // afterEach runs for ABORTED navigations too, which is why the
  // failure argument is checked rather than ignored. A guard that
  // refuses to leave - the builder asking about unsaved work - would
  // otherwise retitle the tab after the page it names was never
  // reached, and clear the chunk-reload flag on a navigation that did
  // not get through.
  if (failure) return

  const pageTitle = to.meta.title as string | undefined
  document.title = pageTitle ? `${pageTitle} - ${APP_NAME}` : DEFAULT_TITLE

  sessionStorage.removeItem(CHUNK_RELOAD)
})

// Every view is lazy, so a page is a separate file fetched at the
// moment you click it. After an upgrade the file this document names is
// gone, and the click does nothing at all - no page, no message, just
// a TypeError in the console. The tab has to be reloaded to pick up the
// new document, so reload it.
//
// Once, tracked in sessionStorage. If the chunk is genuinely missing
// rather than stale, a loop would replace a legible console error with
// a page that reloads forever.
router.onError((err, to) => {
  const message = String((err as Error)?.message ?? err)
  // Safari, Chrome and Firefox each word this differently.
  if (!/module script failed|dynamically imported module/i.test(message)) return
  if (sessionStorage.getItem(CHUNK_RELOAD)) return
  sessionStorage.setItem(CHUNK_RELOAD, '1')
  window.location.assign(router.resolve(to).href)
})

export default router
