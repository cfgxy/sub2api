import axios, { type AxiosError, type InternalAxiosRequestConfig } from 'axios'
import { getAPIBaseURL } from './url'
import type {
  EmployeeCreateInput,
  EmployeeUpdateInput,
  EnterpriseBrand,
  EnterpriseBrandUpdate,
  EnterpriseDepartment,
  EnterpriseEmployee,
  EnterpriseSession,
  EnterpriseTokenPair,
} from '@/types/enterprise'

const ACCESS_KEY = 'enterprise_access_token'
const REFRESH_KEY = 'enterprise_refresh_token'
let refreshPromise: Promise<EnterpriseTokenPair> | null = null
const sessionListeners = new Set<(pair: EnterpriseTokenPair) => void>()

export function onEnterpriseAuthSession(listener: (pair: EnterpriseTokenPair) => void) {
  sessionListeners.add(listener)
  return () => sessionListeners.delete(listener)
}

export function syncEnterpriseAuthSession(pair: EnterpriseTokenPair) {
  localStorage.setItem(ACCESS_KEY, pair.access_token)
  localStorage.setItem(REFRESH_KEY, pair.refresh_token)
  localStorage.setItem('enterprise_principal', JSON.stringify(pair.principal))
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
        localStorage.removeItem(ACCESS_KEY)
        localStorage.removeItem(REFRESH_KEY)
        localStorage.removeItem('enterprise_principal')
        if (!window.location.pathname.startsWith('/enterprise/login')) window.location.href = '/enterprise/login'
      }
    }
    const data = error.response?.data as Record<string, unknown> | undefined
    return Promise.reject({
      status: error.response?.status ?? 0,
      code: data?.code,
      message: data?.message || error.message,
    })
  },
)

const data = <T>(request: Promise<{ data: T }>) => request.then((response) => response.data)

export const enterpriseAPI = {
  getBrand: () => data<EnterpriseBrand>(enterpriseClient.get('/enterprise/brand')),
  login: (input: { email: string; password: string }) => data<EnterpriseTokenPair>(enterpriseClient.post('/enterprise/auth/login', input)),
  refresh: (refresh_token: string) => data<EnterpriseTokenPair>(enterpriseClient.post('/enterprise/auth/refresh', { refresh_token })),
  logout: (refresh_token: string) => data<{ success: boolean }>(enterpriseClient.post('/enterprise/auth/logout', { refresh_token })),
  forgotPassword: (email: string) => data<{ success: boolean }>(enterpriseClient.post('/enterprise/auth/forgot-password', { email })),
  resetPassword: (token: string, password: string) => data<{ success: boolean }>(enterpriseClient.post('/enterprise/auth/reset-password', { token, password })),
  changeInitialPassword: (current_password: string, new_password: string) => data<{ success: boolean }>(enterpriseClient.post('/enterprise/password/first-change', { current_password, new_password })),
  listSessions: () => data<EnterpriseSession[]>(enterpriseClient.get('/enterprise/sessions')),
  revokeSession: (id: string) => data<{ success: boolean }>(enterpriseClient.delete(`/enterprise/sessions/${id}`)),
  revokeAllSessions: () => data<{ success: boolean }>(enterpriseClient.delete('/enterprise/sessions')),
  listDepartments: () => data<EnterpriseDepartment[]>(enterpriseClient.get('/enterprise/admin/departments')),
  createDepartment: (name: string) => data<EnterpriseDepartment>(enterpriseClient.post('/enterprise/admin/departments', { name })),
  deleteDepartment: (id: number) => data<{ success: boolean }>(enterpriseClient.delete(`/enterprise/admin/departments/${id}`)),
  listEmployees: () => data<EnterpriseEmployee[]>(enterpriseClient.get('/enterprise/admin/employees')),
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
}
