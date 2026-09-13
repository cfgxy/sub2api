export type EnterpriseRole = 'enterprise_admin' | 'enterprise_employee'

export interface EnterprisePrincipal {
  enterprise_id: number
  principal_type: 'admin' | 'employee'
  principal_id: number
  email: string
  role: EnterpriseRole
  force_password_change: boolean
}

export interface EnterpriseTokenPair {
  access_token: string
  refresh_token: string
  token_type: 'Bearer'
  expires_in: number
  principal: EnterprisePrincipal
}

export interface EnterpriseSession {
  id: string
  user_agent: string
  ip_address: string
  last_seen_at: string
  created_at: string
  expires_at: string
  revoked_at?: string
  current: boolean
}

export interface EnterpriseDepartment { id: number; name: string }

export interface EnterpriseEmployee {
  id: number
  email: string
  status: 'active' | 'disabled' | 'terminated'
  department_id?: number | null
  must_change_password: boolean
  terminated_at?: string
}

export interface EnterpriseBrand {
  enterprise_name: string
  title: string
  body: string
  slogan: string
  background_url: string
  background_content_type: string
  background_sha256: string
  background_size_bytes: number
}

export type EnterpriseBrandUpdate = Pick<EnterpriseBrand, 'enterprise_name' | 'title' | 'body' | 'slogan' | 'background_url'>

export interface EmployeeCreateInput {
  email: string
  initial_password: string
  department_id: number | null
}

export interface EmployeeUpdateInput {
  status: 'active' | 'disabled'
  department_id: number | null
}

export interface EnterpriseEmployeeKey {
  id: number
  masked_key: string
  name: string
  status: 'active' | 'disabled' | 'quota_exhausted' | 'expired'
  quota: number
  quota_used: number
  group_id?: number | null
  expires_at?: string
  rate_limit_5h: number
  rate_limit_1d: number
  rate_limit_7d: number
  usage_5h: number
  usage_1d: number
  usage_7d: number
  window_5h_start?: string
  window_1d_start?: string
  window_7d_start?: string
  reset_5h_at?: string
  reset_1d_at?: string
  reset_7d_at?: string
  created_at: string
  updated_at: string
}

export interface EnterpriseEmployeeKeyMutationResult {
  key: EnterpriseEmployeeKey
  plaintext?: string
  replayed: boolean
}
