import api from './client'

// PlatformSetting is one registry entry with its effective value.
// `overridden` distinguishes an administrator's choice from the
// built-in default.
export interface PlatformSetting {
  key: string
  // Mirrors internal/models/setting. `list` is several values in one
  // setting, sent as a JSON array and edited one per line - the page
  // renders its control from this field, which is why the type lives in
  // the registry rather than as a key the console recognises by name.
  type: 'string' | 'int' | 'bool' | 'list'
  default: string
  description: string
  value: string
  overridden: boolean
  unit?: string
  // The console page that OWNS this setting, and its name. Present on
  // the seven keys with a purpose-built editor - the ACME four and the
  // three listener assignments - which this page then shows read-only
  // rather than offering a second, worse control for. Sent by the
  // server so the console keeps no list of its own.
  managed_at?: string
  managed_in?: string
  // The build this setting only does something in, when it is not both.
  // Compared against the edition the server reports on /auth/info: the
  // key stays stored and settable, but a control that governs nothing
  // in the running build says so rather than pretending.
  edition?: string
  updated_at?: string
  updated_by?: string
}

export interface ScheduledJob {
  name: string
  schedule: string
  running: boolean
  last_run_at: string | null
  last_error: string
  next_run_at: string | null
  last_duration_ms: number
}

export const settingsApi = {
  list: () => api.get<{ settings: PlatformSetting[] }>('/admin/settings'),
  update: (settings: Array<{ key: string; value: string }>) =>
    api.put<{ settings: PlatformSetting[] }>('/admin/settings', { settings }),
  jobs: () => api.get<{ jobs: ScheduledJob[] }>('/admin/jobs'),
  runJob: (name: string) => api.post<{ jobs: ScheduledJob[] }>(`/admin/jobs/${name}/run`),
}
