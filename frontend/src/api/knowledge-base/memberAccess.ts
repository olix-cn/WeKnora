import { get, put } from '@/utils/request'

export type KBAccessMode = 'inherit' | 'restricted'
export type KBMemberPermission = 'viewer' | 'editor' | 'admin'
export interface KBMemberGrant { user_id: string; permission: KBMemberPermission }
export interface KBAccessPolicy {
  mode: KBAccessMode
  version: number
  members: KBMemberGrant[]
  updated_at?: string
}
export interface KBMemberCandidate {
  user_id: string
  username: string
  role: 'owner' | 'admin' | 'contributor' | 'viewer'
}

// Human-only policy APIs. The backend always rechecks current membership and
// permissions; frontend visibility is not an authorization boundary.
export function getKBAccessPolicy(kbId: string) {
  return get<{ success: boolean; data: KBAccessPolicy }>(
    `/api/v1/knowledge-bases/${encodeURIComponent(kbId)}/member-access`)
}

export function replaceKBAccessPolicy(kbId: string, policy: KBAccessPolicy) {
  return put<{ success: boolean; data: KBAccessPolicy }>(
    `/api/v1/knowledge-bases/${encodeURIComponent(kbId)}/member-access`, {
      mode: policy.mode,
      expected_version: policy.version,
      members: policy.members.map(({ user_id, permission }) => ({ user_id, permission })),
    })
}

export function searchKBAccessMembers(kbId: string, search: string, page = 1) {
  return get<{ success: boolean; data: KBMemberCandidate[]; total: number }>(
    `/api/v1/knowledge-bases/${encodeURIComponent(kbId)}/member-access/candidates`, {
      params: { q: search, page, page_size: 20 },
    })
}
