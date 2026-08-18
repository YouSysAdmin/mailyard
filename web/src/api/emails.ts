import api from './client'
import type { Email, EmailAttachment } from './types'

// Field names mirror the Go input DTOs in
// internal/domain/email/endpoint.go (sendInput) and
// endpoint_template.go (templateSendInput): from / to / html / text.
export interface SendEmailPayload {
  from: string
  to: string[]
  subject: string
  html?: string
  text?: string
  headers?: Record<string, string>
  attachments?: EmailAttachment[]
  send_at?: string
  dry_run?: boolean
  // smtp_group routes the send to a named server pool by slug, empty
  // uses the project's default. smtp_server_id pins one server
  // exactly and overrides the group - it never falls back, so it is
  // for testing a specific server rather than for routing.
  smtp_group?: string
  smtp_server_id?: string
}

export interface SendTemplatePayload {
  from: string
  to: string[]
  template_id?: string
  template_name?: string
  language?: string
  data?: Record<string, unknown>
  headers?: Record<string, string>
  attachments?: EmailAttachment[]
  send_at?: string
  dry_run?: boolean
  // smtp_group routes the send to a named server pool by slug, empty
  // uses the project's default. smtp_server_id pins one server
  // exactly and overrides the group - it never falls back, so it is
  // for testing a specific server rather than for routing.
  smtp_group?: string
  smtp_server_id?: string
}

// What a send will accept. Served rather than hardcoded because
// sending.max_attachment_size is an operator setting - a copy here
// would drift the moment someone tunes the config.
export interface SendLimits {
  max_recipients: number
  max_attachments: number
  max_attachment_size: number
  max_total_attachment_size: number
}

export interface EmailListParams {
  status?: string
  // Matches a whole recipient address or part of the subject. Never
  // the body - see the store.
  search?: string
  limit?: number
  /**
   * Keyset cursor over created_at. Send before_id with it: two messages
   * can share a created_at, and the timestamp alone SKIPS every row tied
   * with the last one on the page - they appear on neither page.
   */
  before?: string
  before_id?: string
}

export const emailsApi = {
  list: (params: EmailListParams = {}) => api.get<{ emails: Email[] }>('/emails/', { params }),
  stats: () => api.get<{ counts: Record<string, number> }>('/emails/stats'),
  limits: () => api.get<{ limits: SendLimits }>('/emails/limits'),
  get: (id: string) => api.get<{ email: Email }>(`/emails/${id}`),

  // The original destinations behind the click redirects in this
  // message's body, keyed by link hash. The preview strips our
  // redirects (rendering one would count a click) and puts these back.
  trackedLinks: (id: string) =>
    api.get<{ links: Record<string, string> }>(`/emails/${id}/tracked-links`),
  status: (id: string) =>
    api.get<{
      id: string
      status: string
      attempts: number
      error_message?: string
      sent_at?: string
    }>(`/emails/${id}/status`),
  send: (payload: SendEmailPayload) =>
    api.post<{ email: Email; suppressed_recipients: string[] }>('/emails/send', payload),
  sendTemplate: (payload: SendTemplatePayload) =>
    api.post<{ email: Email; suppressed_recipients: string[] }>('/emails/send-template', payload),
  batch: (payload: { emails: unknown[] }) => api.post('/emails/batch', payload),
  preview: (payload: Record<string, unknown>) => api.post('/emails/preview', payload),
  retry: (id: string) => api.post<{ email: Email }>(`/emails/${id}/retry`),
}
