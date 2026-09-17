import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import EnterpriseEmployeeHomeView from './EnterpriseEmployeeHomeView.vue'

const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div/>' } }, { path: '/enterprise/keys', component: { template: '<div/>' } }, { path: '/enterprise/usage', component: { template: '<div/>' } }] })

const { getEmployeeHome, listEmployeeUsage } = vi.hoisted(() => ({ getEmployeeHome: vi.fn(), listEmployeeUsage: vi.fn() }))

vi.mock('@/api/enterprise', () => ({
  enterpriseAPI: { getEmployeeHome, listEmployeeUsage },
}))

const baseUsage = {
  window_type: 'week' as const,
  window_anchor: new Date().toISOString(),
  source_status: 'available' as const,
  allocation: '12.50',
  actual_cost: '2.75',
  remaining: '9.75',
  overage: '0',
  requests: 3,
}
const basePool = {
  source_status: 'available' as const,
  pool_limit: '100',
  pool_used: '30',
  pool_remaining: '70',
  pool_exhausted: false,
  window_anchor: new Date().toISOString(),
}

describe('EnterpriseEmployeeHomeView', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    listEmployeeUsage.mockResolvedValue({
      items: [{ request_at: new Date().toISOString(), window_anchor: new Date().toISOString(), api_key_masked: 'sk-***abcd', generation: 1, actual_cost: '0.25' }],
      total: 1, page: 1, page_size: 5, pages: 1,
    })
  })

  it('shows personal allocation alongside an available, non-exhausted enterprise pool', async () => {
    getEmployeeHome.mockResolvedValue({
      usage: baseUsage,
      enterprise_pool: basePool,
      recent_trend: [{ at: new Date().toISOString(), requests: 3, actual_cost: '2.75' }],
      key: null,
    })
    const wrapper = mount(EnterpriseEmployeeHomeView, { global: { plugins: [ElementPlus, router] } })
    await flushPromises()

    expect(wrapper.text()).toContain('12.50')
    expect(wrapper.text()).toContain('企业总池可用')
    expect(wrapper.text()).toContain('70')
    wrapper.unmount()
  })

  it('marks the enterprise pool as exhausted distinctly from personal overage', async () => {
    getEmployeeHome.mockResolvedValue({
      usage: baseUsage,
      enterprise_pool: { ...basePool, pool_used: '100', pool_remaining: '0', pool_exhausted: true },
      recent_trend: [],
      key: null,
    })
    const wrapper = mount(EnterpriseEmployeeHomeView, { global: { plugins: [ElementPlus, router] } })
    await flushPromises()

    expect(wrapper.text()).toContain('企业总池已耗尽')
    wrapper.unmount()
  })

  it('shows the pool as unavailable without fabricating limit figures when the upstream source is missing', async () => {
    getEmployeeHome.mockResolvedValue({
      usage: baseUsage,
      enterprise_pool: { source_status: 'unavailable', pool_limit: '0', pool_used: '0', pool_remaining: '0', pool_exhausted: false },
      recent_trend: [],
      key: null,
    })
    const wrapper = mount(EnterpriseEmployeeHomeView, { global: { plugins: [ElementPlus, router] } })
    await flushPromises()

    expect(wrapper.text()).toContain('企业总池来源暂不可用')
    expect(wrapper.text()).not.toContain('企业总池可用')
    wrapper.unmount()
  })

  it('shows an explicit empty state for the recent trend instead of a fabricated zero series', async () => {
    getEmployeeHome.mockResolvedValue({ usage: baseUsage, enterprise_pool: basePool, recent_trend: [], key: null })
    const wrapper = mount(EnterpriseEmployeeHomeView, { global: { plugins: [ElementPlus, router] } })
    await flushPromises()

    expect(wrapper.text()).toContain('近 7 天暂无调用记录')
    wrapper.unmount()
  })

  it('renders the recent-calls detail table sourced from the existing usage-details API', async () => {
    getEmployeeHome.mockResolvedValue({
      usage: baseUsage,
      enterprise_pool: basePool,
      recent_trend: [],
      key: null,
    })
    const wrapper = mount(EnterpriseEmployeeHomeView, { global: { plugins: [ElementPlus, router] } })
    await flushPromises()

    expect(listEmployeeUsage).toHaveBeenCalledWith({ page: 1, page_size: 5 })
    expect(wrapper.text()).toContain('sk-***abcd')
    expect(wrapper.text()).toContain('0.25')
    wrapper.unmount()
  })

  it('shows the recent-calls source-unavailable state without blocking the other panels', async () => {
    getEmployeeHome.mockResolvedValue({
      usage: baseUsage,
      enterprise_pool: basePool,
      recent_trend: [],
      key: null,
    })
    listEmployeeUsage.mockRejectedValue(new Error('usage detail source unavailable'))
    const wrapper = mount(EnterpriseEmployeeHomeView, { global: { plugins: [ElementPlus, router] } })
    await flushPromises()

    expect(wrapper.text()).toContain('调用明细来源暂时不可用')
    expect(wrapper.text()).toContain('12.50')
    wrapper.unmount()
  })
})
