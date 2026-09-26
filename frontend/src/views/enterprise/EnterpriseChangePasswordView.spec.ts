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
  useEnterpriseAuthStore: () => ({ changeInitialPassword, principal: null }),
}))

vi.mock('vue-router', async () => {
  const actual = await vi.importActual<typeof import('vue-router')>('vue-router')
  return { ...actual, useRouter: () => ({ replace }) }
}

)

// 与后端 enterpriseidentity 权威一致：最小长度 8（WEAK_PASSWORD = "password must be at least 8 characters"）
const validPassword = 'Ab1' + 'c'.repeat(5) // 8 位，无组成复杂度要求

const mountView = () =>
  mount(EnterpriseChangePasswordView, {
    global: {
      plugins: [ElementPlus],
      stubs: { EnterpriseAuthShell: { template: '<div><slot /></div>' } },
    },
  })

describe('EnterpriseChangePasswordView', () => {
  it('states the backend 8-character policy and never the 12-character one', () => {
    const wrapper = mountView()
    const text = wrapper.text()
    expect(text).toContain('至少 8 个字符')
    expect(text).not.toContain('12 个字符')
    wrapper.unmount()
  })

  it('wires the shared validator into the next-password rule and never asks for the current password', () => {
    expect(source).toContain("validatePassword(form.next)")
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
    await wrapper.find('button').trigger('click')
    await flushPromises()
    expect(changeInitialPassword).toHaveBeenCalledTimes(1)
    expect(changeInitialPassword).toHaveBeenCalledWith(validPassword)
    expect(replace).toHaveBeenCalledWith('/enterprise/home')
    wrapper.unmount()
  })
})
