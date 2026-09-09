import { describe, expect, it } from 'vitest'
import { resolveEnterpriseNavigation } from '@/router/enterpriseGuard'

describe('enterprise route guard', () => {
  it('forces employees with an initial password onto the change page', () => {
    expect(resolveEnterpriseNavigation(
      { requiresEnterpriseAuth: true, enterpriseRole: 'employee' },
      '/enterprise/sessions',
      { authenticated: true, role: 'enterprise_employee', forcePasswordChange: true },
    )).toBe('/enterprise/change-password')
  })

  it('keeps enterprise employees out of admin routes', () => {
    expect(resolveEnterpriseNavigation(
      { requiresEnterpriseAuth: true, enterpriseRole: 'admin' },
      '/enterprise/admin/employees',
      { authenticated: true, role: 'enterprise_employee', forcePasswordChange: false },
    )).toBe('/enterprise/sessions')
  })
})
