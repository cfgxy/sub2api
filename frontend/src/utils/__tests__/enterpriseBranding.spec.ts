import { describe, expect, it } from 'vitest'
import { PAINUOYUN_ENTERPRISE_FAVICON, isPainuoyunEnterprisePortal } from '../enterpriseBranding'

describe('派诺云企业端路径边界', () => {
  it.each([
    ['/enterprise/login', true],
    ['/enterprise', true],
    ['/enterprise/', true],
    ['/enterprise/admin/workbench', true],
    ['/admin/enterprises', false],
    ['/dashboard', false],
    ['/enterprise-login', false],
  ])('仅识别 %s', (pathname, expected) => {
    expect(isPainuoyunEnterprisePortal(pathname)).toBe(expected)
  })

  it('exposes the enterprise-specific favicon for route-transition callers', () => {
    expect(PAINUOYUN_ENTERPRISE_FAVICON).toBe('/brand/painuoyun-icon.svg')
  })
})
