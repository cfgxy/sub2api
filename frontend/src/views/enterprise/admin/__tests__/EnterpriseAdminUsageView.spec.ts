import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import EnterpriseAdminUsageView from '../EnterpriseAdminUsageView.vue'

const { getWorkbenchSummary, listWorkbenchUsage, listDepartments, listEmployees } = vi.hoisted(() => ({
  getWorkbenchSummary: vi.fn(),
  listWorkbenchUsage: vi.fn(),
  listDepartments: vi.fn(),
  listEmployees: vi.fn(),
}))

vi.mock('@/api/enterprise', () => ({
  enterpriseAPI: { getWorkbenchSummary, listWorkbenchUsage, listDepartments, listEmployees },
}))

const summaryFixture = {
  total_usage_credit: '2.75', total_requests: 1, employee_count: 1, active_employee_count: 1,
  subscription_status: 'active', subscription_plan: 'Business', enterprise_pool_limit: '100', enterprise_pool_used: '2.75', enterprise_pool_remaining: '97.25', enterprise_pool_exhausted: false, pool_source_status: 'available', pool_source: 'upstream subscription', pool_window_type: 'week', pool_window_anchor: new Date().toISOString(),
  employee_summaries: [{ employee_id: 22, email: 'employee@example.com', requests: 1, configured_credit: '12.50', usage_credit: '2.75', remaining_credit: '9.75', overage_credit: '0', recommendation: '当前 allocation 范围内' }],
  usage_trend: [],
}

const usageRow = { attribution_id: 1, usage_log_id: 2, employee_id: 22, employee_email: 'employee@example.com', api_key_id: 9, api_key_masked: 'sk-abc...1234', model: 'gpt-4o', window_type: 'week', window_anchor: new Date().toISOString(), request_at: new Date().toISOString(), classification: 'employee', assignment_generation: 1, usage_credit: '0.5', configured_credit: '10' }

describe('EnterpriseAdminUsageView', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    getWorkbenchSummary.mockResolvedValue(summaryFixture)
    listWorkbenchUsage.mockResolvedValue({ items: [usageRow], total: 1, page: 1, page_size: 20, pages: 1 })
    listDepartments.mockResolvedValue([{ id: 3, name: '研发部', created_at: new Date().toISOString() }])
    listEmployees.mockResolvedValue([{ id: 22, email: 'employee@example.com', status: 'active', must_change_password: false, version: 1 }])
  })

  it('renders the department and employee filter controls and passes the model filter through on search', async () => {
    const wrapper = mount(EnterpriseAdminUsageView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    // The department/employee selects previously existed only as reactive
    // state on the workbench tab with no rendered UI; the standalone usage
    // page must render actual controls for them.
    const selects = wrapper.findAllComponents({ name: 'ElSelect' })
    expect(selects.length).toBeGreaterThanOrEqual(3) // window_type + department + employee

    const modelInput = wrapper.find('input[placeholder="模型"]')
    expect(modelInput.exists()).toBe(true)
    await modelInput.setValue('gpt-4o')
    await wrapper.find('button.el-button--primary').trigger('click')
    await flushPromises()

    const lastCall = listWorkbenchUsage.mock.calls.at(-1)?.[0] as Record<string, unknown>
    expect(lastCall.model).toBe('gpt-4o')
    wrapper.unmount()
  })

  it('only offers window_type values the backend accepts (B1 regression)', async () => {
    // backend/internal/enterpriseidentity/workbench.go parseWorkbenchQuery
    // 400s any window_type other than "" or "week" — the window selector
    // must never offer a value the API will reject, or the whole page
    // degrades to "数据源不可用" on selection.
    const wrapper = mount(EnterpriseAdminUsageView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    const windowSelect = wrapper.findAllComponents({ name: 'ElSelect' })[0]
    const options = windowSelect.findAllComponents({ name: 'ElOption' })
    const values = options.map((option) => option.props('value'))
    expect(values).toEqual(['week'])
    wrapper.unmount()
  })

  it('respects pagination boundaries when requesting the next page', async () => {
    listWorkbenchUsage.mockResolvedValue({ items: [usageRow], total: 45, page: 1, page_size: 20, pages: 3 })
    const wrapper = mount(EnterpriseAdminUsageView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    const pager = wrapper.findComponent({ name: 'ElPagination' })
    expect(pager.exists()).toBe(true)
    expect(pager.props('total')).toBe(45)
    wrapper.unmount()
  })

  it('degrades gracefully and shows the unavailable notice when the usage source fails', async () => {
    listWorkbenchUsage.mockRejectedValue(new Error('source unavailable'))
    const wrapper = mount(EnterpriseAdminUsageView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    expect(wrapper.text()).toContain('部分数据源暂时不可用')
    expect(wrapper.text()).toContain('用量明细数据源暂时不可用')
    wrapper.unmount()
  })

  it('shows an empty state when a filter combination yields no rows', async () => {
    listWorkbenchUsage.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, pages: 1 })
    const wrapper = mount(EnterpriseAdminUsageView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    expect(wrapper.text()).toContain('当前筛选条件暂无明细')
    wrapper.unmount()
  })
})
