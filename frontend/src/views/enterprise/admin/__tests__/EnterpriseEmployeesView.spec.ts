import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import EnterpriseEmployeesView from '../EnterpriseEmployeesView.vue'

const { listEmployees, listDepartments, updateEmployee, pushMock } = vi.hoisted(() => ({
  listEmployees: vi.fn(),
  listDepartments: vi.fn(),
  updateEmployee: vi.fn(),
  pushMock: vi.fn(),
}))

vi.mock('@/api/enterprise', () => ({
  enterpriseAPI: { listEmployees, listDepartments, updateEmployee },
  isEnterpriseEmployeeVersionConflict: (error: unknown) => (error as { reason?: string })?.reason === 'EMPLOYEE_VERSION_CONFLICT',
  isEnterpriseEmployeeDepartmentInvalid: (error: unknown) => (error as { reason?: string })?.reason === 'ENTERPRISE_EMPLOYEE_DEPARTMENT_INVALID',
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: pushMock }),
}))

const baseEmployees = [
  { id: 10, email: 'employee-a@example.com', status: 'active' as const, department_id: 7, must_change_password: false, version: 3 },
]

const baseDepartments = [{ id: 7, name: '研发中心', created_at: '2026-01-01T00:00:00Z' }]

describe('EnterpriseEmployeesView', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    listEmployees.mockResolvedValue(baseEmployees)
    listDepartments.mockResolvedValue(baseDepartments)
  })

  it('keeps the edit dialog open and refills it with the latest department on a version conflict, instead of closing it', async () => {
    const wrapper = mount(EnterpriseEmployeesView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    const editButton = wrapper.findAll('button').find((button) => button.text() === '编辑')
    await editButton?.trigger('click')
    await flushPromises()
    expect(wrapper.find('.el-dialog').exists()).toBe(true)

    updateEmployee.mockRejectedValueOnce({ reason: 'EMPLOYEE_VERSION_CONFLICT' })
    listEmployees.mockResolvedValueOnce([{ ...baseEmployees[0], department_id: 9, version: 4 }])
    listDepartments.mockResolvedValueOnce([...baseDepartments, { id: 9, name: '市场中心', created_at: '2026-01-01T00:00:00Z' }])

    const saveButton = wrapper.findAll('.el-dialog__footer button').find((button) => button.text() === '保存')
    await saveButton?.trigger('click')
    await flushPromises()

    const dialog = wrapper.find('.el-dialog')
    expect(dialog.exists()).toBe(true)
    expect(dialog.find('.el-select__selected-item.el-select__placeholder').text()).toBe('市场中心')
    wrapper.unmount()
  })

  it('keeps the edit dialog open with a readable error when the selected department is invalid, so the admin can pick another department without reopening the dialog', async () => {
    const wrapper = mount(EnterpriseEmployeesView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    const editButton = wrapper.findAll('button').find((button) => button.text() === '编辑')
    await editButton?.trigger('click')
    await flushPromises()
    expect(wrapper.find('.el-dialog').exists()).toBe(true)

    updateEmployee.mockRejectedValueOnce({ status: 400, reason: 'ENTERPRISE_EMPLOYEE_DEPARTMENT_INVALID', message: 'selected department is invalid' })

    const saveButton = wrapper.findAll('.el-dialog__footer button').find((button) => button.text() === '保存')
    await saveButton?.trigger('click')
    await flushPromises()

    expect(wrapper.find('.el-dialog').exists()).toBe(true)
    expect(listEmployees).toHaveBeenCalledTimes(1)
    expect(document.body.textContent).toContain('所选部门无效')
    wrapper.unmount()
  })
})
