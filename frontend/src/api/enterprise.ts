import axios, { type AxiosError, type InternalAxiosRequestConfig } from 'axios'
import { getAPIBaseURL } from './url'
import type {
  EmployeeCreateInput,
  EmployeeUpdateInput,
  EnterpriseBrand,
  EnterpriseBrandUpdate,
  EnterpriseAllocationSummary,
  EnterpriseDepartment,
  EnterpriseEmployee,
  EnterpriseEmployeeKey,
  EnterpriseEmployeeKeyMutationResult,
  EnterpriseEmployeeHome,
  EnterpriseEmployeeUsageSummary,
  EnterpriseEmployeeUsageRecord,
  EnterpriseKeySummary,
  EnterprisePrincipal,
  EnterprisePaginated,
  EnterpriseSession,
  EnterpriseTokenPair,
  EnterpriseWorkbenchAuditEvent,
  EnterpriseWorkbenchSummary,
  EnterpriseWorkbenchUsageRow,
} from '@/types/enterprise'

const ACCESS_KEY = 'enterprise_access_token'
const REFRESH_KEY = 'enterprise_refresh_token'
const PRINCIPAL_KEY = 'enterprise_principal'
const enterpriseKeyMutationPrefix = 'sub2api:enterprise:key-mutation:'
let refreshPromise: Promise<EnterpriseTokenPair> | null = null
const sessionListeners = new Set<(pair: EnterpriseTokenPair) => void>()
const enterpriseKeyMutationKeys = new Map<string, string>()

export type EnterpriseSessionState =
  | 'session-expired'
  | 'employee-disabled'
  | 'enterprise-disabled'
  | 'source-unavailable'
  | 'forbidden'
  | 'not-found'
  | 'cross-enterprise'

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}

export function enterpriseSessionStateForError(error: unknown): EnterpriseSessionState | null {
  if (!isRecord(error)) return null
  const response = isRecord(error.response) ? error.response : null
  const responseData = response && isRecord(response.data) ? response.data : null
  const data = responseData || (isRecord(error) ? error : null)
  const reason = typeof data?.reason === 'string' ? data.reason : ''
  if (reason === 'ENTERPRISE_HOST_MISMATCH') return 'cross-enterprise'
  if (reason === 'ENTERPRISE_DISABLED') return 'enterprise-disabled'
  if (reason === 'ENTERPRISE_PRINCIPAL_INACTIVE') return 'employee-disabled'
  if (reason === 'ENTERPRISE_KEY_NOT_FOUND') return null

  const status = typeof response?.status === 'number' ? response.status : 0
  if (status === 401) return 'session-expired'
  if (status === 403) return 'forbidden'
  if (status === 404) return 'not-found'
  if (status >= 500) return 'source-unavailable'
  if (status === 0 && axios.isAxiosError(error) && !response) return 'source-unavailable'
  return null
}

export function onEnterpriseAuthSession(listener: (pair: EnterpriseTokenPair) => void) {
  sessionListeners.add(listener)
  return () => sessionListeners.delete(listener)
}

export function syncEnterpriseAuthSession(pair: EnterpriseTokenPair) {
  if (!sameEnterprisePrincipal(readEnterprisePrincipal(), pair.principal)) clearEnterpriseKeyMutationRetryState()
  localStorage.setItem(ACCESS_KEY, pair.access_token)
  localStorage.setItem(REFRESH_KEY, pair.refresh_token)
  localStorage.setItem(PRINCIPAL_KEY, JSON.stringify(pair.principal))
  sessionListeners.forEach((listener) => listener(pair))
}

export const enterpriseClient = axios.create({
  baseURL: getAPIBaseURL(),
  timeout: 30000,
  headers: { 'Content-Type': 'application/json' },
})

enterpriseClient.interceptors.request.use((config) => {
  const token = localStorage.getItem(ACCESS_KEY)
  if (token && config.headers) config.headers.Authorization = `Bearer ${token}`
  return config
})

