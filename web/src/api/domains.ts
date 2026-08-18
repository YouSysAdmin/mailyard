import api from './client'

// InboundDomain mirrors internal/models/domain/domain.go. The
// verification token is not a secret - knowing it is useless without
// DNS control over the domain.
export interface InboundDomain {
  id: string
  project_id: string
  created_by?: string
  domain: string
  verification_token: string
  verified: boolean
  verified_at?: string
  created_at: string
  // DKIM signing. The private key never leaves the server - only the
  // selector and the public half are exposed, and both are meant to
  // be published in DNS.
  dkim_selector?: string
  dkim_public_key?: string
  // The three record checks, refreshed by verify(). Separate from
  // `verified`, which is ownership alone.
  spf_verified: boolean
  dkim_verified: boolean
  dmarc_verified: boolean
  checked_at?: string
}

// DNSRecord is one record the operator must publish, assembled
// server-side in internal/domain/domains/records.go.
export interface DNSRecord {
  // kind is 'ownership' | 'spf' | 'dkim' | 'dmarc'.
  kind: string
  type: string
  host: string
  value: string
  // required marks the records without which the domain does not work
  // at all, as opposed to those that only improve how receivers treat
  // its mail.
  required: boolean
  verified: boolean
  detail?: string
}

export interface DomainPayload {
  domain: InboundDomain
  dns_records: DNSRecord[]
}

export const domainsApi = {
  list: () => api.get<{ domains: InboundDomain[] }>('/domains/'),
  // 409 when the name is already claimed by any project.
  create: (domain: string) => api.post<DomainPayload>('/domains/', { domain }),
  get: (id: string) => api.get<DomainPayload>(`/domains/${id}`),
  // Runs a live DNS TXT check - the returned verified flag reflects
  // the outcome and a lost record un-verifies the domain again.
  verify: (id: string) => api.post<DomainPayload>(`/domains/${id}/verify`),
  remove: (id: string) => api.delete(`/domains/${id}`),
}
