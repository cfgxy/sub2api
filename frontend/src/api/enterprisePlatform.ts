import { apiClient } from './client'

export interface PlatformEnterprise {
  id: number
  name: string
  portal_host: string
  dedicated_upstream_user_id: number
  status: 'active' | 'disabled'
  created_at: string
}

export const enterprisePlatformAPI = {
  list: (params?: { search?: string; status?: string }) => apiClient.get<PlatformEnterprise[]>('/admin/enterprises', { params }).then((response) => response.data),
  get: (id: number) => apiClient.get<PlatformEnterprise>(`/admin/enterprises/${id}`).then((response) => response.data),
  create: (input: { name: string; portal_host: string; dedicated_upstream_user_id: number; reason: string }) => apiClient.post<PlatformEnterprise>('/admin/enterprises', input).then((response) => response.data),
  disable: (id: number, reason: string) => apiClient.post<{ success: boolean }>(`/admin/enterprises/${id}/disable`, { reason }).then((response) => response.data),
}
