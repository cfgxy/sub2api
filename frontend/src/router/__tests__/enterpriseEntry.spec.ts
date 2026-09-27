import { describe, expect, it, vi } from 'vitest'
import { resolveEnterpriseEntry } from '../enterpriseGuard'

describe('企业域入口分流', () => {
  for (const path of ['/', '/home']) {
    it.each([
      ['未登录', { authenticated: false, forcePasswordChange: false }, '/enterprise/login'],
      ['员工', { authenticated: true, role: 'enterprise_employee' as const, forcePasswordChange: false }, '/enterprise/home'],
      ['管理员', { authenticated: true, role: 'enterprise_admin' as const, forcePasswordChange: false }, '/enterprise/admin/workbench'],
      ['首登改密', { authenticated: true, role: 'enterprise_employee' as const, forcePasswordChange: true }, '/enterprise/change-password'],
    ])(`${path}：%s进入企业门户`, async (_label, auth, target) => {
      expect(await resolveEnterpriseEntry(path, vi.fn().mockResolvedValue({}), auth)).toBe(target)
    })

    it(`${path}：官方 Host 对企业本地会话也保持原入口`, async () => {
      const probe = vi.fn().mockRejectedValue({ reason: 'ENTERPRISE_HOST_MISMATCH' })
      expect(await resolveEnterpriseEntry(path, probe, {
        authenticated: true, role: 'enterprise_admin', forcePasswordChange: false,
      })).toBeNull()
      expect(probe).toHaveBeenCalledOnce()
    })
  }

  it('非入口页面不查询企业品牌，也不覆盖 SEO 页面路由', async () => {
    const probe = vi.fn()
    expect(await resolveEnterpriseEntry('/pricing', probe, {
      authenticated: false, forcePasswordChange: false,
    })).toBeNull()
    expect(probe).not.toHaveBeenCalled()
  })
})
