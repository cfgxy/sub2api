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
      employee_summaries: [{ employee_id: 22, email: 'employee@example.com', requests: 1, configured_credit: '12.50', usage_credit: '2.75' }],
    })
    listWorkbenchUsage.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, pages: 1 })
    listWorkbenchAuditEvents.mockResolvedValue({
      items: [{ id: 1, event_type: 'key.rotated', entity_type: 'api_key', payload: {}, actor_ref: 'enterprise_session:must-not-render', created_at: new Date().toISOString() }],
      total: 1, page: 1, page_size: 20, pages: 1,
    })
    listDepartments.mockResolvedValue([])
    listEmployees.mockResolvedValue([])
  })

  it('uses one unqualified default query and never renders an actor session reference', async () => {
    const wrapper = mount(EnterpriseWorkbenchView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    const summaryParams = getWorkbenchSummary.mock.calls[0]?.[0] as Record<string, unknown>
    expect(summaryParams).not.toHaveProperty('window_type')
    expect(wrapper.text()).not.toContain('enterprise_session:must-not-render')
    expect(wrapper.find('th').text()).not.toContain('操作者')
    wrapper.unmount()
  })
})
