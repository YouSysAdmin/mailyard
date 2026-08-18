import api from './client'
import type { SMTPCredential, SubmissionInfo } from './types'

export interface CreateCredentialPayload {
  name: string
  allowed_ips?: string[]
  smtp_group?: string
  // sandbox mints a credential whose submissions are captured into
  // the project sandbox instead of delivered. Fixed at creation.
  sandbox?: boolean
}

export const smtpCredentialsApi = {
  list: () =>
    api.get<{ smtp_credentials: SMTPCredential[]; submission: SubmissionInfo }>(
      '/smtp-credentials/',
    ),
  // Create returns the plaintext password exactly once.
  create: (payload: CreateCredentialPayload) =>
    api.post<{ smtp_credential: SMTPCredential; password: string; submission: SubmissionInfo }>(
      '/smtp-credentials/',
      payload,
    ),
  revoke: (id: string) => api.post(`/smtp-credentials/${id}/revoke`),
  remove: (id: string) => api.delete(`/smtp-credentials/${id}`),
}