enterpriseClient.interceptors.response.use(
  (response) => {
    if (response.data && typeof response.data === 'object' && 'code' in response.data) {
      if (response.data.code !== 0) return Promise.reject(response.data)
      response.data = response.data.data
    }
    return response
  },
  async (error: AxiosError) => {
    const request = error.config as (InternalAxiosRequestConfig & { _enterpriseRetry?: boolean }) | undefined
    const refreshToken = localStorage.getItem(REFRESH_KEY)
    const isAuthRequest = request?.url?.includes('/enterprise/auth/')
    if (error.response?.status === 401 && request && !request._enterpriseRetry && refreshToken && !isAuthRequest) {
      request._enterpriseRetry = true
      refreshPromise ||= enterpriseAPI.refresh(refreshToken).finally(() => { refreshPromise = null })
      try {
        const pair = await refreshPromise
        syncEnterpriseAuthSession(pair)
        request.headers.Authorization = `Bearer ${pair.access_token}`
        return enterpriseClient(request)
      } catch {
        clearEnterpriseKeyMutationRetryState()
        localStorage.removeItem(ACCESS_KEY)
        localStorage.removeItem(REFRESH_KEY)
        localStorage.removeItem(PRINCIPAL_KEY)
        if (!window.location.pathname.startsWith('/enterprise/login')) window.location.href = '/enterprise/login'
      }
    }
    const data = error.response?.data as Record<string, unknown> | undefined
    const status = error.response?.status ?? 0
    const state = enterpriseSessionStateForError(error)
    if (!isAuthRequest && state && typeof window !== 'undefined' && !window.location.pathname.startsWith('/enterprise/session-states')) {
      window.location.href = `/enterprise/session-states?state=${state}`
    }
    return Promise.reject({
      status,
      code: data?.code,
      reason: data?.reason,
      message: data?.message || error.message,
    })
  },
)

