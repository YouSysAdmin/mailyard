import api from './client'

// Sender mirrors internal/models/sender/sender.go. Addresses can only
// be registered for domains verified by the project and the console
// offers them in every From selector.
export interface Sender {
  id: string
  project_id: string
  created_by?: string
  email: string
  name?: string
  created_at: string
  // The key the address signs its mail with, described and without its
  // material. Absent when it has none.
  signing?: SenderSigning | null
}

// SenderSigning mirrors sender.SigningKey as the list carries it.
export interface SenderSigning {
  kind: 'pgp' | 'smime'
  fingerprint: string
  algorithm: string
  subject: string
  issuer?: string
  not_after?: string
  sign: boolean
  attach_key: boolean
  created_at: string
  updated_at: string
}

// SigningPayload mirrors signingInput: which fields matter depends on
// kind. A PGP key is generated when private_key is empty. An S/MIME
// pair is a PEM certificate chain plus private_key, or pkcs12 as the
// base64 of the .p12 file, with its passphrase.
export interface SigningPayload {
  kind: 'pgp' | 'smime'
  mode?: 'generate' | 'import'
  private_key?: string
  certificate?: string
  pkcs12?: string
  passphrase?: string
}

export interface SigningFlagsPayload {
  sign?: boolean
  attach_key?: boolean
}

export const sendersApi = {
  list: () => api.get<{ senders: Sender[] }>('/senders/'),
  // 400 when the address domain is not verified by this project,
  // 409 when the address is already registered.
  create: (payload: { email: string; name?: string }) =>
    api.post<{ sender: Sender }>('/senders/', payload),
  remove: (id: string) => api.delete(`/senders/${id}`),
  // 400 on a key the server will not accept, named on its field. 409
  // when the sender already has one: remove it first.
  setSigning: (id: string, payload: SigningPayload) =>
    api.post<{ sender: Sender }>(`/senders/${id}/signing`, payload),
  setSigningFlags: (id: string, payload: SigningFlagsPayload) =>
    api.patch<{ sender: Sender }>(`/senders/${id}/signing`, payload),
  removeSigning: (id: string) => api.delete(`/senders/${id}/signing`),
  publicKey: (id: string) =>
    api.get<{ kind: 'pgp' | 'smime'; public_key: string }>(`/senders/${id}/signing/public-key`),
}
