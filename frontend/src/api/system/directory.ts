import { get, post, put, del } from '@/utils/request'
export interface Department {
  id: string; parent_id: string | null; name: string; code: string; sort_order: number
  leader_id: string | null; version: number; member_count: number
}
export interface Personnel {
  id: string; username: string; email: string; display_name: string; department_id: string | null
  department_name: string; version: number; is_active: boolean; is_system_admin: boolean
  tenant_id: number; created_at: string
}
export interface DepartmentInput {
  name: string; code?: string; parent_id: string | null; sort_order: number
  leader_id: string | null; expected_version: number
}
export const getDirectory = () => get<{ directory: { name: string; version: number }; departments: Department[] }>('/api/v1/system/admin/directory')
export const listPersonnel = (params: Record<string, unknown>) => get<{ users: Personnel[]; total: number; has_more: boolean; offset: number; limit: number }>('/api/v1/system/admin/directory/members', { params })
export const saveDepartment = (id: string, input: DepartmentInput) => id
  ? put<Department>(`/api/v1/system/admin/directory/departments/${encodeURIComponent(id)}`, input)
  : post<Department>('/api/v1/system/admin/directory/departments', input)
export const deleteDepartment = (id: string, version: number) => del(`/api/v1/system/admin/directory/departments/${encodeURIComponent(id)}`, { expected_version: version })
export const movePersonnel = (departmentId: string | null, members: Array<{ user_id: string; expected_version: number }>) => put('/api/v1/system/admin/directory/members/department', { department_id: departmentId, members })

export const updatePersonnel = (id: string, name: string, version: number) => put(`/api/v1/system/admin/directory/members/${encodeURIComponent(id)}`, { display_name: name, expected_version: version })
