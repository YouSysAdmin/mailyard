import api from './client'
import type { MailProvider, SharedSMTPServer } from './types'

export interface SharedSMTPPayload {
  name?: string
  // Create-only, like on a project server: the credentials mean something
  // different to each provider.
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
  security_mode?: string
  platform_only?: boolean
  priority?: number
  status?: string
}

// The platform SMTP pool. Every route here is platform-admin only -
// these servers belong to no project and no project-scoped endpoint
// returns them.
export const sharedSmtpApi = {
  list: () =>
    api.get<{ shared_smtp_servers: SharedSMTPServer[]; providers: MailProvider[] }>(
      '/admin/shared-smtp-servers/',
    ),
  get: (id: string) =>
    api.get<{ shared_smtp_server: SharedSMTPServer }>(`/admin/shared-smtp-servers/${id}`),
  create: (payload: SharedSMTPPayload) =>
    api.post<{ shared_smtp_server: SharedSMTPServer }>('/admin/shared-smtp-servers/', payload),
  update: (id: string, payload: SharedSMTPPayload) =>
    api.patch<{ shared_smtp_server: SharedSMTPServer }>(
      `/admin/shared-smtp-servers/${id}`,
      payload,
    ),
  remove: (id: string) => api.delete(`/admin/shared-smtp-servers/${id}`),
  test: (id: string) =>
    api.post<{ ok: boolean; error?: string }>(`/admin/shared-smtp-servers/${id}/test`),
}
