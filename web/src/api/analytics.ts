import api from './client'

// DayCount mirrors internal/models/analytics.DayCount. Days with no
// mail are present with a zero count, so a chart's x-axis is stable.
export interface DayCount {
  date: string
  count: number
}

export interface TrendResponse {
  daily_counts: DayCount[]
  status_breakdown: Record<string, number>
  from: string
  to: string
}

export interface TrendParams {
  from?: string
  to?: string
  status?: string
}

// Engagement mirrors internal/models/analytics.Engagement.
//
// tracked_sent is the DENOMINATOR of both rates, and it is reported so a
// page can say what the percentage is out of. Mail sent with tracking off
// can never register an open, so it is excluded - dividing by every send
// would make a correctly configured project look ignored, and one
// transactional message opting out would move the rate for a reason
// nobody could see.
export interface Engagement {
  tracked_sent: number
  opened: number
  clicked: number
  open_rate: number
  click_rate: number
}

export interface DashboardStats {
  emails: Record<string, number>
  total_emails: number
  failure_rate: number
  inbound: Record<string, number>
  resources: Record<string, number>
  engagement: Engagement
}

export const analyticsApi = {
  // Delivery trend over a date range, optionally narrowed to one
  // status. Defaults to the trailing 30 days server-side.
  trend: (params: TrendParams = {}) => api.get<TrendResponse>('/analytics', { params }),
  // The project readout in one call. The dashboard predated this
  // endpoint and assembled its own numbers from /emails/stats, which is
  // why the engagement figures it computes were visible nowhere.
  dashboard: () => api.get<{ stats: DashboardStats }>('/dashboard/stats'),
}
