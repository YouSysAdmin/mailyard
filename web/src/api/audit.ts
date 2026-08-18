import api, { appApi } from './client'

export interface AuditEvent {
  id: string
  category: 'project' | 'security'
  type: string
  project_id?: string
  actor_id?: string
  actor_email?: string
  client_ip?: string
  // What the request announced itself as. Neither this nor the address
  // identifies anybody - see the model - but together they are what a
  // request tells us, and this is the half a shared egress proxy does not
  // flatten.
  user_agent?: string
  method?: string
  path?: string
  status?: number
  detail?: string
  created_at: string
}

// One export, unpaged, over an optional window. `truncated` says the cap
// was reached - the file is short and the caller has to narrow the range,
// which is the one thing an export must never keep quiet about.
export interface AuditExport {
  events: AuditEvent[]
  from: string
  to: string
  count: number
  truncated: boolean
  cap: number
}

// A window with both ends optional. Either a date (2026-08-01) or an
// RFC 3339 timestamp, decided by the server.
export interface AuditWindow {
  from?: string
  to?: string
}

export const auditApi = {
  // Project configuration activity. Requires audit:read.
  projectLog: (limit = 50, offset = 0) =>
    api.get<{ events: AuditEvent[]; limit: number; offset: number }>('/audit-log', {
      params: { limit, offset },
    }),
  // Account activity. `all` is honored only for platform admins.
  //
  // On appApi, not api: the security log is a browser ceremony - who
  // signed in, from where, and whether it worked - so it stayed on
  // /app/api when the product surface moved to /api/v1. The project
  // audit trail above it is a product operation and did move.
  securityLog: (limit = 50, offset = 0, all = false) =>
    appApi.get<{ events: AuditEvent[]; limit: number; offset: number }>('/security-log', {
      params: { limit, offset, ...(all ? { all: 'true' } : {}) },
    }),

  // The two exports mirror the two logs above, including which prefix
  // each one lives on.
  projectLogExport: (w: AuditWindow = {}) =>
    api.get<AuditExport>('/audit-log/export', { params: { ...w } }),
  securityLogExport: (w: AuditWindow = {}, all = false) =>
    appApi.get<AuditExport>('/security-log/export', {
      params: { ...w, ...(all ? { all: 'true' } : {}) },
    }),
}
