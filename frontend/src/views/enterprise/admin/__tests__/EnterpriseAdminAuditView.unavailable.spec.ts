import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import type { AxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { enterpriseClient } from '@/api/enterprise'
import EnterpriseAdminAuditView from '../EnterpriseAdminAuditView.vue'

// 不 mock '@/api/enterprise'，理由同 EnterpriseAdminUsageView.unavailable.spec.ts：
// 审计页与用量页同根因（listWorkbenchAuditEvents 封装层未透传豁免参数），Review
// 结论明确指出本页也需要在真实拦截器路径下验证。

type Route = { status: number; data?: unknown }

function installAdapter(routes: Record<string, () => Route>) {
  enterpriseClient.defaults.adapter = (async (config: AxiosRequestConfig) => {
    const url = config.url ?? ''
    const matched = Object.entries(routes).find(([path]) => url.includes(path))
    const route = matched ? matched[1]() : { status: 200, data: { code: 0, data: null } }
    if (route.status >= 400) {
      return Promise.reject({
        response: { status: route.status, data: route.data ?? {}, headers: {}, config },
        config,
        code: 'ERR_BAD_RESPONSE',
      })
    }
    return { status: route.status, data: route.data, headers: {}, config, statusText: 'OK' }
  }) as never
}

const auditPayload = { code: 0, data: { items: [], total: 0, page: 1, page_size: 20, pages: 1 } }

describe('EnterpriseAdminAuditView — 真实 axios 拦截器路径下的分区降级（SHAN-241 返工）', () => {
  const originalLocation = window.location

  beforeEach(() => {
    localStorage.clear()
    Object.defineProperty(window, 'location', {
      value: { ...originalLocation, pathname: '/enterprise/admin/audit', href: '/enterprise/admin/audit' },
      writable: true,
    })
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', { value: originalLocation, writable: true })
  })

  it('审计数据源真实 5xx 时只显示分区提示，不整页跳转', async () => {
    installAdapter({ '/enterprise/admin/workbench/audit-events': () => ({ status: 500 }) })
    const wrapper = mount(EnterpriseAdminAuditView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    expect(window.location.href).not.toContain('/enterprise/session-states')
    expect(wrapper.text()).toContain('审计数据源暂时不可用')
    wrapper.unmount()
  })

  it('身份/会话类错误（401）仍整页跳转到全局会话状态页，不放宽既有边界', async () => {
    installAdapter({ '/enterprise/admin/workbench/audit-events': () => ({ status: 401, data: {} }) })
    const wrapper = mount(EnterpriseAdminAuditView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    expect(window.location.href).toContain('/enterprise/session-states?state=session-expired')
    wrapper.unmount()
  })

  it('权限类错误（403/ENTERPRISE_DISABLED）仍整页跳转，不放宽既有边界', async () => {
    installAdapter({
      '/enterprise/admin/workbench/audit-events': () => ({ status: 403, data: { reason: 'ENTERPRISE_DISABLED' } }),
    })
    const wrapper = mount(EnterpriseAdminAuditView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    expect(window.location.href).toContain('/enterprise/session-states?state=enterprise-disabled')
    wrapper.unmount()
  })

  it('数据源恢复后重新查询能清除降级提示', async () => {
    let fail = true
    installAdapter({ '/enterprise/admin/workbench/audit-events': () => (fail ? { status: 500 } : { status: 200, data: auditPayload }) })
    const wrapper = mount(EnterpriseAdminAuditView, { global: { plugins: [ElementPlus] } })
    await flushPromises()
    expect(wrapper.text()).toContain('审计数据源暂时不可用')

    fail = false
    await wrapper.find('button.el-button--primary').trigger('click')
    await flushPromises()

    expect(wrapper.text()).not.toContain('审计数据源暂时不可用')
    wrapper.unmount()
  })
})
