import api, { browserURL } from './client'
import type { Template, TemplateLocalization, TemplateVersion } from './types'

export interface TemplatePayload {
  name?: string
  description?: string
  default_language?: string
  sample_data?: string
}

export interface LocalizationPayload {
  language: string
  subject: string
  html?: string
  text?: string
}

// Ad-hoc preview - subject is required by the backend.
export interface PreviewPayload {
  subject: string
  html?: string
  text?: string
  css?: string
  data?: Record<string, unknown>
}

export interface RenderedPreview {
  subject: string
  html: string
  text: string
}

// POST /api/templates/:id/send-test body (from is required).
export interface SendTestPayload {
  from: string
  to: string[]
  language?: string
  data?: Record<string, unknown>
}

// Template attachment metadata - list responses never carry file bytes.
export interface TemplateAttachment {
  id: string
  project_id: string
  template_id: string
  filename: string
  content_type?: string
  size: number
  storage_key?: string
  created_at: string
}

// POST /api/templates/:id/attachments body - content is raw base64.
export interface AttachmentUploadPayload {
  filename: string
  content_type?: string
  content: string
}

// Portable export document (format mailyard-template-v1).
export interface TemplateExportVersion {
  version: number
  active: boolean
  sample_data?: string
  stylesheet?: { name: string; css?: string }
  localizations?: Array<{ language: string; subject: string; html?: string; text?: string }>
}

export interface TemplateExportDoc {
  format: string
  template: {
    name: string
    description?: string
    default_language?: string
    sample_data?: string
  }
  versions?: TemplateExportVersion[]
}

export const templatesApi = {
  list: () => api.get<{ templates: Template[] }>('/templates/'),
  get: (id: string) =>
    api.get<{ template: Template; versions: TemplateVersion[] }>(`/templates/${id}`),
  create: (payload: TemplatePayload) => api.post<{ template: Template }>('/templates/', payload),
  update: (id: string, payload: TemplatePayload) =>
    api.patch<{ template: Template }>(`/templates/${id}`, payload),
  remove: (id: string) => api.delete(`/templates/${id}`),

  listVersions: (id: string) =>
    api.get<{ versions: TemplateVersion[] }>(`/templates/${id}/versions`),
  createVersion: (id: string, payload: { stylesheet_id?: string; sample_data?: string }) =>
    api.post<{ version: TemplateVersion }>(`/templates/${id}/versions`, payload),
  updateVersion: (
    id: string,
    versionId: string,
    payload: { stylesheet_id?: string; sample_data?: string },
  ) => api.patch<{ version: TemplateVersion }>(`/templates/${id}/versions/${versionId}`, payload),
  deleteVersion: (id: string, versionId: string) =>
    api.delete(`/templates/${id}/versions/${versionId}`),
  activate: (id: string, versionId: string) =>
    api.post<{ active_version_id: string }>(`/templates/${id}/activate/${versionId}`),

  listLocalizations: (id: string, versionId: string) =>
    api.get<{ localizations: TemplateLocalization[] }>(
      `/templates/${id}/versions/${versionId}/localizations`,
    ),
  putLocalization: (id: string, versionId: string, payload: LocalizationPayload) =>
    api.put<{ localization: TemplateLocalization }>(
      `/templates/${id}/versions/${versionId}/localizations`,
      payload,
    ),
  deleteLocalization: (id: string, localizationId: string) =>
    api.delete(`/templates/${id}/localizations/${localizationId}`),

  preview: (payload: PreviewPayload) =>
    api.post<{ preview: RenderedPreview }>('/templates/preview', payload),
  previewVersion: (
    id: string,
    versionId: string,
    payload: { language?: string; data?: Record<string, unknown> },
  ) =>
    api.post<{ preview: RenderedPreview; language: string }>(
      `/templates/${id}/versions/${versionId}/preview`,
      payload,
    ),
  sendTest: (id: string, payload: SendTestPayload) =>
    api.post(`/templates/${id}/send-test`, payload),

  listAttachments: (id: string) =>
    api.get<{ attachments: TemplateAttachment[] }>(`/templates/${id}/attachments`),
  uploadAttachment: (id: string, payload: AttachmentUploadPayload) =>
    api.post<{ attachment: TemplateAttachment }>(`/templates/${id}/attachments`, payload),
  deleteAttachment: (id: string, attId: string) =>
    api.delete(`/templates/${id}/attachments/${attId}`),
  attachmentDownloadURL: (id: string, attId: string) =>
    browserURL(`/templates/${id}/attachments/${attId}/download`),

  exportURL: (id: string) => browserURL(`/templates/${id}/export`),
  export: (id: string) => api.get<{ export: TemplateExportDoc }>(`/templates/${id}/export`),
  import: (payload: TemplateExportDoc) =>
    api.post<{ template: Template }>('/templates/import', payload),
}
