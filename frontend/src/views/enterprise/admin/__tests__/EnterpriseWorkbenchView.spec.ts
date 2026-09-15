import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import EnterpriseWorkbenchView from '../EnterpriseWorkbenchView.vue'

const { getWorkbenchSummary, listWorkbenchUsage, listWorkbenchAuditEvents, listDepartments, listEmployees } = vi.hoisted(() => ({
  getWorkbenchSummary: vi.fn(),
  listWorkbenchUsage: vi.fn(),
  listWorkbenchAuditEvents: vi.fn(),
  listDepartments: vi.fn(),
  listEmployees: vi.fn(),
}))

vi.mock('@/api/enterprise', () => ({
  enterpriseAPI: { getWorkbenchSummary, listWorkbenchUsage, listWorkbenchAuditEvents, listDepartments, listEmployees },
}))

describe('EnterpriseWorkbenchView', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    getWorkbenchSummary.mockResolvedValue({
      total_usage_credit: '2.75', total_requests: 1, employee_count: 1, active_employee_count: 1,
      subscription_status: 'active', subscription_plan: 'Business', enterprise_pool_limit: '100', enterprise_pool_used: '2.75', enterprise_pool_remaining: '97.25', enterprise_pool_exhausted: false, pool_source_status: 'available', pool_source: 'upstream subscription', pool_window_type: 'week', pool_window_anchor: new Date().toISOString(),
      employee_summaries: [{ employee_id: 22, email: 'employee@example.com', requests: 1, configured_credit: '12.50', usage_credit: '2.75', remaining_credit: '9.75', overage_credit: '0', recommendation: '当前 allocation 范围内' }], usage_trend: [],
    })
    listWorkbenchUsage.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, pages: 1 })
    listWorkbenchAuditEvents.mockResolvedValue({
      items: [{ id: 1, event_type: 'key.rotated', entity_type: 'api_key', result: 'success', reason: '', payload: {}, actor_ref: 'enterprise_session:must-not-render', created_at: new Date().toISOString() }],
      total: 1, page: 1, page_size: 20, pages: 1,
    })
    listDepartments.mockResolvedValue([])
    listEmployees.mockResolvedValue([])
  })

  it('uses weekly as the explicit MVP query and renders sanitized audit metadata', async () => {
    const wrapper = mount(EnterpriseWorkbenchView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    const summaryParams = getWorkbenchSummary.mock.calls[0]?.[0] as Record<string, unknown>
    expect(summaryParams).not.toHaveProperty('window_type')
    expect(wrapper.text()).not.toContain('enterprise_session:must-not-render')
    expect(wrapper.text()).toContain('操作者')
    wrapper.unmount()
  })

  it('clears prior results and marks the source unavailable after a reload failure', async () => {
    const wrapper = mount(EnterpriseWorkbenchView, { global: { plugins: [ElementPlus] } })
    await flushPromises()
    getWorkbenchSummary.mockRejectedValueOnce(new Error('source unavailable'))
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('部分数据源暂时不可用')
    expect(wrapper.text()).not.toContain('employee@example.com')
    wrapper.unmount()
  })
})
