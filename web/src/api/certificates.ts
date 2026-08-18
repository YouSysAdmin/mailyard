import api from './client'

// What a certificate says about itself, parsed from the public half by
// the server - the console never sees a private key.
export interface CertificateDetails {
  subject: string
  issuer: string
  dns_names?: string[]
  not_before: string
  not_after: string
  serial: string
  fingerprint: string
  key_algorithm: string
  self_signed: boolean
  // is_ca marks an authority. It carries no host names and no
  // ServerAuth, so a listener serving one refuses every client - which
  // is why it is filtered out of the assignment control rather than
  // just labelled.
  is_ca: boolean
  subject_key_id?: string
  authority_key_id?: string
  chain_length: number
}

// The Subject an operator fills in. Every field is optional -
// common_name falls back to the first host for a certificate, and to
// the name for an authority.
export interface CertificateSubject {
  common_name?: string
  organization?: string
  unit?: string
  country?: string
  state?: string
  locality?: string
}

export interface ManagedCertificate {
  name: string
  details?: CertificateDetails
  used_by?: string[]
  // Listeners assigned to it that do not terminate TLS. The assignment
  // is recorded and nothing presents it - separate from used_by
  // because merging them had this page reporting a certificate as in
  // use while the listener spoke plaintext.
  dormant?: string[]
  created_at: string
  updated_at: string
}

export interface SystemCertificate {
  scope: string
  name: string
  details?: CertificateDetails
}

export interface ACMEHost {
  host: string
  details?: CertificateDetails
}

// What ACME is configured to do. All of it is platform settings, so it is
// written through the settings endpoint and takes effect with no restart.
export interface ACMEStatus {
  enabled: boolean
  email: string
  directory_url: string
  staging: boolean
  hosts: ACMEHost[]
  // A hostname worth adding that is not configured yet - the one in
  // server.public_url. A suggestion, not a default: ordering is an
  // outbound call against a rate limit.
  suggested?: string
  challenge_addr?: string
  // Whether this process does its own TLS handshake. Without it the CA
  // cannot validate over tls-alpn-01 and port 80 is the only route.
  tls_terminated_here: boolean
}

// What one listener presents right now, resolved by the server down the
// same chain a handshake walks: assigned certificate, then ACME, then the
// self-signed pair.
//
// Not reassembled here. An assignment map plus a list of listeners with
// TLS off cannot say what a listener with no assignment is actually
// serving, so an operator whose Let's Encrypt order had just succeeded
// would read "Nothing assigned" three times while that certificate was
// on the wire.
export interface ListenerState {
  listener: string
  tls: boolean
  assigned?: string
  // 'managed' | 'acme' | 'selfsigned' | 'none'
  serving: string
  serving_names?: string[]
  // What the chain answers with nothing assigned, which is what the
  // unassigned option in the selector is labelled with.
  fallback: string
  fallback_names?: string[]
}

export const certificatesApi = {
  list: () =>
    api.get<{
      certificates: ManagedCertificate[]
      listeners: ListenerState[]
    }>('/admin/certificates/'),
  system: () => api.get<{ certificates: SystemCertificate[] }>('/admin/certificates/system'),
  acme: () => api.get<ACMEStatus>('/admin/certificates/acme'),
  upload: (payload: { name: string; certificate: string; private_key: string }) =>
    api.post<{ certificate: ManagedCertificate }>('/admin/certificates/', payload),
  generate: (payload: {
    name: string
    hosts: string[]
    subject?: CertificateSubject
    algorithm?: string
    // issuer names a stored authority to sign with. Empty is
    // self-signed.
    issuer?: string
    validity_days?: number
  }) => api.post<{ certificate: ManagedCertificate }>('/admin/certificates/generate', payload),
  generateCA: (payload: {
    name: string
    subject?: CertificateSubject
    algorithm?: string
    validity_days?: number
  }) => api.post<{ certificate: ManagedCertificate }>('/admin/certificates/generate-ca', payload),
  // The public half, so an authority can be installed wherever it has
  // to be trusted. JSON rather than a download, so this reads like
  // every other call - the page turns it into a file.
  pem: (name: string) =>
    api.get<{ name: string; pem: string }>(`/admin/certificates/${encodeURIComponent(name)}/pem`),
  renew: (host: string) =>
    api.post<{ message: string }>('/admin/certificates/acme/renew', { host }),
  order: (host: string) =>
    api.post<{ message: string }>('/admin/certificates/acme/order', { host }),
  remove: (name: string) => api.delete(`/admin/certificates/${encodeURIComponent(name)}`),
}
