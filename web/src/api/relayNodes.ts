import api from './client'
import type { RelayNode } from './types'

// Relay nodes: egress machines that enrolled themselves and deliver
// straight to recipient mail exchangers. Platform-admin only - a node
// serves the shared pool, which no project-scoped route exposes.
//
// There is no create here on purpose. A node is not made from the
// console: it enrols itself with the shared token and appears
// pending, and what an admin does is decide whether it may carry mail.
export const relayNodesApi = {
  list: () =>
    api.get<{
      relay_nodes: RelayNode[]
      spf_include: string
      // mx_hosts are the approved nodes running an MX. A node that
      // receives is useless until DNS points at it, and nothing else
      // says so.
      mx_hosts: string[]
      auto_approve: boolean
      // enabled reports relay_nodes.enabled. The listing works either
      // way, but with it off no node can enrol - so an empty list
      // means "not turned on" rather than "none yet".
      enabled: boolean
      // available reports whether the SERVER BUILD carries relay nodes.
      // The community edition answers false, and the page says so
      // instead of pointing at a switch that build refuses to start
      // with.
      available: boolean
    }>('/admin/relay-nodes/'),
  approve: (id: string) => api.post<{ status: string }>(`/admin/relay-nodes/${id}/approve`),
  suspend: (id: string) => api.post<{ status: string }>(`/admin/relay-nodes/${id}/suspend`),
  remove: (id: string) => api.delete(`/admin/relay-nodes/${id}`),
  // The emergency lever: the private authority that signs every node
  // certificate, and every node identity with it. Nothing comes back on
  // its own - each node has to be given its enrolment token again.
  resetAuthority: () =>
    api.delete<{ nodes_unenrolled: number; message: string }>('/admin/relay-nodes/authority'),
}

// A project's own relay nodes: the same machine, enrolled with an API
// key holding relay:write instead of the operator's shared token,
// and approved by an admin of this project rather than a platform
// admin.
//
// Also no create. A node enrols itself and appears here pending.
export const myRelayNodesApi = {
  list: () =>
    api.get<{
      relay_nodes: RelayNode[]
      mx_hosts: string[]
      enabled: boolean
      available: boolean
    }>('/my/relay-nodes/'),
  approve: (id: string) => api.post<{ status: string }>(`/my/relay-nodes/${id}/approve`),
  suspend: (id: string) => api.post<{ status: string }>(`/my/relay-nodes/${id}/suspend`),
  remove: (id: string) => api.delete(`/my/relay-nodes/${id}`),
}
