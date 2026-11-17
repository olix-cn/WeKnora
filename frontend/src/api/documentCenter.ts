import { get, post, put, del } from '@/utils/request'

export interface DocumentSource {
  session_id: string
  message_id: string
  result_id: string
  row_count: number
  column_count: number
  partial: boolean
}
export interface DocumentDataset extends DocumentSource {
  id: string; expires_at: string; issues: string[]; fields: { id: string; title: string }[]
}
export interface CustomTemplate {
  id: string; name: string; family_id: string; version: number; status: string; created_at: string
  can_manage: boolean
}
export interface TemplateLayout {
  sheet: string; header_row: number; start_row: number
  fields: { key: string; label: string; column: string }[]
}
export interface TemplateInspection {
  sheets: string[]; cell_count: number; header_candidates: TemplateLayout[]
}
export interface GenerationTask {
  id: string; dataset_id: string; template_id: string; status: string; partial: boolean
  file_name: string; file_size: number; created_at: string; expires_at: string; error_code?: string
}

export function getDocumentTemplates() {
  return get<{ success: boolean; data: unknown }>('/api/v1/document-center/templates')
}

export function downloadBOMTemplatePreview() {
  return get<Blob>('/api/v1/document-center/templates/engineering_bom_v1/preview', { responseType: 'blob' })
}

export function listDocumentSources(sessionId: string) {
  return get<{ success: boolean; data: DocumentSource[] }>('/api/v1/document-center/sources', {
    params: { session_id: sessionId },
  })
}
export function importDocumentDataset(source: DocumentSource) {
  return post<{ success: boolean; data: DocumentDataset }>('/api/v1/document-center/datasets/import', {
    session_id: source.session_id, message_id: source.message_id, result_id: source.result_id,
  })
}
export function generateDocument(datasetId: string, requestKey: string, acceptPartial: boolean,
  templateId = 'engineering_bom_v1', bindings?: Record<string, string>) {
  return post<{ success: boolean; data: GenerationTask }>('/api/v1/document-center/tasks', {
    dataset_id: datasetId, template_id: templateId, request_key: requestKey, accept_partial: acceptPartial,
    ...(bindings ? { bindings } : {}),
  }, { timeout: 45000 })
}

export function listCustomTemplates() {
  return get<{ success: boolean; data: CustomTemplate[] }>('/api/v1/document-center/custom-templates')
}
export function uploadCustomTemplate(name: string, file: File, baseVersionId = '') {
  const form = new FormData(); form.append('name', name); form.append('file', file)
  if (baseVersionId) form.append('base_version_id', baseVersionId)
  return post<{ success: boolean; data: CustomTemplate; inspection: TemplateInspection }>(
    '/api/v1/document-center/custom-templates', form, { headers: { 'Content-Type': 'multipart/form-data' } })
}
export function inspectCustomTemplate(id: string) {
  return get<{ success: boolean; data: TemplateInspection }>(
    `/api/v1/document-center/custom-templates/${encodeURIComponent(id)}/inspection`)
}
export function previewCustomTemplate(id: string) {
  return get<Blob>(`/api/v1/document-center/custom-templates/${encodeURIComponent(id)}/preview`, { responseType: 'blob' })
}
export function getTemplateLayout(id: string) {
  return get<{ success: boolean; data: TemplateLayout }>(`/api/v1/document-center/custom-templates/${encodeURIComponent(id)}/layout`)
}
export function configureCustomTemplate(id: string, layout: TemplateLayout) {
  return put(`/api/v1/document-center/custom-templates/${encodeURIComponent(id)}/layout`, layout)
}
export function testCustomTemplate(id: string) {
  return post<Blob>(`/api/v1/document-center/custom-templates/${encodeURIComponent(id)}/test`, {},
    { responseType: 'blob', timeout: 45000 })
}
export function publishCustomTemplate(id: string) {
  return post(`/api/v1/document-center/custom-templates/${encodeURIComponent(id)}/publish`, { sample_cleared: true })
}
export function disableCustomTemplate(id: string) {
  return post(`/api/v1/document-center/custom-templates/${encodeURIComponent(id)}/disable`, {})
}
export function listGenerationTasks(page = 1) {
  return get<{ success: boolean; data: GenerationTask[]; total: number }>('/api/v1/document-center/tasks', {
    params: { page, page_size: 30 },
  })
}
export function downloadGenerationFile(taskId: string) {
  return get<Blob>(`/api/v1/document-center/tasks/${encodeURIComponent(taskId)}/file`, { responseType: 'blob' })
}
export function deleteGenerationFile(taskId: string) {
  return del(`/api/v1/document-center/tasks/${encodeURIComponent(taskId)}/file`)
}
