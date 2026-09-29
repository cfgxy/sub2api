import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import type { AxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import { enterpriseClient } from '@/api/enterprise'
import EnterpriseWorkbenchView from '../EnterpriseWorkbenchView.vue'
import EnterpriseEmployeeDetailView from '../EnterpriseEmployeeDetailView.vue'
import EnterpriseAllocationView from '../EnterpriseAllocationView.vue'
import EnterpriseAdminKeysView from '../EnterpriseAdminKeysView.vue'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: (await import('@/views/admin/__tests__/enterpriseTestI18n')).useEnterpriseTestI18n,
}))

vi.mock('vue-router', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-router')>()),
  useRoute: () => ({ params: { id: '42' } }),
}))

// 不 mock '@/api/enterprise'：在真实 axios 拦截器路径下验证 5xx 不整页跳转到会话状态页。
type Route = { status: number; data?: unknown }

function installAdapter(routes: Record<string, () => Route>) {
  enterpriseClient.defaults.adapter = (async (config: AxiosRequestConfig) => {
    const url = config.url ?? ''
    const matched = Object.entries(routes).find(([path]) => url.includes(path))
    const route = matched ? matched[1]() : { status: 500 }
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

const allFail = {
  '/enterprise/admin/workbench/summary': () => ({ status: 500 }),
  '/enterprise/admin/workbench/usage': () => ({ status: 500 }),
  '/enterprise/admin/workbench/audit-events': () => ({ status: 500 }),
  '/enterprise/admin/departments': () => ({ status: 500 }),
  '/enterprise/admin/employees': () => ({ status: 500 }),
  '/enterprise/admin/keys': () => ({ status: 500 }),
}

const router = createRouter({
  history: createMemoryHistory(),
  routes: [{ path: '/', component: { template: '<div/>' } }, { path: '/:pathMatch(.*)*', component: { template: '<div/>' } }],
})

describe('企业后台四页主数据 5xx：页内错误态，不整页跳转（SHAN-392 返工）', () => {
  const originalLocation = window.location

  beforeEach(() => {
    localStorage.clear()
    setActivePinia(createPinia())
    Object.defineProperty(window, 'location', {
      value: { ...originalLocation, pathname: '/enterprise/admin', href: '/enterprise/admin' },
      writable: true,
    })
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', { value: originalLocation, writable: true })
  })

  const mountWith = (component: object) =>
    mount(component, { global: { plugins: [router, createPinia()], stubs: { teleport: true, EnterpriseUsageTrendChart: { template: '<div />' } } } })

  it('工作台：汇总与目录 5xx 显示页内错误态', async () => {
    installAdapter(allFail)
    const wrapper = mountWith(EnterpriseWorkbenchView)
    await flushPromises()
    expect(window.location.href).not.toContain('/enterprise/session-states')
    expect(wrapper.text()).toContain('部分数据源暂时不可用')
    wrapper.unmount()
  })

  it('员工详情：详情 5xx 显示页内错误态', async () => {
    installAdapter(allFail)
    const wrapper = mountWith(EnterpriseEmployeeDetailView)
    await flushPromises()
    expect(window.location.href).not.toContain('/enterprise/session-states')
    expect(wrapper.text()).toContain('重试')
    wrapper.unmount()
  })

  it('额度分配：汇总 5xx 显示页内错误态', async () => {
    installAdapter(allFail)
    const wrapper = mountWith(EnterpriseAllocationView)
    await flushPromises()
    expect(window.location.href).not.toContain('/enterprise/session-states')
    expect(wrapper.text()).toContain('数据来源暂时不可用')
    wrapper.unmount()
  })

  it('员工 Key：列表 5xx 显示页内错误态', async () => {
    installAdapter(allFail)
    const wrapper = mountWith(EnterpriseAdminKeysView)
    await flushPromises()
    expect(window.location.href).not.toContain('/enterprise/session-states')
    expect(wrapper.text()).toContain('员工 Key 信息暂时不可用')
    wrapper.unmount()
  })

  it('身份类错误（401）仍整页跳转，不放宽既有边界', async () => {
    installAdapter({ '/enterprise/admin/keys': () => ({ status: 401 }) })
    const wrapper = mountWith(EnterpriseAdminKeysView)
    await flushPromises()
    expect(window.location.href).toContain('/enterprise/session-states?state=session-expired')
    wrapper.unmount()
  })
})
