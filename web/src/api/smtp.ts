import api from './client'
import type { MailProvider, SMTPServer } from './types'

export interface SMTPServerPayload {
  name?: string
  // Set on create only. The server refuses it on PATCH: switching a live
  // row from a dial to an API leaves the credentials meaning something
  // else, and the fields that are then wrong are the ones a PATCH leaves
  // alone.
  provider?: string
  provider_config?: Record<string, string>
  host?: string
  port?: number
  username?: string
  password?: string
  encryption?: string
  skip_dkim?: boolean
  ses_topic_arn?: string
  allowed_emails?: string[]
  allowed_domains?: string[]
  group_id?: string
  priority?: number
}

export const smtpApi = {
  list: () => api.get<{ smtp_servers: SMTPServer[]; providers: MailProvider[] }>('/smtp-servers/'),
  get: (id: string) => api.get<{ smtp_server: SMTPServer }>(`/smtp-servers/${id}`),
  create: (payload: SMTPServerPayload) =>
    api.post<{ smtp_server: SMTPServer }>('/smtp-servers/', payload),
  update: (id: string, payload: SMTPServerPayload) =>
    api.patch<{ smtp_server: SMTPServer }>(`/smtp-servers/${id}`, payload),
  remove: (id: string) => api.delete(`/smtp-servers/${id}`),
  // Test returns the connection verdict, not the server record.
  test: (id: string) => api.post<{ ok: boolean; error?: string }>(`/smtp-servers/${id}/test`),
  enable: (id: string) => api.post<{ smtp_server: SMTPServer }>(`/smtp-servers/${id}/enable`),
  disable: (id: string) => api.post<{ smtp_server: SMTPServer }>(`/smtp-servers/${id}/disable`),
}
