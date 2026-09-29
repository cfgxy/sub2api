import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import EnterpriseEmployeeHomeView from './EnterpriseEmployeeHomeView.vue'

vi.mock('vue-i18n', async () => ({
  ...(await vi.importActual<typeof import('vue-i18n')>('vue-i18n')),
  useI18n: (await import('@/views/admin/__tests__/enterpriseTestI18n')).useEnterpriseTestI18n,
}))

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
    const wrapper = mount(EnterpriseEmployeeHomeView, { global: { plugins: [router] } })
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
    const wrapper = mount(EnterpriseEmployeeHomeView, { global: { plugins: [router] } })
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
    const wrapper = mount(EnterpriseEmployeeHomeView, { global: { plugins: [router] } })
    await flushPromises()

    expect(wrapper.text()).toContain('企业总池来源暂不可用')
    expect(wrapper.text()).not.toContain('企业总池可用')
    wrapper.unmount()
  })

  it('shows an explicit empty state for the recent trend instead of a fabricated zero series', async () => {
    getEmployeeHome.mockResolvedValue({ usage: baseUsage, enterprise_pool: basePool, recent_trend: [], key: null })
    const wrapper = mount(EnterpriseEmployeeHomeView, { global: { plugins: [router] } })
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
    const wrapper = mount(EnterpriseEmployeeHomeView, { global: { plugins: [router] } })
    await flushPromises()

    expect(getEmployeeHome).toHaveBeenCalledWith({ suppressUnavailableRedirect: true })
    expect(listEmployeeUsage).toHaveBeenCalledWith({ page: 1, page_size: 5 }, { suppressUnavailableRedirect: true })
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
    const wrapper = mount(EnterpriseEmployeeHomeView, { global: { plugins: [router] } })
    await flushPromises()

    expect(wrapper.text()).toContain('调用明细来源暂时不可用')
    expect(wrapper.text()).toContain('12.50')
    wrapper.unmount()
  })

  it('shows the loading state before the home payload resolves', async () => {
    let resolveHome: (value: unknown) => void = () => undefined
    getEmployeeHome.mockReturnValue(new Promise((resolve) => { resolveHome = resolve }))
    const wrapper = mount(EnterpriseEmployeeHomeView, { global: { plugins: [router] } })
    await nextTick()

    expect(wrapper.find('[data-testid="home-loading"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="home-load-error"]').exists()).toBe(false)

    resolveHome({ usage: baseUsage, enterprise_pool: basePool, recent_trend: [], key: null })
    await flushPromises()
    expect(wrapper.find('[data-testid="home-loading"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows a load-error state and recovers on retry', async () => {
    getEmployeeHome
      .mockRejectedValueOnce(new Error('home source unavailable'))
      .mockResolvedValueOnce({ usage: baseUsage, enterprise_pool: basePool, recent_trend: [], key: null })
    const wrapper = mount(EnterpriseEmployeeHomeView, { global: { plugins: [router] } })
    await flushPromises()

    expect(wrapper.find('[data-testid="home-load-error"]').exists()).toBe(true)

    await wrapper.get('[data-testid="home-load-retry"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="home-load-error"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('12.50')
    wrapper.unmount()
  })

  it('shows an explicit empty state for recent calls instead of a fabricated row', async () => {
    getEmployeeHome.mockResolvedValue({ usage: baseUsage, enterprise_pool: basePool, recent_trend: [], key: null })
    listEmployeeUsage.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 5, pages: 1 })
    const wrapper = mount(EnterpriseEmployeeHomeView, { global: { plugins: [router] } })
    await flushPromises()

    expect(wrapper.find('[data-testid="home-recent-empty"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('never renders platform-admin-only fields that leak into the employee payload', async () => {
    const platformOnly = 'PLATFORM-ONLY-SENTINEL-4213'
    const employeeVisible = 'EMPLOYEE-VISIBLE-SENTINEL'
    getEmployeeHome.mockResolvedValue({
      usage: { ...baseUsage, allocation: employeeVisible },
      enterprise_pool: basePool,
      recent_trend: [],
      key: null,
      // 后端若误带平台管理员专属字段，员工页面不得渲染
      platform_total_revenue: platformOnly,
      enterprises: [{ id: 1, name: platformOnly, owner_email: platformOnly }],
    })
    const wrapper = mount(EnterpriseEmployeeHomeView, { global: { plugins: [router] } })
    await flushPromises()

    // 正向对照：同一断言方式确实能检出已渲染文本，证明下面的负向断言不是空断言
    expect(wrapper.text()).toContain(employeeVisible)
    expect(wrapper.text()).not.toContain(platformOnly)
    wrapper.unmount()
  })
})
