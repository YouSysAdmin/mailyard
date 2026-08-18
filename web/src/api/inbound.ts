import api from './client'

// InboundAttachment mirrors internal/models/inbound (Attachment).
// Content is the base64-encoded body and may be absent.
export interface InboundAttachment {
  filename: string
  content_type?: string
  size: number
  content?: string
}

export type InboundEmailStatus = 'received' | 'rejected' | 'failed'

// InboundEmail mirrors internal/models/inbound (Email). Sender and
// recipients are the SMTP envelope, the source of truth for routing.
export interface InboundEmail {
  id: string
  project_id: string
  domain_id: string
  message_id?: string
  sender: string
  recipients: string[]
  subject?: string
  text_body?: string
  html_body?: string
  headers?: Record<string, string>
  attachments?: InboundAttachment[]
  // has_raw reports whether the original wire bytes are downloadable.
  // Only failed-parse messages retain them.
  has_raw?: boolean
  size: number
  // auth is the SPF/DKIM/DMARC verdict recorded when the message
  // arrived. Absent on rows received before authentication existed,
  // which means "never checked" and not "failed".
  auth?: InboundAuth
  status: InboundEmailStatus
  error_message?: string
  received_at: string
  created_at: string
}

// InboundAuth mirrors internal/models/inbound.Auth. Values are the
// RFC 8601 result keywords: pass, fail, softfail, neutral, none,
// temperror, permerror.
export interface InboundAuth {
  spf: string
  dkim: string
  dmarc: string
  dmarc_policy?: string
  // aligned is the verdict that matters: something the From domain
  // vouches for actually passed. Everything else is detail.
  aligned: boolean
  client_ip?: string
}

export interface InboundListParams {
  status?: string
  limit?: number
  // RFC 3339 keyset cursor over received_at, whose other half is
  // before_id - the timestamp alone drops rows that share it with the
  // last one on the page.
  before?: string
  before_id?: string
}

export const inboundApi = {
  list: (params: InboundListParams = {}) =>
    api.get<{ inbound_emails: InboundEmail[] }>('/inbound-emails/', { params }),
  stats: () => api.get<{ counts: Record<string, number> }>('/inbound-emails/stats'),
  get: (id: string) => api.get<{ inbound_email: InboundEmail }>(`/inbound-emails/${id}`),
  remove: (id: string) => api.delete(`/inbound-emails/${id}`),
  retryWebhook: (id: string) => api.post<{ emitted: boolean }>(`/inbound-emails/${id}/retry`),
}
