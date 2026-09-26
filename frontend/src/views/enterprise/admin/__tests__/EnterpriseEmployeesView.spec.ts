import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessageBox } from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import EnterpriseEmployeesView from '../EnterpriseEmployeesView.vue'

const { listEmployees, listDepartments, updateEmployee, createEmployee, resetEmployeePassword, pushMock } = vi.hoisted(() => ({
  listEmployees: vi.fn(),
  listDepartments: vi.fn(),
  updateEmployee: vi.fn(),
  createEmployee: vi.fn(),
  resetEmployeePassword: vi.fn(),
  pushMock: vi.fn(),
}))

vi.mock('@/api/enterprise', () => ({
  enterpriseAPI: { listEmployees, listDepartments, updateEmployee, createEmployee, resetEmployeePassword },
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

  it('generates a 12-character initial password and shows a one-time delivery card with email, password and entry URL after creation', async () => {
    const wrapper = mount(EnterpriseEmployeesView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    const createButton = wrapper.findAll('button').find((button) => button.text() === '创建员工')
    await createButton?.trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="generate-initial-password"]').exists()).toBe(true)
    await wrapper.find('[data-testid="generate-initial-password"]').trigger('click')

    const emailInput = wrapper.find('.el-dialog input.el-input__inner')
    await emailInput.setValue('new-hire@example.com')

    createEmployee.mockResolvedValueOnce({ id: 11, email: 'new-hire@example.com', status: 'active', must_change_password: true })
    const confirmButton = wrapper.findAll('.el-dialog__footer button').find((button) => button.text() === '创建')
    await confirmButton?.trigger('click')
    await flushPromises()

    expect(createEmployee).toHaveBeenCalledTimes(1)
    const payload = createEmployee.mock.calls[0][0] as { email: string; initial_password: string }
    expect(payload.email).toBe('new-hire@example.com')
    expect(payload.initial_password).toHaveLength(12)
    expect(payload.initial_password).toMatch(/^[ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789]+$/)

    const delivery = wrapper.find('[data-testid="initial-password-delivery"]')
    expect(delivery.exists()).toBe(true)
    expect(delivery.text()).toContain('new-hire@example.com')
    expect(delivery.text()).toContain(payload.initial_password)
    expect(delivery.text()).toContain(window.location.origin)
    expect(delivery.text()).toContain('不会自动发送邮件')

    Object.assign(navigator, { clipboard: { writeText: vi.fn().mockResolvedValue(undefined) } })
    const copyButton = delivery.findAll('button').find((button) => button.text() === '复制交付信息')
    await copyButton?.trigger('click')
    await flushPromises()
    const copied = (navigator.clipboard.writeText as ReturnType<typeof vi.fn>).mock.calls[0][0] as string
    expect(copied).toContain('new-hire@example.com')
    expect(copied).toContain(payload.initial_password)
    expect(copied).toContain(window.location.origin)
    wrapper.unmount()
  })

  it('resets an active employee password from the row action and delivers the new initial password without any email claim', async () => {
    vi.spyOn(ElMessageBox, 'confirm').mockResolvedValue('confirm')
    const wrapper = mount(EnterpriseEmployeesView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    resetEmployeePassword.mockResolvedValueOnce({ initial_password: 'Rk7mPx2qWz94', must_change_password: true })
    const resetButton = wrapper.findAll('button').find((button) => button.text() === '重置密码')
    expect(resetButton).toBeDefined()
    await resetButton?.trigger('click')
    await flushPromises()

    expect(ElMessageBox.confirm).toHaveBeenCalledTimes(1)
    expect(resetEmployeePassword).toHaveBeenCalledWith(10)

    const delivery = wrapper.find('[data-testid="initial-password-delivery"]')
    expect(delivery.exists()).toBe(true)
    expect(delivery.text()).toContain('employee-a@example.com')
    expect(delivery.text()).toContain('Rk7mPx2qWz94')
    expect(delivery.text()).toContain('不会自动发送邮件')
    wrapper.unmount()
  })
})
