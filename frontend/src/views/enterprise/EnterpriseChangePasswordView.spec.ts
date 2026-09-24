import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { describe, expect, it, vi } from 'vitest'
import EnterpriseChangePasswordView from './EnterpriseChangePasswordView.vue'
import source from './EnterpriseChangePasswordView.vue?raw'

const { changeInitialPassword, replace } = vi.hoisted(() => ({
  changeInitialPassword: vi.fn(),
  replace: vi.fn(),
}))

vi.mock('@/stores/enterpriseAuth', () => ({
  useEnterpriseAuthStore: () => ({ changeInitialPassword }),
}))

vi.mock('vue-router', async () => {
  const actual = await vi.importActual<typeof import('vue-router')>('vue-router')
  return { ...actual, useRouter: () => ({ replace }) }
}

)

// 与后端 enterpriseidentity 权威一致：最小长度 12（WEAK_PASSWORD = "password must be at least 12 characters"）
const validPassword = 'Ab1' + 'c'.repeat(9) // 12 位

const mountView = () =>
  mount(EnterpriseChangePasswordView, {
    global: {
      plugins: [ElementPlus],
      stubs: { EnterpriseAuthShell: { template: '<div><slot /></div>' } },
    },
  })

describe('EnterpriseChangePasswordView', () => {
  it('states the backend 12-character policy and never the 8-character one', () => {
    const wrapper = mountView()
    const text = wrapper.text()
    expect(text).toContain('至少 12 个字符')
    expect(text).not.toContain('8 个字符')
    wrapper.unmount()
  })

  it('wires the shared 12-character validator into the next-password rule', () => {
    expect(source).toContain("validatePassword(form.next)")
  })

  it('accepts a 12-character password and continues after the change', async () => {
    changeInitialPassword.mockClear()
    replace.mockClear()
    changeInitialPassword.mockResolvedValue(undefined)
    const wrapper = mountView()
    const inputs = wrapper.findAll('input[type="password"]')
    await inputs[0].setValue('init-password')
    await inputs[1].setValue(validPassword)
    await inputs[2].setValue(validPassword)
    await wrapper.find('button').trigger('click')
    await flushPromises()
    expect(changeInitialPassword).toHaveBeenCalledTimes(1)
    expect(replace).toHaveBeenCalledWith('/enterprise/login')
    wrapper.unmount()
  })
})
