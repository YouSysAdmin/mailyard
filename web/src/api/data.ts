import api from './client'

// Project data export. The response is a JSON document, not a file
// stream, so the caller builds the download itself - see
// downloadExport below.
export interface ProjectExport {
  mailyard_version: string
  exported_at: string
  project: { id: string; name: string; slug: string }
  templates: unknown[]
  stylesheets: unknown[]
  languages: unknown[]
  contacts: unknown[]
  subscribers: unknown[]
  subscriber_lists: unknown[]
  suppressions: unknown[]
  unsubscribe_lists: unknown[]
  webhooks: unknown[]
  smtp_servers: unknown[]
  domains: unknown[]
  senders: unknown[]
  // Sections that hit their ceiling. Only the suppression list has one -
  // it is the single section that grows per message rather than per thing
  // somebody made - and an export that stopped early has to say so.
  truncated?: string[]
}

export const dataApi = {
  // The active project comes from the X-Mailyard-Project-Id header
  // the client injects, so there is no id parameter.
  export: () => api.get<{ export: ProjectExport }>('/data/export'),
}

// Section counts for the summary shown after an export, in the order
// they appear on screen. Keyed on the export document so a new
// section added server-side shows up here without a second edit.
export function exportCounts(doc: ProjectExport): { label: string; count: number }[] {
  const sections: [string, unknown][] = [
    ['Templates', doc.templates],
    ['Stylesheets', doc.stylesheets],
    ['Languages', doc.languages],
    ['Contacts', doc.contacts],
    ['Subscribers', doc.subscribers],
    ['Subscriber lists', doc.subscriber_lists],
    ['Suppressions', doc.suppressions],
    ['Unsubscribe lists', doc.unsubscribe_lists],
    ['Webhooks', doc.webhooks],
    ['SMTP servers', doc.smtp_servers],
    ['Domains', doc.domains],
    ['Sender addresses', doc.senders],
  ]
  return sections.map(([label, rows]) => ({
    label,
    count: Array.isArray(rows) ? rows.length : 0,
  }))
}
