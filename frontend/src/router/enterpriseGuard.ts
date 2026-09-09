import type { EnterpriseRole } from '@/types/enterprise'

export interface EnterpriseRouteAccess {
  requiresEnterpriseAuth?: boolean
  enterpriseRole?: 'admin' | 'employee'
}

export interface EnterpriseAuthSnapshot {
  authenticated: boolean
  role?: EnterpriseRole
  forcePasswordChange: boolean
}

export function enterpriseHome(role?: EnterpriseRole): string {
  return role === 'enterprise_admin' ? '/enterprise/admin/employees' : '/enterprise/sessions'
}

export function resolveEnterpriseNavigation(meta: EnterpriseRouteAccess, path: string, auth: EnterpriseAuthSnapshot): string | null {
  if (!meta.requiresEnterpriseAuth) {
    return path === '/enterprise/login' && auth.authenticated
      ? auth.forcePasswordChange ? '/enterprise/change-password' : enterpriseHome(auth.role)
      : null
  }
  if (!auth.authenticated) return `/enterprise/login?redirect=${encodeURIComponent(path)}`
  if (auth.forcePasswordChange && path !== '/enterprise/change-password') return '/enterprise/change-password'
  if (!auth.forcePasswordChange && path === '/enterprise/change-password') return enterpriseHome(auth.role)
  if (meta.enterpriseRole === 'admin' && auth.role !== 'enterprise_admin') return '/enterprise/sessions'
  return null
}
