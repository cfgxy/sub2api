import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import EnterpriseDepartmentsView from '../EnterpriseDepartmentsView.vue'
import EnterpriseEmployeesView from '../EnterpriseEmployeesView.vue'

const { listDepartments, listEmployees, pushMock } = vi.hoisted(() => ({
  listDepartments: vi.fn(),
  listEmployees: vi.fn(),
  pushMock: vi.fn(),
}))

vi.mock('@/api/enterprise', () => ({
  enterpriseAPI: { listDepartments, listEmployees },
  isEnterpriseEmployeeVersionConflict: (error: unknown) => (error as { reason?: string })?.reason === 'EMPLOYEE_VERSION_CONFLICT',
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: pushMock }),
}))

const baseDepartment = { id: 7, name: '研发中心', created_at: '2026-01-01T00:00:00Z' }
const baseEmployee = {
  id: 42,
  email: 'employee-a@example.com',
  status: 'active' as const,
  department_id: 7,
  must_change_password: false,
  version: 1,
  created_at: '2026-01-01T00:00:00Z',
}

describe('EnterpriseEmployeesView list load error state', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    listDepartments.mockResolvedValue([baseDepartment])
    listEmployees.mockResolvedValue([baseEmployee])
  })

  it('shows an error state with retry when the employees list fails to load, and reloads on retry', async () => {
    listEmployees.mockRejectedValueOnce({ message: '员工列表暂不可用' })
    const wrapper = mount(EnterpriseEmployeesView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    expect(wrapper.text()).toContain('员工列表暂不可用')
    expect(wrapper.text()).not.toContain('暂无匹配员工')

    const retryButton = wrapper.findAll('button').find((button) => button.text().includes('重试'))
    expect(retryButton).toBeTruthy()
    await retryButton?.trigger('click')
    await flushPromises()

    expect(listEmployees).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('employee-a@example.com')
    expect(wrapper.text()).not.toContain('员工列表暂不可用')
    wrapper.unmount()
  })
})

describe('EnterpriseDepartmentsView list load error state', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    listDepartments.mockResolvedValue([baseDepartment])
    listEmployees.mockResolvedValue([baseEmployee])
  })

  it('shows an error state with retry when the departments list fails to load, and reloads on retry', async () => {
    listDepartments.mockRejectedValueOnce({ message: '部门列表暂不可用' })
    const wrapper = mount(EnterpriseDepartmentsView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    expect(wrapper.text()).toContain('部门列表暂不可用')
    expect(wrapper.text()).not.toContain('尚未创建部门')

    const retryButton = wrapper.findAll('button').find((button) => button.text().includes('重试'))
    expect(retryButton).toBeTruthy()
    listDepartments.mockResolvedValueOnce([baseDepartment])
    await retryButton?.trigger('click')
    await flushPromises()

    expect(listDepartments).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('研发中心')
    expect(wrapper.text()).not.toContain('部门列表暂不可用')
    wrapper.unmount()
  })
})
