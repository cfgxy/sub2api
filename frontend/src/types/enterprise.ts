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

export interface EnterpriseWorkbenchSummary {
  total_usage_credit: string
  total_requests: number
  employee_count: number
  active_employee_count: number
  subscription_status: string
  subscription_plan: string
  enterprise_pool_limit: string
  enterprise_pool_used: string
  enterprise_pool_remaining: string
  enterprise_pool_exhausted: boolean
  pool_source_status: 'available' | 'unavailable'
  pool_source: string
  pool_window_type: string
  pool_window_anchor?: string
  employee_summaries: EnterpriseWorkbenchEmployeeSummary[]
  usage_trend: EnterpriseWorkbenchUsageTrendPoint[]
}

export interface EnterpriseWorkbenchEmployeeSummary {
  employee_id: number
  email: string
  department_id?: number | null
  requests: number
  usage_credit: string
  configured_credit?: string
  remaining_credit: string
  overage_credit: string
  recommendation: string
}

export interface EnterpriseWorkbenchUsageTrendPoint {
  at: string
  requests: number
  usage_credit: string
}

export interface EnterpriseWorkbenchUsageRow {
  attribution_id: number
  usage_log_id: number
  employee_id?: number | null
  employee_email?: string
  department_id?: number | null
  api_key_id: number
  api_key_masked: string
  window_type: 'day' | 'week' | 'month'
  window_anchor: string
  request_at: string
  classification: 'employee' | 'controlled_external'
  assignment_generation: number
  usage_credit: string
  configured_credit: string
}

export interface EnterpriseWorkbenchAuditEvent {
  id: number
  event_type: string
  entity_type: string
  entity_id?: number | null
  result: string
  reason?: string
  payload: Record<string, unknown>
  actor_ref: string
  created_at: string
}

export interface EnterprisePaginated<T> {
  items: T[]
  total: number
  page: number
  page_size: number
  pages: number
}
