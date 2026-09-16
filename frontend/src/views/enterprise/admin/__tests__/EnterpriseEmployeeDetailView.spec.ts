import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import EnterpriseEmployeeDetailView from '../EnterpriseEmployeeDetailView.vue'

const { getEmployee, listDepartments, listWorkbenchUsage, getWorkbenchSummary, listWorkbenchAuditEvents, updateEmployee, pushMock } = vi.hoisted(() => ({
  getEmployee: vi.fn(),
  listDepartments: vi.fn(),
  listWorkbenchUsage: vi.fn(),
  getWorkbenchSummary: vi.fn(),
  listWorkbenchAuditEvents: vi.fn(),
  updateEmployee: vi.fn(),
  pushMock: vi.fn(),
}))

vi.mock('@/api/enterprise', () => ({
  enterpriseAPI: { getEmployee, listDepartments, listWorkbenchUsage, getWorkbenchSummary, listWorkbenchAuditEvents, updateEmployee },
  isEnterpriseEmployeeVersionConflict: (error: unknown) => (error as { reason?: string })?.reason === 'EMPLOYEE_VERSION_CONFLICT',
  isEnterpriseEmployeeDepartmentInvalid: (error: unknown) => (error as { reason?: string })?.reason === 'ENTERPRISE_EMPLOYEE_DEPARTMENT_INVALID',
}))

vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { id: '42' } }),
  useRouter: () => ({ push: pushMock }),
}))

const baseDetail = {
  id: 42,
  email: 'employee-a@example.com',
  status: 'active' as const,
  department_id: 7,
  department_name: '研发中心',
  must_change_password: false,
  version: 3,
  created_at: '2026-01-08T09:24:00Z',
  current_key: {
    id: 1, masked_key: 'sk-****abcd', name: 'default', status: 'active' as const,
    quota: 1000, quota_used: 120, rate_limit_5h: 50, rate_limit_1d: 200, rate_limit_7d: 500,
    usage_5h: 10, usage_1d: 40, usage_7d: 90, created_at: '2026-01-08T09:24:00Z', updated_at: '2026-01-08T09:24:00Z',
  },
}

