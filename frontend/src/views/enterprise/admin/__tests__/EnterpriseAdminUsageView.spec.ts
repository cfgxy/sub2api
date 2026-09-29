import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import EnterpriseAdminUsageView from '../EnterpriseAdminUsageView.vue'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: (await import('@/views/admin/__tests__/enterpriseTestI18n')).useEnterpriseTestI18n,
}))

const { getWorkbenchSummary, listWorkbenchUsage, listDepartments, listEmployees } = vi.hoisted(() => ({
  getWorkbenchSummary: vi.fn(), listWorkbenchUsage: vi.fn(), listDepartments: vi.fn(), listEmployees: vi.fn(),
}))
vi.mock('@/api/enterprise', () => ({ enterpriseAPI: { getWorkbenchSummary, listWorkbenchUsage, listDepartments, listEmployees } }))

const summary = {
  total_usage_credit: '2.75', total_requests: 1, employee_count: 1, active_employee_count: 1,
  subscription_status: 'active', subscription_plan: 'Business', enterprise_pool_limit: '100', enterprise_pool_used: '2.75', enterprise_pool_remaining: '97.25', enterprise_pool_exhausted: false, pool_source_status: 'available', pool_source: 'upstream subscription', pool_window_type: 'week', pool_window_anchor: new Date().toISOString(),
  employee_summaries: [{ employee_id: 22, email: 'employee@example.com', requests: 1, configured_credit: '12.50', usage_credit: '2.75', remaining_credit: '9.75', overage_credit: '0', recommendation: '当前额度范围内' }],
  usage_trend: [],
}
const row = { attribution_id: 1, usage_log_id: 2, employee_id: 22, employee_email: 'employee@example.com', api_key_id: 9, api_key_masked: 'sk-abc...1234', model: 'gpt-4o', window_type: 'week', window_anchor: new Date().toISOString(), request_at: new Date().toISOString(), classification: 'employee', assignment_generation: 1, usage_credit: '0.5', configured_credit: '10' }
const page = (items = [row], total = items.length) => ({ items, total, page: 1, page_size: 20, pages: Math.ceil(total / 20) })
function mountView() { return mount(EnterpriseAdminUsageView, { global: { plugins: [createPinia()], stubs: { EnterpriseUsageTrendChart: { template: '<div class="chart" />' } } } }) }

describe('EnterpriseAdminUsageView', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    getWorkbenchSummary.mockResolvedValue(summary)
    listWorkbenchUsage.mockResolvedValue(page())
    listDepartments.mockResolvedValue([{ id: 3, name: '研发部' }])
    listEmployees.mockResolvedValue([{ id: 22, email: 'employee@example.com' }])
  })

  it('preserves filter controls, backend-compatible week value and model query', async () => {
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.findAllComponents({ name: 'Select' }).length).toBeGreaterThanOrEqual(3)
    expect(wrapper.getComponent({ name: 'Select' }).props('options')).toEqual([{ value: 'week', label: '按周' }])
    await wrapper.get('input[placeholder="模型"]').setValue('gpt-4o')
    await wrapper.findAll('button').find(item => item.text() === '查询')!.trigger('click')
    await flushPromises()
    expect(listWorkbenchUsage.mock.calls.at(-1)?.[0]).toMatchObject({ model: 'gpt-4o', window_type: 'week' })
    expect(wrapper.text()).toContain('剩余额度')
    expect(wrapper.text()).not.toContain('actual cost')
    wrapper.unmount()
  })

  it('keeps only existing sequence points in the trend chart', async () => {
    getWorkbenchSummary.mockResolvedValueOnce({ ...summary, usage_trend: [{ at: '2026-01-01T00:00:00Z', requests: 3, usage_credit: '1' }] })
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.find('.chart').exists()).toBe(true)
    wrapper.unmount()
  })

  it('preserves pagination and independent source failure recovery', async () => {
    listWorkbenchUsage.mockResolvedValueOnce(page([row], 45))
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.getComponent({ name: 'Pagination' }).props('total')).toBe(45)
    listWorkbenchUsage.mockRejectedValueOnce(new Error('offline'))
    await wrapper.findAll('button').find(item => item.text().includes('刷新数据'))!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('用量明细数据源暂时不可用')
    expect(wrapper.text()).toContain('2.75')
    wrapper.unmount()
  })

  it('shows empty results without inventing trend or rows', async () => {
    listWorkbenchUsage.mockResolvedValueOnce(page([]))
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('当前筛选条件暂无趋势数据')
    expect(wrapper.text()).toContain('当前筛选条件暂无明细')
    wrapper.unmount()
  })
})
