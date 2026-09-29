import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import EnterpriseForgotPasswordView from './EnterpriseForgotPasswordView.vue'
import EnterpriseResetPasswordView from './EnterpriseResetPasswordView.vue'
import EnterpriseSessionStatesView from './EnterpriseSessionStatesView.vue'

const { forgotPassword, resetPassword, getBrand, replace, query } = vi.hoisted(() => ({
  forgotPassword: vi.fn(),
  resetPassword: vi.fn(),
  getBrand: vi.fn(),
  replace: vi.fn(),
  query: { token: undefined as string | undefined, state: undefined as string | undefined },
}))

vi.mock('@/api/enterprise', () => ({ enterpriseAPI: { forgotPassword, resetPassword, getBrand } }))
vi.mock('@/stores', () => ({
  useAppStore: () => ({ siteName: 'Sub2API', siteLogo: '', cachedPublicSettings: null, publicSettingsLoaded: true, fetchPublicSettings: vi.fn() }),
}))
vi.mock('vue-router', async () => {
  const actual = await vi.importActual<typeof import('vue-router')>('vue-router')
  return { ...actual, useRoute: () => ({ query }), useRouter: () => ({ replace }) }
})

const mountView = (component: typeof EnterpriseForgotPasswordView) => mount(component, {
  global: { stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } } },
})

beforeEach(() => {
  vi.clearAllMocks()
  query.token = undefined
  query.state = undefined
  getBrand.mockResolvedValue({ enterprise_name: '示例企业', slogan: '企业访问' })
  replace.mockResolvedValue(undefined)
})

