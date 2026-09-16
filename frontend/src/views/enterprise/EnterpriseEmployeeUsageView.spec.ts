import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import EnterpriseEmployeeUsageView from './EnterpriseEmployeeUsageView.vue'

const { getEmployeeUsage, listEmployeeUsage, getEmployeeUsageTrend } = vi.hoisted(() => ({
  getEmployeeUsage: vi.fn(),
  listEmployeeUsage: vi.fn(),
  getEmployeeUsageTrend: vi.fn(),
}))

vi.mock('@/api/enterprise', () => ({
  enterpriseAPI: { getEmployeeUsage, listEmployeeUsage, getEmployeeUsageTrend },
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
const baseRecord = {
  request_at: new Date().toISOString(),
  window_anchor: new Date().toISOString(),
  api_key_masked: 'sk-abc...7890',
  generation: 2,
  actual_cost: '1.00',
}

describe('EnterpriseEmployeeUsageView', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    getEmployeeUsage.mockResolvedValue({ usage: baseUsage, enterprise_pool: basePool })
    listEmployeeUsage.mockResolvedValue({ items: [baseRecord], total: 1, page: 1, page_size: 20, pages: 1 })
    getEmployeeUsageTrend.mockResolvedValue([{ at: new Date().toISOString(), requests: 3, actual_cost: '2.75' }])
  })

  it('renders personal allocation next to enterprise pool figures without fabricating values', async () => {
    const wrapper = mount(EnterpriseEmployeeUsageView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    expect(wrapper.text()).toContain('12.50')
    expect(wrapper.text()).toContain('100')
    expect(wrapper.text()).toContain('70')
    expect(wrapper.text()).toContain('sk-abc...7890')
    expect(wrapper.text()).toContain('2')
    wrapper.unmount()
  })

  it('marks personal and enterprise-pool sections unavailable separately instead of showing zeros as real data', async () => {
    getEmployeeUsage.mockResolvedValue({
      usage: { ...baseUsage, source_status: 'unavailable', allocation: '0', actual_cost: '0', remaining: '0', overage: '0', requests: 0 },
      enterprise_pool: basePool,
    })
    const wrapper = mount(EnterpriseEmployeeUsageView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    expect(wrapper.text()).toContain('当前没有可核验的订阅窗口')
    expect(wrapper.text()).toContain('70')
    wrapper.unmount()
  })

  it('shows an explicit empty state for the trend chart instead of a fabricated zero series', async () => {
    getEmployeeUsageTrend.mockResolvedValue([])
    const wrapper = mount(EnterpriseEmployeeUsageView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    expect(wrapper.text()).toContain('当前筛选范围暂无趋势数据')
    wrapper.unmount()
  })

  it('sends the selected time window to both the detail and trend queries on filter apply', async () => {
    const wrapper = mount(EnterpriseEmployeeUsageView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    const vm = wrapper.vm as unknown as { dateRange: string[] }
    vm.dateRange = ['2026-09-01T00:00:00Z', '2026-09-08T00:00:00Z']
    await wrapper.vm.$nextTick()
    await wrapper.get('button.el-button--primary').trigger('click')
    await flushPromises()

    const detailParams = listEmployeeUsage.mock.calls.at(-1)?.[0] as Record<string, unknown>
    const trendParams = getEmployeeUsageTrend.mock.calls.at(-1)?.[0] as Record<string, unknown>
    expect(detailParams.start_at).toBe('2026-09-01T00:00:00Z')
    expect(detailParams.end_at).toBe('2026-09-08T00:00:00Z')
    expect(trendParams.start_at).toBe('2026-09-01T00:00:00Z')
    expect(trendParams.end_at).toBe('2026-09-08T00:00:00Z')
    wrapper.unmount()
  })

  it('paginates the detail table using the page and page_size params', async () => {
    listEmployeeUsage.mockResolvedValue({ items: [baseRecord], total: 45, page: 1, page_size: 20, pages: 3 })
    const wrapper = mount(EnterpriseEmployeeUsageView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    const vm = wrapper.vm as unknown as { page: number; loadDetail: () => Promise<void> }
    vm.page = 2
    await vm.loadDetail()
    await flushPromises()

    const lastCallParams = listEmployeeUsage.mock.calls.at(-1)?.[0] as Record<string, unknown>
    expect(lastCallParams.page).toBe(2)
    wrapper.unmount()
  })

  it('clears prior data and marks sources unavailable after a reload failure, without stale values lingering', async () => {
    const wrapper = mount(EnterpriseEmployeeUsageView, { global: { plugins: [ElementPlus] } })
    await flushPromises()
    expect(wrapper.text()).toContain('sk-abc...7890')

    getEmployeeUsage.mockRejectedValueOnce(new Error('source unavailable'))
    listEmployeeUsage.mockRejectedValueOnce(new Error('source unavailable'))
    getEmployeeUsageTrend.mockRejectedValueOnce(new Error('source unavailable'))
    await wrapper.get('.page-heading button').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('部分数据源暂时不可用')
    expect(wrapper.text()).not.toContain('sk-abc...7890')
    wrapper.unmount()
  })
})
