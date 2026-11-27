import { describe, expect, it } from 'vitest'
import { isPainuoyunEnterprisePortal } from '../enterpriseBranding'

describe('派诺云企业端路径边界', () => {
  it.each([
    ['/enterprise/login', true],
    ['/enterprise', true],
    ['/enterprise/admin/workbench', true],
    ['/admin/enterprises', false],
    ['/dashboard', false],
    ['/enterprise-login', false],
  ])('仅识别 %s', (pathname, expected) => {
    expect(isPainuoyunEnterprisePortal(pathname)).toBe(expected)
  })
})
