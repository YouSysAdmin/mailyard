import api, { browserURL } from './client'
import type { SMTPCredential } from './types'

// SandboxAttachment mirrors internal/models/sandbox.Attachment. There
// is no content field on purpose: the bytes live once, inside the raw
// message, and a download re-parses it.
export interface SandboxAttachment {
  filename: string
  content_type?: string
  size: number
}

export type SandboxSource = 'submission' | 'api'

// SandboxEmail mirrors internal/models/sandbox.Email - a message that
// was captured instead of delivered. Sender and recipients are the
// SMTP envelope, which is what a real receiver would have routed on
// and routinely differs from the From and To headers.
export interface SandboxEmail {
  id: string
  project_id: string
  source: SandboxSource
  credential_id?: string
  api_key_id?: string
  sender: string
  recipients: string[]
  subject?: string
  text_body?: string
  html_body?: string
  headers?: Record<string, string>
  attachments?: SandboxAttachment[]
  size: number
  client_ip?: string
  // expires_at is when retention may remove this message. Absent
  // means it is kept until the per-project cap pushes it out.
  expires_at?: string
  received_at: string
  created_at: string
}

export interface SandboxSettings {
  retention_days: number
  max_messages: number
}

// SandboxInfo answers the two questions this page opens with: how do
// I send into it, and what am I allowed to see.
export interface SandboxInfo {
  submission: {
    enabled: boolean
    host: string
    addr: string
    starttls: boolean
  }
  retention_days: number
  max_messages: number
  // sandbox_only says the signed-in member reaches this page and no
  // other resource in the project, so the console can drop the rest
  // of the navigation rather than offering links that all answer 403.
  //
  // Computed from the permission SET, not from a role name: a project
  // writes its own roles, so there is no name to compare against.
  sandbox_only: boolean
}

export const sandboxApi = {
  list: (params: { limit?: number; offset?: number } = {}) =>
    api.get<{ sandbox_emails: SandboxEmail[]; total: number; settings: SandboxSettings }>(
      '/sandbox/',
      { params },
    ),
  info: () => api.get<SandboxInfo>('/sandbox/info'),
  get: (id: string) => api.get<{ sandbox_email: SandboxEmail }>(`/sandbox/${id}`),
  raw: (id: string) => api.get<string>(`/sandbox/${id}/raw`, { responseType: 'text' }),
  remove: (id: string) => api.delete(`/sandbox/${id}`),
  clear: () => api.post<{ deleted: number }>('/sandbox/clear'),
  attachmentUrl: (id: string, idx: number) => browserURL(`/sandbox/${id}/attachments/${idx}`),

  // Sandbox credentials live under /api/sandbox rather than
  // /api/smtp-credentials, which is project admin. The server forces
  // the sandbox flag on every write here, so this endpoint cannot
  // mint a live credential no matter what is sent to it.
  listCredentials: () =>
    api.get<{ smtp_credentials: SMTPCredential[]; submission: SandboxInfo['submission'] }>(
      '/sandbox/credentials',
    ),
  createCredential: (name: string) =>
    api.post<{
      smtp_credential: SMTPCredential
      password: string
      submission: SandboxInfo['submission']
    }>('/sandbox/credentials', { name }),
  revokeCredential: (id: string) => api.post(`/sandbox/credentials/${id}/revoke`),
}
