import api from './client'
import type { Campaign, CampaignMessage, CampaignVariant } from './types'

export interface CampaignPayload {
  name?: string
  subject?: string
  from_email?: string
  from_name?: string
  template_id?: string
  language?: string
  template_data?: Record<string, unknown>
  list_id?: string
  send_rate?: number
  // smtp_group routes the whole campaign to a named pool, by slug.
  // Empty uses the project's default group.
  smtp_group?: string
  send_at_local_time?: boolean
  ab_test_enabled?: boolean
  ab_variants?: CampaignVariant[]
}

export interface CampaignDetail {
  campaign: Campaign
  stats?: Record<string, number>
  stats_by_variant?: Record<string, Record<string, number>>
  // Rates arrive computed. The console does not divide these itself:
  // the dashboard reports the same pair project-wide, and two places
  // doing the arithmetic is two places to disagree about the
  // denominator. `sent` IS that denominator - recipients delivered to,
  // not the audience size.
  engagement?: {
    opened: number
    clicked: number
    sent: number
    open_rate: number
    click_rate: number
  }
}

export const campaignsApi = {
  list: () => api.get<{ campaigns: Campaign[] }>('/campaigns/'),
  get: (id: string) => api.get<CampaignDetail>(`/campaigns/${id}`),
  create: (payload: CampaignPayload) => api.post<{ campaign: Campaign }>('/campaigns/', payload),
  update: (id: string, payload: CampaignPayload) =>
    api.patch<{ campaign: Campaign }>(`/campaigns/${id}`, payload),
  remove: (id: string) => api.delete(`/campaigns/${id}`),

  send: (id: string, payload: { scheduled_at?: string } = {}) =>
    api.post<{ campaign: Campaign }>(`/campaigns/${id}/send`, payload),
  pause: (id: string) => api.post<{ campaign: Campaign }>(`/campaigns/${id}/pause`),
  resume: (id: string) => api.post<{ campaign: Campaign }>(`/campaigns/${id}/resume`),
  cancel: (id: string) => api.post<{ campaign: Campaign }>(`/campaigns/${id}/cancel`),
  duplicate: (id: string) => api.post<{ campaign: Campaign }>(`/campaigns/${id}/duplicate`),
  messages: (id: string, params: { limit?: number; offset?: number } = {}) =>
    api.get<{ messages: CampaignMessage[] }>(`/campaigns/${id}/messages`, { params }),
  analytics: (id: string) => api.get<CampaignAnalytics>(`/campaigns/${id}/analytics`),
}

// TrackedLink mirrors internal/models/campaign.TrackedLink.
export interface TrackedLink {
  id: string
  campaign_id: string
  original_url: string
  hash: string
  click_count: number
  created_at: string
}

// SeriesPoint is one day's event tally. Series reach back only as far
// as tracking-event retention.
export interface SeriesPoint {
  day: string
  count: number
}

export interface CampaignAnalytics {
  links: TrackedLink[]
  open_series: SeriesPoint[]
  click_series: SeriesPoint[]
}
