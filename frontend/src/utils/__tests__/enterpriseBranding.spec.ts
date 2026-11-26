import { describe, expect, it } from 'vitest'
import {
  isPainuoyunEnterpriseHost,
  isPainuoyunEnterprisePortal,
  PAINUOYUN_ENTERPRISE_NAME,
  replaceLegacyEnterpriseBrand,
} from '../enterpriseBranding'

describe('派诺云企业端 host 边界', () => {
  it.each([
    ['tenant.ainoriahub.com', true],
    ['TENANT.AINORIAHUB.COM.', true],
    ['ainoriahub.com', false],
    ['not-ainoriahub.com', false],
    ['tenant.ainoriahub.com.evil.example', false],
  ])('仅识别 %s', (host, expected) => {
    expect(isPainuoyunEnterpriseHost(host)).toBe(expected)
  })

  it('仅在企业路由启用派诺云资产，避免改动同 host 的个人或平台页面', () => {
    expect(isPainuoyunEnterprisePortal('tenant.ainoriahub.com', '/enterprise/login')).toBe(true)
    expect(isPainuoyunEnterprisePortal('tenant.ainoriahub.com', '/enterprise')).toBe(true)
    expect(isPainuoyunEnterprisePortal('tenant.ainoriahub.com', '/admin/enterprises')).toBe(false)
    expect(isPainuoyunEnterprisePortal('tenant.ainoriahub.com', '/dashboard')).toBe(false)
  })

  it('只在目标 host 改写运行时返回的旧品牌文本', () => {
    expect(replaceLegacyEnterpriseBrand('Sub2API 企业工作台', true)).toBe(`${PAINUOYUN_ENTERPRISE_NAME} 企业工作台`)
    expect(replaceLegacyEnterpriseBrand('Sub2API 企业工作台', false)).toBe('Sub2API 企业工作台')
  })
})
