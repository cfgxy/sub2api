import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useAppStore } from '@/stores/app'
import EnterpriseEmployeesView from '../EnterpriseEmployeesView.vue'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: (await import('@/views/admin/__tests__/enterpriseTestI18n')).useEnterpriseTestI18n,
}))

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
const clipboardDescriptor = Object.getOwnPropertyDescriptor(navigator, 'clipboard')

// BaseDialog / Select 都 Teleport 到 body，测试里就地渲染以便在 wrapper 内查询
function mountView() {
  const pinia = createPinia()
  setActivePinia(pinia)
  return mount(EnterpriseEmployeesView, {
    attachTo: document.body,
    global: { plugins: [pinia], stubs: { teleport: true } },
  })
}

const toastMessages = () => useAppStore().toasts.map((toast) => toast.message).join('\n')
const clickText = async (wrapper: ReturnType<typeof mountView>, text: string) => {
  const button = wrapper.findAll('button').find((item) => item.text() === text)
  expect(button, `未找到按钮：${text}`).toBeDefined()
  await button!.trigger('click')
  await flushPromises()
}

async function createDeliveryCard() {
  const wrapper = mountView()
  await flushPromises()
  await clickText(wrapper, '创建员工')
  await wrapper.get('#create-email').setValue('new-hire@example.com')
  await wrapper.get('#create-password').setValue('abcdefgh')
  createEmployee.mockResolvedValueOnce({ id: 11, email: 'new-hire@example.com', status: 'active', must_change_password: true })
  await clickText(wrapper, '创建')
  expect(createEmployee).toHaveBeenCalledTimes(1)
  return wrapper
}

