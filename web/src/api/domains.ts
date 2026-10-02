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
  // A rotation in progress: the next key, published beside the
  // current record and cut over by verify() once it is seen.
  dkim_next_selector?: string
  dkim_next_public_key?: string
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
  // kind is 'ownership' | 'spf' | 'dkim' | 'dkim_next' | 'dmarc'.
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

// DomainGrant is another project this project's domain is shared with.
// The id is for the revoke call only, the console shows the name.
export interface DomainGrant {
  domain_id: string
  project_id: string
  project_name: string
  project_slug: string
  granted_by?: string
  created_at: string
}

// SharedDomain is a domain another project shared with this one: this
// project may send as it, only the owner manages it.
export interface SharedDomain {
  id: string
  domain: string
  owner_name: string
  created_at: string
}

export const domainsApi = {
  list: () => api.get<{ domains: InboundDomain[]; shared: SharedDomain[] }>('/domains/'),
  // 409 when the name is already claimed by any project.
  create: (domain: string) => api.post<DomainPayload>('/domains/', { domain }),
  get: (id: string) => api.get<DomainPayload>(`/domains/${id}`),
  // Runs a live DNS TXT check - the returned verified flag reflects
  // the outcome and a lost record un-verifies the domain again.
  verify: (id: string) => api.post<DomainPayload>(`/domains/${id}/verify`),
  // Mints the next DKIM key. Signing switches to it when verify() sees
  // the new record published.
  rotateDkim: (id: string) => api.post<DomainPayload>(`/domains/${id}/dkim/rotate`),
  cancelDkimRotation: (id: string) => api.delete<DomainPayload>(`/domains/${id}/dkim/rotate`),
  remove: (id: string) => api.delete(`/domains/${id}`),
  grants: (id: string) => api.get<{ grants: DomainGrant[] }>(`/domains/${id}/grants`),
  // The project is named by its slug. Sharing again is a no-op.
  share: (id: string, projectSlug: string) =>
    api.post<{ grants: DomainGrant[] }>(`/domains/${id}/grants`, { project_slug: projectSlug }),
  unshare: (id: string, projectId: string) => api.delete(`/domains/${id}/grants/${projectId}`),
}
