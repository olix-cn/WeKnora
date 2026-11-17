export const DOCUMENT_CENTER_TABS = ['files', 'templates', 'tasks'] as const
export type DocumentCenterTab = typeof DOCUMENT_CENTER_TABS[number]

export function newGenerationRequestKey(): string {
  const bytes = globalThis.crypto.getRandomValues(new Uint8Array(16))
  return `document-${Array.from(bytes, b => b.toString(16).padStart(2, '0')).join('')}`
}

export function parseDocumentCenterTab(value: unknown): DocumentCenterTab {
  return DOCUMENT_CENTER_TABS.includes(value as DocumentCenterTab) ? value as DocumentCenterTab : 'files'
}

// New deployment endpoints must positively advertise a capability. An old
// backend, failed request or malformed payload must not show enabled actions.
export interface DocumentTemplate {
  id: string
  version: number
  format: 'xlsx'
  builtin: boolean
  preview_available: boolean
}

export function parseDocumentCatalog(value: unknown): DocumentTemplate[] {
  if (!value || typeof value !== 'object') return []
  const rows = (value as { templates?: unknown }).templates
  if (!Array.isArray(rows)) return []
  return rows.filter((row): row is DocumentTemplate => !!row && typeof row === 'object'
    && row.id === 'engineering_bom_v1' && row.version === 1 && row.format === 'xlsx'
    && row.builtin === true && row.preview_available === true)
}