describe('EnterpriseForgotPasswordView', () => {
  it('uses AuthLayout, validates email and shows the neutral success state', async () => {
    forgotPassword.mockResolvedValue({ success: true })
    const wrapper = mountView(EnterpriseForgotPasswordView)
    expect(wrapper.findAll('.auth-layout')).toHaveLength(1)
    await wrapper.get('input[type="email"]').setValue('invalid')
    await wrapper.get('form').trigger('submit')
    expect(forgotPassword).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('请输入有效邮箱')
    await wrapper.get('input[type="email"]').setValue('member@example.test')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(forgotPassword).toHaveBeenCalledWith('member@example.test')
    expect(wrapper.text()).toContain('如果账号存在，重置邮件会发送到该邮箱')
    expect(wrapper.find('form').exists()).toBe(false)
    wrapper.unmount()
  })

  it('uses custom validation (novalidate) so the fixed email message is reachable instead of the native bubble', async () => {
    const wrapper = mountView(EnterpriseForgotPasswordView)
    expect(wrapper.get('form').attributes('novalidate')).toBeDefined()
    await wrapper.get('form').trigger('submit')
    expect(forgotPassword).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('请输入有效邮箱')
    wrapper.unmount()
  })

  it('keeps the form available after failure and never displays backend details', async () => {
    forgotPassword.mockRejectedValue({ message: 'sensitive backend detail' })
    const wrapper = mountView(EnterpriseForgotPasswordView)
    await wrapper.get('input[type="email"]').setValue('member@example.test')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('请稍后重试')
    expect(wrapper.text()).not.toContain('sensitive backend detail')
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('disables submission while sending and ignores repeated requests', async () => {
    let finishRequest!: (value: { success: boolean }) => void
    forgotPassword.mockReturnValue(new Promise((resolve) => { finishRequest = resolve }))
    const wrapper = mountView(EnterpriseForgotPasswordView)
    await wrapper.get('input[type="email"]').setValue('member@example.test')
    await wrapper.get('form').trigger('submit')
    await wrapper.get('form').trigger('submit')
    expect(forgotPassword).toHaveBeenCalledTimes(1)
    expect(wrapper.get('input[type="email"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('button[type="submit"]').text()).toContain('发送中')
    finishRequest({ success: true })
    await flushPromises()
    expect(wrapper.text()).toContain('请求已提交')
    wrapper.unmount()
  })
})

describe('EnterpriseResetPasswordView', () => {
  it('shows an actionable missing-link state without exposing a form', () => {
    const wrapper = mountView(EnterpriseResetPasswordView)
    expect(wrapper.findAll('.auth-layout')).toHaveLength(1)
    expect(wrapper.text()).toContain('重置链接缺少令牌')
    expect(wrapper.get('a[href="/enterprise/forgot-password"]').text()).toContain('重新申请')
    expect(wrapper.find('form').exists()).toBe(false)
    wrapper.unmount()
  })

  it('uses custom validation (novalidate) so empty fields show the fixed messages', async () => {
    query.token = 'fixture-token'
    const wrapper = mountView(EnterpriseResetPasswordView)
    expect(wrapper.get('form').attributes('novalidate')).toBeDefined()
    await wrapper.get('form').trigger('submit')
    expect(resetPassword).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('密码至少需要 8 个字符')
    wrapper.unmount()
  })

  it('accepts 8 plain characters, rejects shorter or mismatched passwords, then shows success', async () => {
    query.token = 'fixture-token'
    resetPassword.mockResolvedValue({ success: true })
    const wrapper = mountView(EnterpriseResetPasswordView)
    const fields = wrapper.findAll('input[type="password"]')
    await fields[0].setValue('abcdefg')
    await fields[1].setValue('different')
    await wrapper.get('form').trigger('submit')
    expect(resetPassword).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('密码至少需要 8 个字符')
    await fields[0].setValue('abcdefgh')
    await fields[1].setValue('abcdefgh')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(resetPassword).toHaveBeenCalledWith('fixture-token', 'abcdefgh')
    expect(wrapper.text()).toContain('密码已重置')
    expect(wrapper.find('form').exists()).toBe(false)
    wrapper.unmount()
  })

  it('keeps the invalid-token redirect and masks other server errors', async () => {
    query.token = 'fixture-token'
    resetPassword.mockRejectedValueOnce({ reason: 'PASSWORD_RESET_INVALID', message: 'sensitive backend detail' })
    const invalid = mountView(EnterpriseResetPasswordView)
    for (const field of invalid.findAll('input[type="password"]')) await field.setValue('abcdefgh')
    await invalid.get('form').trigger('submit')
    await flushPromises()
    expect(replace).toHaveBeenCalledWith('/enterprise/session-states?state=reset-link-invalid')
    invalid.unmount()

    resetPassword.mockRejectedValueOnce({ message: 'sensitive backend detail' })
    const failed = mountView(EnterpriseResetPasswordView)
    for (const field of failed.findAll('input[type="password"]')) await field.setValue('abcdefgh')
    await failed.get('form').trigger('submit')
    await flushPromises()
    expect(failed.get('[role="alert"]').text()).toContain('请稍后重试')
    expect(failed.text()).not.toContain('sensitive backend detail')
    failed.unmount()
  })

  it('disables fields during reset and ignores repeated submits', async () => {
    query.token = 'fixture-token'
    let finishRequest!: (value: { success: boolean }) => void
    resetPassword.mockReturnValue(new Promise((resolve) => { finishRequest = resolve }))
    const wrapper = mountView(EnterpriseResetPasswordView)
    for (const field of wrapper.findAll('input[type="password"]')) await field.setValue('abcdefgh')
    await wrapper.get('form').trigger('submit')
    await wrapper.get('form').trigger('submit')
    expect(resetPassword).toHaveBeenCalledTimes(1)
    expect(wrapper.get('input[type="password"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('button[type="submit"]').text()).toContain('重置中')
    finishRequest({ success: true })
    await flushPromises()
    expect(wrapper.text()).toContain('密码已重置')
    wrapper.unmount()
  })
})

describe('EnterpriseSessionStatesView', () => {
  it('displays only the selected state and retains its original action destination', () => {
    query.state = 'reset-link-invalid'
    const wrapper = mountView(EnterpriseSessionStatesView)
    expect(wrapper.findAll('.auth-layout')).toHaveLength(1)
    expect(wrapper.get('h2').text()).toBe('重置链接不可用')
    expect(wrapper.get('a.btn').attributes('href')).toBe('/enterprise/forgot-password')
    expect(wrapper.get('details').text()).toContain('登录会话已过期')
    wrapper.unmount()
  })

  it('falls back to expired-session guidance for an unknown state', () => {
    query.state = 'unknown'
    const wrapper = mountView(EnterpriseSessionStatesView)
    expect(wrapper.get('h2').text()).toBe('登录会话已过期')
    expect(wrapper.get('a.btn').attributes('href')).toBe('/enterprise/login')
    wrapper.unmount()
  })
})