describe('EnterpriseEmployeesView', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    listEmployees.mockResolvedValue(baseEmployees)
    listDepartments.mockResolvedValue(baseDepartments)
  })

  afterEach(() => {
    vi.restoreAllMocks()
    if (clipboardDescriptor) Object.defineProperty(navigator, 'clipboard', clipboardDescriptor)
    else Reflect.deleteProperty(navigator, 'clipboard')
  })

  it('keeps the edit dialog open and refills it with the latest department on a version conflict, instead of closing it', async () => {
    const wrapper = mountView()
    await flushPromises()

    await clickText(wrapper, '编辑')
    expect(wrapper.find('#edit-department').exists()).toBe(true)

    updateEmployee.mockRejectedValueOnce({ reason: 'EMPLOYEE_VERSION_CONFLICT' })
    listEmployees.mockResolvedValueOnce([{ ...baseEmployees[0], department_id: 9, version: 4 }])
    listDepartments.mockResolvedValueOnce([...baseDepartments, { id: 9, name: '市场中心', created_at: '2026-01-01T00:00:00Z' }])

    await clickText(wrapper, '保存')

    const department = wrapper.find('#edit-department')
    expect(department.exists()).toBe(true)
    expect(department.get('.select-value').text()).toBe('市场中心')
    wrapper.unmount()
  })

  it('keeps the edit dialog open with a readable error when the selected department is invalid, so the admin can pick another department without reopening the dialog', async () => {
    const wrapper = mountView()
    await flushPromises()

    await clickText(wrapper, '编辑')
    expect(wrapper.find('#edit-department').exists()).toBe(true)

    updateEmployee.mockRejectedValueOnce({ status: 400, reason: 'ENTERPRISE_EMPLOYEE_DEPARTMENT_INVALID', message: 'selected department is invalid' })

    await clickText(wrapper, '保存')

    expect(wrapper.find('#edit-department').exists()).toBe(true)
    expect(listEmployees).toHaveBeenCalledTimes(1)
    expect(toastMessages()).toContain('所选部门无效')
    wrapper.unmount()
  })

  it('keeps the create dialog open with a visible duplicate-email hint on a 409, instead of an unhandled rejection (SHAN-392 rework)', async () => {
    const wrapper = mountView()
    await flushPromises()

    await clickText(wrapper, '创建员工')
    await wrapper.get('[data-testid="generate-initial-password"]').trigger('click')
    await wrapper.get('#create-email').setValue('dup@example.com')

    createEmployee.mockRejectedValueOnce({ status: 409, code: 'ENTERPRISE_CONFLICT', message: 'conflict' })
    await clickText(wrapper, '创建')

    expect(createEmployee).toHaveBeenCalledTimes(1)
    expect(wrapper.find('#create-email').exists()).toBe(true)
    expect(wrapper.text()).toContain('该邮箱已被使用，请更换邮箱')
    expect(toastMessages()).toContain('该邮箱已被使用，请更换邮箱')
    expect(wrapper.text()).not.toContain('复制交付信息')
    wrapper.unmount()
  })

  it('generates a 12-character initial password and shows a one-time delivery card with email, password and entry URL after creation', async () => {
    const wrapper = mountView()
    await flushPromises()

    await clickText(wrapper, '创建员工')

    expect(wrapper.find('[data-testid="generate-initial-password"]').exists()).toBe(true)
    await wrapper.get('[data-testid="generate-initial-password"]').trigger('click')
    await wrapper.get('#create-email').setValue('new-hire@example.com')

    createEmployee.mockResolvedValueOnce({ id: 11, email: 'new-hire@example.com', status: 'active', must_change_password: true })
    await clickText(wrapper, '创建')

    expect(createEmployee).toHaveBeenCalledTimes(1)
    const payload = createEmployee.mock.calls[0][0] as { email: string; initial_password: string }
    expect(payload.email).toBe('new-hire@example.com')
    expect(payload.initial_password).toHaveLength(12)
    expect(payload.initial_password).toMatch(/^[ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789]+$/)

    const delivery = wrapper.find('[data-testid="initial-password-delivery"]')
    expect(delivery.exists()).toBe(true)
    expect(delivery.get('[data-testid="delivery-email"]').text()).toBe('new-hire@example.com')
    expect(delivery.get('[data-testid="delivery-password"]').text()).toBe(payload.initial_password)
    expect(delivery.get('[data-testid="delivery-origin"]').text()).toBe(window.location.origin)
    expect(delivery.text()).toContain('不会自动发送邮件')

    Object.assign(navigator, { clipboard: { writeText: vi.fn().mockResolvedValue(undefined) } })
    await clickText(wrapper, '复制交付信息')
    const copied = (navigator.clipboard.writeText as ReturnType<typeof vi.fn>).mock.calls[0][0] as string
    expect(copied).toBe(`邮箱：new-hire@example.com\n初始密码：${payload.initial_password}\n入口域名：${window.location.origin}`)
    expect(toastMessages()).toContain('交付信息已复制')
    expect(wrapper.find('[data-testid="manual-copy-text"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it.each([
    ['HTTP 环境不支持剪贴板', false],
    ['剪贴板写入失败', true],
  ])('%s 时提示并选中完整交付信息供手动复制', async (_scenario, rejects) => {
    const clipboard = rejects ? { writeText: vi.fn().mockRejectedValue(new Error('not allowed')) } : undefined
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: clipboard })
    const wrapper = await createDeliveryCard()
    await clickText(wrapper, '复制交付信息')

    const manualCopy = wrapper.find<HTMLTextAreaElement>('[data-testid="manual-copy-text"]')
    expect(manualCopy.exists()).toBe(true)
    expect(manualCopy.element.value).toContain('邮箱：new-hire@example.com')
    expect(manualCopy.element.value).toContain('初始密码：abcdefgh')
    expect(manualCopy.element.value).toContain(`入口域名：${window.location.origin}`)
    expect(document.activeElement).toBe(manualCopy.element)
    expect(manualCopy.element.selectionStart).toBe(0)
    expect(manualCopy.element.selectionEnd).toBe(manualCopy.element.value.length)
    expect(wrapper.find('[data-testid="initial-password-delivery"]').text()).toContain('手动复制')
    expect(toastMessages()).toContain('无法自动复制')
    wrapper.unmount()
  })

  it('resets an active employee password from the row action and delivers the new initial password without any email claim', async () => {
    const wrapper = mountView()
    await flushPromises()

    resetEmployeePassword.mockResolvedValueOnce({ initial_password: 'Rk7mPx2qWz94', must_change_password: true })
    await clickText(wrapper, '重置密码')
    // 行内动作只打开确认对话框，确认前不得调用接口
    expect(resetEmployeePassword).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('重置员工密码')

    await clickText(wrapper, '确认')
    expect(resetEmployeePassword).toHaveBeenCalledWith(10)

    const delivery = wrapper.find('[data-testid="initial-password-delivery"]')
    expect(delivery.exists()).toBe(true)
    expect(delivery.get('[data-testid="delivery-email"]').text()).toBe('employee-a@example.com')
    expect(delivery.get('[data-testid="delivery-password"]').text()).toBe('Rk7mPx2qWz94')
    expect(delivery.text()).toContain('不会自动发送邮件')
    wrapper.unmount()
  })
})
