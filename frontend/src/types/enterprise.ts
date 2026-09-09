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
  title: string
  body: string
  slogan: string
  background_url: string
  background_content_type: string
  background_sha256: string
  background_size_bytes: number
}

export interface EmployeeCreateInput {
  email: string
  initial_password: string
  department_id: number | null
}

export interface EmployeeUpdateInput {
  status: 'active' | 'disabled'
  department_id: number | null
}