describe('EnterpriseEmployeeDetailView', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    getEmployee.mockResolvedValue(baseDetail)
    listDepartments.mockResolvedValue([{ id: 7, name: '研发中心', created_at: '2026-01-01T00:00:00Z' }])
    listWorkbenchUsage.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, pages: 1 })
    getWorkbenchSummary.mockResolvedValue({ total_usage_credit: '0', total_requests: 0, employee_count: 1, active_employee_count: 1, employee_summaries: [] })
    listWorkbenchAuditEvents.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, pages: 1 })
  })

  it('masks employee_id and email in the header and info list, and shows the masked API key', async () => {
    const wrapper = mount(EnterpriseEmployeeDetailView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    expect(wrapper.text()).toContain('••••••••••')
    expect(wrapper.text()).toContain('e***@example.com')
    expect(wrapper.text()).not.toContain('employee-a@example.com')
    expect(wrapper.text()).toContain('sk-****abcd')
    wrapper.unmount()
  })

  it('lazy-loads usage with the employee_id filter only when the tab is activated, and shows the empty state', async () => {
    const wrapper = mount(EnterpriseEmployeeDetailView, { global: { plugins: [ElementPlus] } })
    await flushPromises()
    expect(listWorkbenchUsage).not.toHaveBeenCalled()

    const usageTab = wrapper.findAll('.el-tabs__item').find((item) => item.text() === '额度与用量')
    await usageTab?.trigger('click')
    await flushPromises()

    expect(listWorkbenchUsage.mock.calls[0]?.[0]).toMatchObject({ employee_id: 42 })
    expect(getWorkbenchSummary.mock.calls[0]?.[0]).toMatchObject({ employee_id: 42 })
    expect(wrapper.text()).toContain('暂无用量记录')
    wrapper.unmount()
  })

  it('shows an error state with retry when the employee detail fails to load', async () => {
    getEmployee.mockRejectedValueOnce({ message: '员工不存在' })
    const wrapper = mount(EnterpriseEmployeeDetailView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    expect(wrapper.text()).toContain('员工不存在')

    getEmployee.mockResolvedValueOnce(baseDetail)
    const retryButton = wrapper.findAll('button').find((button) => button.text().includes('重试'))
    await retryButton?.trigger('click')
    await flushPromises()
    expect(getEmployee).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('shows the empty state when the employee has no current API key', async () => {
    getEmployee.mockResolvedValueOnce({ ...baseDetail, current_key: null })
    const wrapper = mount(EnterpriseEmployeeDetailView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    const keyTab = wrapper.findAll('.el-tabs__item').find((item) => item.text() === 'API Key')
    await keyTab?.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('该员工当前没有可用的 API Key')
    wrapper.unmount()
  })

  it('keeps the edit dialog open and refills it with the latest data on a version conflict, instead of closing it', async () => {
    const wrapper = mount(EnterpriseEmployeeDetailView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    const editButton = wrapper.findAll('button').find((button) => button.text() === '编辑资料')
    await editButton?.trigger('click')
    await flushPromises()
    expect(wrapper.find('.el-dialog').exists()).toBe(true)

    updateEmployee.mockRejectedValueOnce({ reason: 'EMPLOYEE_VERSION_CONFLICT' })
    const latestDetail = { ...baseDetail, department_id: 9, department_name: '市场中心', version: 4 }
    getEmployee.mockResolvedValueOnce(latestDetail)
    listDepartments.mockResolvedValueOnce([
      { id: 7, name: '研发中心', created_at: '2026-01-01T00:00:00Z' },
      { id: 9, name: '市场中心', created_at: '2026-01-01T00:00:00Z' },
    ])

    const saveButton = wrapper.findAll('.el-dialog__footer button').find((button) => button.text() === '保存')
    await saveButton?.trigger('click')
    await flushPromises()

    expect(wrapper.find('.el-dialog').exists()).toBe(true)
    expect(wrapper.find('.el-select__selected-item.el-select__placeholder').text()).toBe('市场中心')
    wrapper.unmount()
  })

  it('keeps the edit dialog open with a readable error when the selected department is invalid, so the admin can pick another department without reopening the dialog', async () => {
    const wrapper = mount(EnterpriseEmployeeDetailView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    const editButton = wrapper.findAll('button').find((button) => button.text() === '编辑资料')
    await editButton?.trigger('click')
    await flushPromises()
    expect(wrapper.find('.el-dialog').exists()).toBe(true)

    updateEmployee.mockRejectedValueOnce({ status: 400, reason: 'ENTERPRISE_EMPLOYEE_DEPARTMENT_INVALID', message: 'selected department is invalid' })

    const saveButton = wrapper.findAll('.el-dialog__footer button').find((button) => button.text() === '保存')
    await saveButton?.trigger('click')
    await flushPromises()

    expect(wrapper.find('.el-dialog').exists()).toBe(true)
    expect(getEmployee).toHaveBeenCalledTimes(1)
    expect(document.body.textContent).toContain('所选部门无效')
    wrapper.unmount()
  })

  it('invalidates the usage and history caches after a save, so the next tab visit refetches instead of showing stale data', async () => {
    updateEmployee.mockResolvedValueOnce(undefined)
    const wrapper = mount(EnterpriseEmployeeDetailView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    const usageTab = wrapper.findAll('.el-tabs__item').find((item) => item.text() === '额度与用量')
    await usageTab?.trigger('click')
    await flushPromises()
    expect(listWorkbenchUsage).toHaveBeenCalledTimes(1)

    const editButton = wrapper.findAll('button').find((button) => button.text() === '编辑资料')
    await editButton?.trigger('click')
    await flushPromises()
    const saveButton = wrapper.findAll('.el-dialog__footer button').find((button) => button.text() === '保存')
    await saveButton?.trigger('click')
    await flushPromises()

    expect(listWorkbenchUsage).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })
})
