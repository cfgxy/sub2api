import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import EnterpriseChangePasswordView from './EnterpriseChangePasswordView.vue'
import source from './EnterpriseChangePasswordView.vue?raw'

const { changeInitialPassword, replace, getBrand } = vi.hoisted(() => ({
  changeInitialPassword: vi.fn(),
  replace: vi.fn(),
  getBrand: vi.fn(),
}))

vi.mock('@/stores/enterpriseAuth', () => ({
  useEnterpriseAuthStore: () => ({ changeInitialPassword, principal: null }),
}))
vi.mock('@/stores', () => ({
  useAppStore: () => ({ siteName: 'Sub2API', siteLogo: '', cachedPublicSettings: null, publicSettingsLoaded: true, fetchPublicSettings: vi.fn() }),
}))
vi.mock('@/api/enterprise', () => ({ enterpriseAPI: { getBrand } }))

vi.mock('vue-router', async () => {
  const actual = await vi.importActual<typeof import('vue-router')>('vue-router')
  return { ...actual, useRouter: () => ({ replace }) }
}

)

const validPassword = 'abcdefgh'

const mountView = () =>
  mount(EnterpriseChangePasswordView, {
    global: { stubs: { RouterLink: { template: '<a><slot /></a>' } } },
  })

beforeEach(() => {
  vi.clearAllMocks()
  getBrand.mockResolvedValue({ enterprise_name: '示例企业', slogan: '企业访问' })
  replace.mockResolvedValue(undefined)
})

describe('EnterpriseChangePasswordView', () => {
  it('states the backend 8-character policy and never the 12-character one', () => {
    const wrapper = mountView()
    const text = wrapper.text()
    expect(text).toContain('至少需要 8 个字符')
    expect(text).not.toContain('12 个字符')
    expect(wrapper.findAll('.auth-layout')).toHaveLength(1)
    expect(wrapper.get('form').attributes('aria-busy')).toBe('false')
    wrapper.unmount()
  })

  it('wires the shared validator into the next-password rule and never asks for the current password', () => {
    expect(source).toContain('validatePassword(form.next)')
    expect(source).not.toContain('form.current')
    expect(source).not.toContain('当前初始密码')
  })

  it('accepts an 8-character password and goes straight to the role homepage keeping the session', async () => {
    changeInitialPassword.mockClear()
    replace.mockClear()
    changeInitialPassword.mockResolvedValue(undefined)
    const wrapper = mountView()
    const inputs = wrapper.findAll('input[type="password"]')
    expect(inputs).toHaveLength(2)
    await inputs[0].setValue(validPassword)
    await inputs[1].setValue(validPassword)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(changeInitialPassword).toHaveBeenCalledTimes(1)
    expect(changeInitialPassword).toHaveBeenCalledWith(validPassword)
    expect(replace).toHaveBeenCalledWith('/enterprise/home')
    wrapper.unmount()
  })

  it('rejects short and mismatched passwords without a request', async () => {
    const wrapper = mountView()
    const inputs = wrapper.findAll('input[type="password"]')
    await inputs[0].setValue('abcdefg')
    await inputs[1].setValue('different')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.text()).toContain('密码至少需要 8 个字符')
    expect(wrapper.text()).toContain('两次输入的密码不一致')
    expect(changeInitialPassword).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('disables fields during submission, prevents duplicates and hides backend details on failure', async () => {
    let rejectRequest!: (error: unknown) => void
    changeInitialPassword.mockReturnValue(new Promise((_resolve, reject) => { rejectRequest = reject }))
    const wrapper = mountView()
    const inputs = wrapper.findAll('input[type="password"]')
    await inputs[0].setValue(validPassword)
    await inputs[1].setValue(validPassword)
    await wrapper.get('form').trigger('submit')
    await wrapper.get('form').trigger('submit')
    expect(changeInitialPassword).toHaveBeenCalledTimes(1)
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()
    rejectRequest({ message: 'sensitive backend detail' })
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('密码更新失败')
    expect(wrapper.text()).not.toContain('sensitive backend detail')
    wrapper.unmount()
  })
})
