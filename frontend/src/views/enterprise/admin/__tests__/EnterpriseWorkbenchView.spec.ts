import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import EnterpriseWorkbenchView from '../EnterpriseWorkbenchView.vue'

const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div/>' } }, { path: '/enterprise/admin/audit', component: { template: '<div/>' } }] })

const { getWorkbenchSummary, listDepartments, listEmployees, listWorkbenchAuditEvents } = vi.hoisted(() => ({
  getWorkbenchSummary: vi.fn(),
  listDepartments: vi.fn(),
  listEmployees: vi.fn(),
  listWorkbenchAuditEvents: vi.fn(),
}))

vi.mock('@/api/enterprise', () => ({
  enterpriseAPI: { getWorkbenchSummary, listDepartments, listEmployees, listWorkbenchAuditEvents },
}))

describe('EnterpriseWorkbenchView', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    listWorkbenchAuditEvents.mockResolvedValue({
      items: [{ id: 1, event_type: 'allocation.update', entity_type: 'employee', result: 'success', payload: {}, actor_ref: 'admin@example.com', created_at: new Date().toISOString() }],
      total: 1, page: 1, page_size: 5, pages: 1,
    })
    getWorkbenchSummary.mockResolvedValue({
      total_usage_credit: '2.75', total_requests: 1, employee_count: 1, active_employee_count: 1,
      subscription_status: 'active', subscription_plan: 'Business', enterprise_pool_limit: '100', enterprise_pool_used: '2.75', enterprise_pool_remaining: '97.25', enterprise_pool_exhausted: false, pool_source_status: 'available', pool_source: 'upstream subscription', pool_window_type: 'week', pool_window_anchor: new Date().toISOString(),
      employee_summaries: [{ employee_id: 22, email: 'employee@example.com', requests: 1, configured_credit: '12.50', usage_credit: '2.75', remaining_credit: '9.75', overage_credit: '0', recommendation: '当前 allocation 范围内' }], usage_trend: [],
    })
    listDepartments.mockResolvedValue([])
    listEmployees.mockResolvedValue([])
  })

  it('uses weekly as the explicit MVP query and renders the employee allocation overview', async () => {
    const wrapper = mount(EnterpriseWorkbenchView, { global: { plugins: [ElementPlus, router] } })
    await flushPromises()

    const summaryParams = getWorkbenchSummary.mock.calls[0]?.[0] as Record<string, unknown>
    expect(summaryParams).not.toHaveProperty('window_type')
    expect(wrapper.text()).toContain('employee@example.com')
    expect(wrapper.text()).toContain('allocation.update')
    expect(wrapper.text()).toContain('admin@example.com')
    wrapper.unmount()
  })

  it('shows the recent-activity source-unavailable state without blocking the summary panels', async () => {
    listWorkbenchAuditEvents.mockRejectedValue(new Error('audit source unavailable'))
    const wrapper = mount(EnterpriseWorkbenchView, { global: { plugins: [ElementPlus, router] } })
    await flushPromises()

    expect(wrapper.text()).toContain('审计数据源暂时不可用')
    expect(wrapper.text()).toContain('employee@example.com')
    wrapper.unmount()
  })

  it('clears prior results and marks the source unavailable after a reload failure', async () => {
    const wrapper = mount(EnterpriseWorkbenchView, { global: { plugins: [ElementPlus, router] } })
    await flushPromises()
    getWorkbenchSummary.mockRejectedValueOnce(new Error('source unavailable'))
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('部分数据源暂时不可用')
    expect(wrapper.text()).not.toContain('employee@example.com')
    wrapper.unmount()
  })
})
