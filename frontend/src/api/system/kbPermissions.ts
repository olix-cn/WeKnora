import { get } from '@/utils/request'
export interface PlatformKBPermissionRow {
  id: string; name: string; tenant_id: number; tenant_name: string; creator_id: string
  mode: 'inherit' | 'restricted'; version: number; member_count: number
}
export const listKBPermissions = (q: string, offset = 0) => get<{ data: PlatformKBPermissionRow[]; total: number }>(
  '/api/v1/system/admin/knowledge-base-permissions', { params: { q, offset, limit: 20 } })
