import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import type { AxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import { enterpriseClient } from '@/api/enterprise'
import EnterpriseEmployeeHomeView from './EnterpriseEmployeeHomeView.vue'

// 不 mock '@/api/enterprise'：Review 结论指出「最近调用」面板的降级用例此前只在
// listEmployeeUsage 的 mock 层验证，拦截器从未被真实触达，导致 5xx 时整页跳转到
// /enterprise/session-states 的缺陷被掩盖。这里改走真实 axios adapter，复现
// SHAN-241 同根因（enterpriseSuppressUnavailableRedirect 未透传）在本页的返工验证。

const router = createRouter({
  history: createMemoryHistory(),
  routes: [
    { path: '/', component: { template: '<div/>' } },
    { path: '/enterprise/keys', component: { template: '<div/>' } },
    { path: '/enterprise/usage', component: { template: '<div/>' } },
  ],
})

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

const homePayload = {
  code: 0,
  data: {
    usage: {
      window_type: 'week',
      window_anchor: new Date().toISOString(),
      source_status: 'available',
      allocation: '12.50',
      actual_cost: '2.75',
      remaining: '9.75',
      overage: '0',
      requests: 3,
    },
    enterprise_pool: {
      source_status: 'available',
      pool_limit: '100',
      pool_used: '30',
      pool_remaining: '70',
      pool_exhausted: false,
      window_anchor: new Date().toISOString(),
    },
    recent_trend: [],
    key: null,
  },
}

describe('EnterpriseEmployeeHomeView — 真实 axios 拦截器路径下的「最近调用」面板降级（SHAN-135 返工）', () => {
  const originalLocation = window.location

  beforeEach(() => {
    localStorage.clear()
    Object.defineProperty(window, 'location', {
      value: { ...originalLocation, pathname: '/enterprise/home', href: '/enterprise/home' },
      writable: true,
    })
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', { value: originalLocation, writable: true })
  })

  it('用量明细来源真实 5xx 时只显示面板内降级提示，不整页跳转到会话状态页', async () => {
    installAdapter({
      '/enterprise/home': () => ({ status: 200, data: homePayload }),
      '/enterprise/usage/me/details': () => ({ status: 500 }),
    })
    const wrapper = mount(EnterpriseEmployeeHomeView, { global: { plugins: [ElementPlus, router] } })
    await flushPromises()

    expect(window.location.href).not.toContain('/enterprise/session-states')
    expect(wrapper.text()).toContain('调用明细来源暂时不可用')
    expect(wrapper.text()).toContain('12.50')
    wrapper.unmount()
  })

  it('身份/会话类错误（401）仍整页跳转到全局会话状态页，不放宽既有边界', async () => {
    installAdapter({
      '/enterprise/home': () => ({ status: 200, data: homePayload }),
      '/enterprise/usage/me/details': () => ({ status: 401, data: {} }),
    })
    const wrapper = mount(EnterpriseEmployeeHomeView, { global: { plugins: [ElementPlus, router] } })
    await flushPromises()

    expect(window.location.href).toContain('/enterprise/session-states?state=session-expired')
    wrapper.unmount()
  })
})
