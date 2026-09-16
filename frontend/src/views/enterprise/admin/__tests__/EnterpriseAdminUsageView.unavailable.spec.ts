import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import type { AxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { enterpriseClient } from '@/api/enterprise'
import EnterpriseAdminUsageView from '../EnterpriseAdminUsageView.vue'

// 本文件不 mock '@/api/enterprise'：EnterpriseAdminUsageView.spec.ts 里那份整体模块
// mock 的用例在 SHAN-241 Review 中被判定为无效证据——它绕过了全局 axios 拦截器，
// 掩盖了「捕获 catch 分支但从未在真实调用链上触发」的缺陷。这里改在 enterpriseClient
// 的 adapter 层注入真实 5xx，让请求走完整的拦截器→API 封装层→组件 catch 链路。

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

const summaryPayload = {
  code: 0,
  data: {
    total_usage_credit: '2.75', total_requests: 1, employee_count: 1, active_employee_count: 1,
    subscription_status: 'active', subscription_plan: 'Business', enterprise_pool_limit: '100',
    enterprise_pool_used: '2.75', enterprise_pool_remaining: '97.25', enterprise_pool_exhausted: false,
    pool_source_status: 'available', pool_source: 'upstream subscription', pool_window_type: 'week',
    pool_window_anchor: new Date().toISOString(), employee_summaries: [], usage_trend: [],
  },
}
const usagePayload = { code: 0, data: { items: [], total: 0, page: 1, page_size: 20, pages: 1 } }
const departmentsPayload = { code: 0, data: [] }
const employeesPayload = { code: 0, data: [] }
const okRoutes = {
  '/enterprise/admin/workbench/summary': () => ({ status: 200, data: summaryPayload }),
  '/enterprise/admin/workbench/usage': () => ({ status: 200, data: usagePayload }),
  '/enterprise/admin/departments': () => ({ status: 200, data: departmentsPayload }),
  '/enterprise/admin/employees': () => ({ status: 200, data: employeesPayload }),
}

describe('EnterpriseAdminUsageView — 真实 axios 拦截器路径下的分区降级（SHAN-241 返工）', () => {
  const originalLocation = window.location

  beforeEach(() => {
    localStorage.clear()
    Object.defineProperty(window, 'location', {
      value: { ...originalLocation, pathname: '/enterprise/admin/usage', href: '/enterprise/admin/usage' },
      writable: true,
    })
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', { value: originalLocation, writable: true })
  })

  it('汇总/趋势数据源真实 5xx 时只降级该分区，不整页跳转，明细表格分区仍正常渲染', async () => {
    installAdapter({ ...okRoutes, '/enterprise/admin/workbench/summary': () => ({ status: 500 }) })
    const wrapper = mount(EnterpriseAdminUsageView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    expect(window.location.href).not.toContain('/enterprise/session-states')
    expect(wrapper.text()).toContain('部分数据源暂时不可用')
    expect(wrapper.text()).not.toContain('用量明细数据源暂时不可用')
    wrapper.unmount()
  })

  it('用量明细数据源真实 5xx 时只降级明细表格，不整页跳转，汇总分区仍正常渲染', async () => {
    installAdapter({ ...okRoutes, '/enterprise/admin/workbench/usage': () => ({ status: 500 }) })
    const wrapper = mount(EnterpriseAdminUsageView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    expect(window.location.href).not.toContain('/enterprise/session-states')
    expect(wrapper.text()).toContain('用量明细数据源暂时不可用')
    expect(wrapper.text()).toContain('2.75')
    wrapper.unmount()
  })

  it('组织目录数据源真实 5xx 时不整页跳转，汇总与明细分区仍正常渲染', async () => {
    installAdapter({ ...okRoutes, '/enterprise/admin/departments': () => ({ status: 500 }) })
    const wrapper = mount(EnterpriseAdminUsageView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    expect(window.location.href).not.toContain('/enterprise/session-states')
    expect(wrapper.text()).toContain('组织目录')
    expect(wrapper.text()).toContain('2.75')
    wrapper.unmount()
  })

  it('身份/会话类错误（401）在同一批端点上仍整页跳转到全局会话状态页，不放宽既有边界', async () => {
    installAdapter({ ...okRoutes, '/enterprise/admin/workbench/summary': () => ({ status: 401, data: {} }) })
    const wrapper = mount(EnterpriseAdminUsageView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    expect(window.location.href).toContain('/enterprise/session-states?state=session-expired')
    wrapper.unmount()
  })
})
