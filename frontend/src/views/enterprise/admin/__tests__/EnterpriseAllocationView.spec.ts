import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import EnterpriseAllocationView from '../EnterpriseAllocationView.vue'
import { useEnterpriseAuthStore } from '@/stores/enterpriseAuth'

const { getWorkbenchSummary, listSubscriptionAllocations, getAllocationSummary, setAllocation, listDepartments, listEmployees } = vi.hoisted(() => ({
  getWorkbenchSummary: vi.fn(),
  listSubscriptionAllocations: vi.fn(),
  getAllocationSummary: vi.fn(),
  setAllocation: vi.fn(),
  listDepartments: vi.fn(),
  listEmployees: vi.fn(),
}))

vi.mock('@/api/enterprise', () => ({
  enterpriseAPI: { getWorkbenchSummary, listSubscriptionAllocations, getAllocationSummary, setAllocation, listDepartments, listEmployees },
  onEnterpriseAuthSession: vi.fn(() => () => {}),
}))

describe('EnterpriseAllocationView', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    setActivePinia(createPinia())
    const auth = useEnterpriseAuthStore()
    auth.principal = { enterprise_id: 7, principal_type: 'admin', principal_id: 1, email: 'admin@example.com', role: 'enterprise_admin', force_password_change: false }
    getWorkbenchSummary.mockResolvedValue({
      total_usage_credit: '0', total_requests: 0, employee_count: 2, active_employee_count: 2,
      subscription_id: 9, subscription_status: 'active', subscription_plan: 'Business',
      enterprise_pool_limit: '2400', enterprise_pool_used: '1980', enterprise_pool_remaining: '420', enterprise_pool_exhausted: false,
      pool_source_status: 'available', pool_source: 'upstream subscription', pool_window_type: 'week', pool_window_anchor: '2026-09-14T00:00:00Z',
      employee_summaries: [], usage_trend: [],
    })
    listSubscriptionAllocations.mockResolvedValue({
      subscription_id: 9, window_type: 'week', window_anchor: '2026-09-14T00:00:00Z',
      authoritative_limit: '2400', allocated_total: '1980', unallocated_total: '420', overallocated_by: '186',
      pool_source_status: 'available', warning: 'allocation exceeds authoritative limit',
      items: [
        { employee_id: 1, email: 'a@example.com', department_id: 10, allocation_id: 1, allocation_version: 3, configured_credit: '220', usage_credit: '198', remaining_credit: '22', overage_credit: '3.2', status: 'overage' },
        { employee_id: 2, email: 'b@example.com', department_id: null, allocation_id: 2, allocation_version: 1, configured_credit: '180', usage_credit: '142', remaining_credit: '38', overage_credit: '0', status: 'normal' },
      ],
    })
    getAllocationSummary.mockResolvedValue({ allocation_id: 1, allocation_version: 3, configured_credit: '220', usage_credit: '198', remaining_credit: '22', overage_credit: '3.2', allocated_total: '1980', overallocated_by: '186' })
    setAllocation.mockResolvedValue({ id: 1, version: 4 })
    listDepartments.mockResolvedValue([{ id: 10, name: '研发中心' }])
    listEmployees.mockResolvedValue([{ id: 1, email: 'a@example.com', status: 'active', must_change_password: false }, { id: 2, email: 'b@example.com', status: 'active', must_change_password: false }])
  })

  it('renders pool stats, per-employee status and overallocation warning', async () => {
    const wrapper = mount(EnterpriseAllocationView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    expect(wrapper.text()).toContain('2400')
    expect(wrapper.text()).toContain('1980')
    expect(wrapper.text()).toContain('420')
    expect(wrapper.text()).toContain('企业池已超分配')
    expect(wrapper.text()).toContain('a@example.com')
    expect(wrapper.text()).toContain('研发中心')
    expect(wrapper.text()).toContain('有超用')
    expect(wrapper.text()).toContain('正常')
    wrapper.unmount()
  })

  it('submits an allocation adjustment carrying expected_version and reason', async () => {
    const wrapper = mount(EnterpriseAllocationView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    const buttons = wrapper.findAll('button').filter((button) => button.text() === '调整')
    await buttons[0]!.trigger('click')
    await flushPromises()

    const creditInput = wrapper.find('.el-dialog input[placeholder="非负数值字符串"]')
    await creditInput.setValue('260')
    const reasonInput = wrapper.find('.el-dialog textarea')
    await reasonInput.setValue('季度调薪后调整')

    const saveButton = wrapper.findAll('.el-dialog__footer button').find((button) => button.text() === '保存')
    await saveButton!.trigger('click')
    await flushPromises()

    expect(setAllocation).toHaveBeenCalledWith(9, 1, expect.objectContaining({
      enterprise_id: 7,
      window_type: 'week',
      window_anchor: '2026-09-14T00:00:00Z',
      credit: '260',
      expected_version: 3,
      reason: '季度调薪后调整',
    }))
    wrapper.unmount()
  })

  it('shows a no-subscription notice when the enterprise has no active subscription', async () => {
    getWorkbenchSummary.mockResolvedValueOnce({
      total_usage_credit: '0', total_requests: 0, employee_count: 0, active_employee_count: 0,
      subscription_id: null, subscription_status: '', subscription_plan: '',
      enterprise_pool_limit: '0', enterprise_pool_used: '0', enterprise_pool_remaining: '0', enterprise_pool_exhausted: false,
      pool_source_status: 'unavailable', pool_source: '', pool_window_type: 'week',
      employee_summaries: [], usage_trend: [],
    })
    const wrapper = mount(EnterpriseAllocationView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    expect(wrapper.text()).toContain('暂无生效订阅')
    expect(listSubscriptionAllocations).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('marks the source unavailable when the summary request fails', async () => {
    getWorkbenchSummary.mockRejectedValueOnce(new Error('source unavailable'))
    const wrapper = mount(EnterpriseAllocationView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    expect(wrapper.text()).toContain('数据来源暂时不可用')
    wrapper.unmount()
  })
})
