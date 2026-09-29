import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import EnterpriseAllocationView from '../EnterpriseAllocationView.vue'
import { useEnterpriseAuthStore } from '@/stores/enterpriseAuth'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: (await import('@/views/admin/__tests__/enterpriseTestI18n')).useEnterpriseTestI18n,
}))

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

// BaseDialog / Select 都 Teleport 到 body，测试里就地渲染以便在 wrapper 内查询
function mountView() {
  return mount(EnterpriseAllocationView, { global: { stubs: { teleport: true } } })
}

const clickText = async (wrapper: ReturnType<typeof mountView>, text: string) => {
  const button = wrapper.findAll('button').find((item) => item.text() === text)
  expect(button, `未找到按钮：${text}`).toBeDefined()
  await button!.trigger('click')
  await flushPromises()
}

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

  it('does not flash the no-subscription empty state on the first frame while data is loading (SHAN-392 rework)', async () => {
    let release: (value: unknown) => void = () => {}
    getWorkbenchSummary.mockReturnValue(new Promise((resolve) => { release = resolve }))
    const wrapper = mountView()

    expect(wrapper.text()).not.toContain('暂无生效订阅')

    release({
      total_usage_credit: '0', total_requests: 0, employee_count: 0, active_employee_count: 0,
      subscription_id: 0, subscription_status: 'none', subscription_plan: '',
      enterprise_pool_limit: '0', enterprise_pool_used: '0', enterprise_pool_remaining: '0', enterprise_pool_exhausted: false,
      pool_source_status: 'available', pool_source: '', pool_window_type: 'week', pool_window_anchor: '2026-09-14T00:00:00Z',
      employee_summaries: [], usage_trend: [],
    })
    await flushPromises()
    expect(wrapper.text()).toContain('暂无生效订阅')
    wrapper.unmount()
  })

  it('renders pool stats, per-employee status and overallocation warning', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('2400')
    expect(wrapper.text()).toContain('1980')
    expect(wrapper.text()).toContain('420')
    expect(wrapper.text()).toContain('企业池已超分配')
    expect(wrapper.text()).toContain('a@example.com')
    expect(wrapper.text()).toContain('研发中心')
    expect(wrapper.text()).toContain('超用量')
    expect(wrapper.text()).toContain('正常')
    wrapper.unmount()
  })

  it('renders the glossary-mapped column headers instead of raw English enum words', async () => {
    const wrapper = mountView()
    await flushPromises()

    // 表头必须走 S5 术语层，不得残留后端英文枚举词（后端 warning 原文不在此断言范围内）
    const headers = wrapper.findAll('th').map((header) => header.text())
    expect(headers).toContain('额度')
    expect(headers).toContain('剩余额度')
    expect(headers).toContain('超用量')
    expect(headers.join(' ')).not.toMatch(/allocation|overage|remaining/i)
    expect(wrapper.text()).toContain('企业总池')
    wrapper.unmount()
  })

  it('submits an allocation adjustment carrying expected_version and reason', async () => {
    const wrapper = mountView()
    await flushPromises()

    await clickText(wrapper, '调整')

    await wrapper.get('input[placeholder="非负数值字符串"]').setValue('260')
    await wrapper.get('#allocation-reason').setValue('季度调薪后调整')

    await clickText(wrapper, '保存')

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
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('暂无生效订阅')
    expect(listSubscriptionAllocations).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('marks the source unavailable when the summary request fails', async () => {
    getWorkbenchSummary.mockRejectedValueOnce(new Error('source unavailable'))
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('数据来源暂时不可用')
    wrapper.unmount()
  })
})