const data = <T>(request: Promise<{ data: T }>) => request.then((response) => response.data)
const newIdempotencyKey = () => globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`
type EnterpriseKeyMutationOperation = 'create' | 'disable' | 'rotate'

function readEnterprisePrincipal(): EnterprisePrincipal | null {
  try {
    const raw = globalThis.localStorage?.getItem(PRINCIPAL_KEY)
    if (!raw) return null
    const principal: unknown = JSON.parse(raw)
    if (typeof principal !== 'object' || principal === null) return null
    const candidate = principal as Partial<EnterprisePrincipal>
    if (
      candidate.principal_type !== 'employee'
      || candidate.role !== 'enterprise_employee'
      || typeof candidate.enterprise_id !== 'number'
      || !Number.isSafeInteger(candidate.enterprise_id)
      || candidate.enterprise_id <= 0
      || typeof candidate.principal_id !== 'number'
      || !Number.isSafeInteger(candidate.principal_id)
      || candidate.principal_id <= 0
    ) return null
    return candidate as EnterprisePrincipal
  } catch {
    return null
  }
}

function sameEnterprisePrincipal(previous: EnterprisePrincipal | null, next: EnterprisePrincipal): boolean {
  return previous?.enterprise_id === next.enterprise_id
    && previous.principal_id === next.principal_id
    && previous.principal_type === next.principal_type
    && previous.role === next.role
}

function enterpriseKeyMutationScope(operation: EnterpriseKeyMutationOperation, expectedAPIKeyID = 0): string | null {
  const principal = readEnterprisePrincipal()
  if (!principal || (operation !== 'create' && (!Number.isSafeInteger(expectedAPIKeyID) || expectedAPIKeyID <= 0))) return null
  return `${enterpriseKeyMutationPrefix}${principal.enterprise_id}:${principal.principal_id}:${operation}:${expectedAPIKeyID}`
}

function readEnterpriseKeyMutationKey(scope: string): string | null {
  try {
    return enterpriseKeyMutationKeys.get(scope) ?? globalThis.sessionStorage?.getItem(scope) ?? null
  } catch {
    return enterpriseKeyMutationKeys.get(scope) ?? null
  }
}

function storeEnterpriseKeyMutationKey(scope: string, key: string | null): void {
  if (key) enterpriseKeyMutationKeys.set(scope, key)
  else enterpriseKeyMutationKeys.delete(scope)
  try {
    if (key) globalThis.sessionStorage?.setItem(scope, key)
    else globalThis.sessionStorage?.removeItem(scope)
  } catch {
    // 存储不可用时仍在当前页面生命周期内保留重试键。
  }
}

export function clearEnterpriseKeyMutationRetryState(): void {
  enterpriseKeyMutationKeys.clear()
  try {
    const keys = Array.from({ length: globalThis.sessionStorage?.length ?? 0 }, (_, index) => globalThis.sessionStorage?.key(index))
    for (const key of keys) {
      if (key?.startsWith(enterpriseKeyMutationPrefix)) globalThis.sessionStorage?.removeItem(key)
    }
  } catch {
    // sessionStorage 不可用时内存状态已清理。
  }
}

export function isEnterpriseKeyMutationStateConflict(error: unknown): boolean {
	if (typeof error !== 'object' || error === null) return false
	const candidate = error as { status?: unknown; code?: unknown; reason?: unknown }
	if (candidate.reason === 'ENTERPRISE_KEY_VERSION_CONFLICT'
		|| candidate.reason === 'ENTERPRISE_KEY_ALREADY_ACTIVE'
		|| candidate.reason === 'ENTERPRISE_KEY_NOT_FOUND') return true
	return candidate.status === 404 || candidate.status === 409 || candidate.code === 404 || candidate.code === 409
}

async function withEnterpriseKeyMutation<T>(
  operation: EnterpriseKeyMutationOperation,
  expectedAPIKeyID: number,
  request: (idempotencyKey: string) => Promise<T>,
): Promise<T> {
  const scope = enterpriseKeyMutationScope(operation, expectedAPIKeyID)
  if (!scope) throw new Error('authenticated enterprise employee is required for API key mutation')
  const idempotencyKey = readEnterpriseKeyMutationKey(scope) ?? newIdempotencyKey()
  storeEnterpriseKeyMutationKey(scope, idempotencyKey)
	try {
		const result = await request(idempotencyKey)
		storeEnterpriseKeyMutationKey(scope, null)
		return result
	} catch (error) {
		if (isEnterpriseKeyMutationStateConflict(error)) storeEnterpriseKeyMutationKey(scope, null)
		throw error
	}
}

const idempotencyHeaders = (idempotencyKey: string) => ({ headers: { 'Idempotency-Key': idempotencyKey } })

export const enterpriseAPI = {
  getBrand: () => data<EnterpriseBrand>(enterpriseClient.get('/enterprise/brand')),
  login: (input: { email: string; password: string }) => data<EnterpriseTokenPair>(enterpriseClient.post('/enterprise/auth/login', input)),
  refresh: (refresh_token: string) => data<EnterpriseTokenPair>(enterpriseClient.post('/enterprise/auth/refresh', { refresh_token })),
  logout: (refresh_token: string) => data<{ success: boolean }>(enterpriseClient.post('/enterprise/auth/logout', { refresh_token })),
  forgotPassword: (email: string) => data<{ success: boolean }>(enterpriseClient.post('/enterprise/auth/forgot-password', { email })),
  resetPassword: (token: string, password: string) => data<{ success: boolean }>(enterpriseClient.post('/enterprise/auth/reset-password', { token, password })),
  changeInitialPassword: (current_password: string, new_password: string) => data<{ success: boolean }>(enterpriseClient.post('/enterprise/password/first-change', { current_password, new_password })),
  changePassword: (current_password: string, new_password: string) => data<{ success: boolean }>(enterpriseClient.post('/enterprise/password/change', { current_password, new_password })),
  listSessions: () => data<EnterpriseSession[]>(enterpriseClient.get('/enterprise/sessions')),
  revokeSession: (id: string) => data<{ success: boolean }>(enterpriseClient.delete(`/enterprise/sessions/${id}`)),
  revokeAllSessions: () => data<{ success: boolean }>(enterpriseClient.delete('/enterprise/sessions')),
  getCurrentKey: () => data<EnterpriseEmployeeKey | null>(enterpriseClient.get('/enterprise/keys/current')),
  getEmployeeHome: () => data<EnterpriseEmployeeHome>(enterpriseClient.get('/enterprise/home')),
  getEmployeeUsage: () => data<EnterpriseEmployeeUsageSummary>(enterpriseClient.get('/enterprise/usage/me')),
  listEmployeeUsage: () => data<EnterpriseEmployeeUsageRecord[]>(enterpriseClient.get('/enterprise/usage/me/details')),
  getEmployeeProfile: () => data<EnterpriseEmployee>(enterpriseClient.get('/enterprise/profile')),
  setAllocation: (subscriptionId: number, employeeId: number, input: { enterprise_id: number; window_type: 'week'; window_anchor: string; credit: string; expected_version: number; reason: string }) => data<{ id: number; version: number }>(enterpriseClient.put(`/enterprise/subscriptions/${subscriptionId}/allocations/${employeeId}`, input)),
  getAllocationSummary: (subscriptionId: number, employeeId: number, params: { enterprise_id: number; window_type: 'week'; window_anchor: string }) => data<EnterpriseAllocationSummary>(enterpriseClient.get(`/enterprise/subscriptions/${subscriptionId}/allocations/${employeeId}`, { params })),
  createKey: () => withEnterpriseKeyMutation('create', 0, (idempotencyKey) => data<EnterpriseEmployeeKeyMutationResult>(
    enterpriseClient.post('/enterprise/keys', undefined, idempotencyHeaders(idempotencyKey)),
  )),
  disableKey: (expected_api_key_id: number) => withEnterpriseKeyMutation('disable', expected_api_key_id, (idempotencyKey) => data<EnterpriseEmployeeKeyMutationResult>(
    enterpriseClient.post('/enterprise/keys/disable', { expected_api_key_id }, idempotencyHeaders(idempotencyKey)),
  )),
  rotateKey: (expected_api_key_id: number) => withEnterpriseKeyMutation('rotate', expected_api_key_id, (idempotencyKey) => data<EnterpriseEmployeeKeyMutationResult>(
    enterpriseClient.post('/enterprise/keys/rotate', { expected_api_key_id }, idempotencyHeaders(idempotencyKey)),
  )),
  listDepartments: () => data<EnterpriseDepartment[]>(enterpriseClient.get('/enterprise/admin/departments')),
  createDepartment: (name: string) => data<EnterpriseDepartment>(enterpriseClient.post('/enterprise/admin/departments', { name })),
  deleteDepartment: (id: number) => data<{ success: boolean }>(enterpriseClient.delete(`/enterprise/admin/departments/${id}`)),
  listEmployees: () => data<EnterpriseEmployee[]>(enterpriseClient.get('/enterprise/admin/employees')),
  getEmployee: (id: number) => data<EnterpriseEmployee>(enterpriseClient.get(`/enterprise/admin/employees/${id}`)),
  createEmployee: (input: EmployeeCreateInput) => data<EnterpriseEmployee>(enterpriseClient.post('/enterprise/admin/employees', input)),
  updateEmployee: (id: number, input: EmployeeUpdateInput) => data<{ success: boolean }>(enterpriseClient.patch(`/enterprise/admin/employees/${id}`, input)),
  terminateEmployee: (id: number) => data<{ success: boolean }>(enterpriseClient.delete(`/enterprise/admin/employees/${id}`)),
  getAdminBrand: () => data<EnterpriseBrand>(enterpriseClient.get('/enterprise/admin/brand')),
  updateBrand: (input: EnterpriseBrandUpdate) => data<EnterpriseBrand>(enterpriseClient.put('/enterprise/admin/brand', input)),
  uploadBrandBackground: async (file: File) => {
    const bytes = await new Promise<ArrayBuffer>((resolve, reject) => {
      const reader = new FileReader()
      reader.onload = () => resolve(reader.result as ArrayBuffer)
      reader.onerror = () => reject(reader.error)
      reader.readAsArrayBuffer(file)
    })
    const digest = await crypto.subtle.digest('SHA-256', bytes)
    const sha256 = Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, '0')).join('')
    const form = new FormData()
    form.append('file', file)
    form.append('sha256', sha256)
    return data<Pick<EnterpriseBrand, 'background_url' | 'background_content_type' | 'background_sha256' | 'background_size_bytes'>>(
      enterpriseClient.post('/enterprise/admin/brand/background', form, { headers: { 'Content-Type': 'multipart/form-data' } }),
    )
  },
  listAdminKeys: () => data<EnterpriseKeySummary[]>(enterpriseClient.get('/enterprise/admin/keys')),
  revokeAdminKey: (id: number, idempotencyKey: string) => data<EnterpriseEmployeeKeyMutationResult>(enterpriseClient.post(`/enterprise/admin/keys/${id}/revoke`, undefined, idempotencyHeaders(idempotencyKey))),
  getWorkbenchSummary: (params?: Record<string, string | number>) => data<EnterpriseWorkbenchSummary>(enterpriseClient.get('/enterprise/admin/workbench/summary', { params })),
  listWorkbenchUsage: (params?: Record<string, string | number>) => data<EnterprisePaginated<EnterpriseWorkbenchUsageRow>>(enterpriseClient.get('/enterprise/admin/workbench/usage', { params })),
  listWorkbenchAuditEvents: (params?: Record<string, string | number>) => data<EnterprisePaginated<EnterpriseWorkbenchAuditEvent>>(enterpriseClient.get('/enterprise/admin/workbench/audit-events', { params })),
}
