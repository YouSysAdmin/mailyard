import api from './client'
import type { Project } from './types'

// Platform-wide usage plan. Every limit uses zero for unlimited.
export interface Plan {
  id: string
  name: string
  description?: string
  is_default: boolean
  hourly_email_limit: number
  daily_email_limit: number
  max_api_keys: number
  max_smtp_servers: number
  max_domains: number
  max_subscribers: number
  max_sandbox_messages: number
  max_sandbox_retention_days: number
  created_at: string
  updated_at?: string
}

export interface PlanPayload {
  name: string
  description: string
  is_default: boolean
  hourly_email_limit: number
  daily_email_limit: number
  max_api_keys: number
  max_smtp_servers: number
  max_domains: number
  max_subscribers: number
  max_sandbox_messages: number
  max_sandbox_retention_days: number
}

export interface PlanUsage {
  sandbox_messages: number
  emails_last_hour: number
  emails_last_day: number
  api_keys: number
  smtp_servers: number
  domains: number
  subscribers: number
}

// GET /api/usage response. A missing plan means unlimited - no
// default plan exists and the project has no explicit assignment.
export interface UsageReport {
  usage: PlanUsage
  plan?: Plan
}

export const plansApi = {
  list: () => api.get<{ plans: Plan[] }>('/admin/plans/'),
  create: (payload: PlanPayload) => api.post<{ plan: Plan }>('/admin/plans/', payload),
  update: (id: string, payload: PlanPayload) =>
    api.patch<{ plan: Plan }>(`/admin/plans/${id}`, payload),
  remove: (id: string) => api.delete(`/admin/plans/${id}`),
  // Assigns a plan to a project. An empty plan_id clears the
  // assignment so the project falls back to the default plan.
  assign: (projectId: string, planId: string) =>
    api.patch<{ project: Project }>(`/admin/projects/${projectId}/plan`, { plan_id: planId }),
  // Usage for the active project (X-Mailyard-Project-Id header).
  // Not /admin/usage. This one reports on the project named by the
  // header - it is a tenant read gated on analytics:read, unlike the
  // plan CRUD above it. A sweep that moved this file onto /api/v1
  // prefixed every path in it, and a 404 popup on the project
  // settings page was the only symptom.
  usage: () => api.get<UsageReport>('/usage'),
}
