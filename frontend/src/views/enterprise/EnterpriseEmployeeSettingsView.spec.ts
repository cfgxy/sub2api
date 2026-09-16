import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessageBox } from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import EnterpriseEmployeeSettingsView from './EnterpriseEmployeeSettingsView.vue'

const { getEmployeeProfile, listSessions, revokeSession, revokeAllSessions, changePassword, logout, clear, replace } = vi.hoisted(() => ({
  getEmployeeProfile: vi.fn(),
  listSessions: vi.fn(),
  revokeSession: vi.fn(),
  revokeAllSessions: vi.fn(),
  changePassword: vi.fn(),
  logout: vi.fn(),
  clear: vi.fn(),
  replace: vi.fn(),
}))

vi.mock('@/api/enterprise', () => ({
  enterpriseAPI: { getEmployeeProfile, listSessions, revokeSession, revokeAllSessions, changePassword },
}))

vi.mock('@/stores/enterpriseAuth', () => ({
  useEnterpriseAuthStore: () => ({ logout, clear }),
}))

vi.mock('vue-router', async () => {
  const actual = await vi.importActual<typeof import('vue-router')>('vue-router')
  return { ...actual, useRouter: () => ({ replace }) }
})

const currentSession = {
  id: 'sess-current', user_agent: 'Mozilla/5.0 Chrome/120.0 Windows', ip_address: '10.20.30.40',
  last_seen_at: new Date().toISOString(), created_at: new Date().toISOString(), expires_at: new Date().toISOString(), current: true,
}
const otherSession = {
  id: 'sess-other', user_agent: 'Mozilla/5.0 Safari/17.0 iPhone', ip_address: '203.0.113.9',
  last_seen_at: new Date().toISOString(), created_at: new Date().toISOString(), expires_at: new Date().toISOString(), current: false,
}

describe('EnterpriseEmployeeSettingsView', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    getEmployeeProfile.mockResolvedValue({ id: 1, email: 'employee@example.com', status: 'active', must_change_password: false, version: 1 })
    listSessions.mockResolvedValue([currentSession, otherSession])
  })

  it('masks IP and user-agent for other devices and tags the current one, never rendering raw values', async () => {
    const wrapper = mount(EnterpriseEmployeeSettingsView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    const html = wrapper.html()
    expect(html).not.toContain(currentSession.ip_address)
    expect(html).not.toContain(otherSession.ip_address)
    expect(html).not.toContain(otherSession.user_agent)
    expect(html).toContain('Safari · iOS')
    expect(wrapper.find('[data-testid="session-current-tag"]').exists()).toBe(true)
    const revokeButtons = wrapper.findAll('[data-testid="revoke-session"]')
    expect(revokeButtons).toHaveLength(1)
  })

  it('shows an explicit error state with retry when the session list fails, never a blank empty state', async () => {
    listSessions.mockRejectedValueOnce(new Error('boom'))
    const wrapper = mount(EnterpriseEmployeeSettingsView, { global: { plugins: [ElementPlus] } })
    await flushPromises()
    expect(wrapper.find('[data-testid="sessions-error"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="sessions-empty"]').exists()).toBe(false)
  })

  it('blocks password submission until length, character-class and confirmation rules all pass', async () => {
    const wrapper = mount(EnterpriseEmployeeSettingsView, { global: { plugins: [ElementPlus] } })
    await flushPromises()
    await wrapper.get('[data-testid="current-password"]').setValue('old-password-1')
    await wrapper.get('[data-testid="new-password"]').setValue('short1')
    await wrapper.get('[data-testid="confirm-password"]').setValue('short1')
    expect(wrapper.get('[data-testid="password-validation-error"]').text()).toContain('至少需要 12 个字符')

    await wrapper.get('[data-testid="new-password"]').setValue('longenoughpassword')
    await wrapper.get('[data-testid="confirm-password"]').setValue('longenoughpassword')
    expect(wrapper.get('[data-testid="password-validation-error"]').text()).toContain('字母和数字')

    await wrapper.get('[data-testid="new-password"]').setValue('longenough12345')
    await wrapper.get('[data-testid="confirm-password"]').setValue('mismatch12345')
    expect(wrapper.get('[data-testid="password-validation-error"]').text()).toContain('不一致')

    await wrapper.get('[data-testid="new-password"]').setValue('longenough12345')
    await wrapper.get('[data-testid="confirm-password"]').setValue('longenough12345')
    expect(wrapper.find('[data-testid="password-validation-error"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="password-policy-ok"]').exists()).toBe(true)

    changePassword.mockResolvedValue({ success: true })
    await wrapper.get('[data-testid="submit-password"]').trigger('click')
    await flushPromises()
    expect(changePassword).toHaveBeenCalledWith('old-password-1', 'longenough12345')
    expect(clear).toHaveBeenCalled()
    expect(replace).toHaveBeenCalledWith('/enterprise/login')
  })

  it('revokes a single non-current session after confirmation and reloads the list', async () => {
    vi.spyOn(ElMessageBox, 'confirm').mockResolvedValue('confirm' as never)
    revokeSession.mockResolvedValue({ success: true })
    const wrapper = mount(EnterpriseEmployeeSettingsView, { global: { plugins: [ElementPlus] } })
    await flushPromises()
    await wrapper.get('[data-testid="revoke-session"]').trigger('click')
    await flushPromises()
    expect(revokeSession).toHaveBeenCalledWith('sess-other')
    expect(listSessions).toHaveBeenCalledTimes(2)
  })

  it('does not call revoke when the confirmation dialog is cancelled', async () => {
    vi.spyOn(ElMessageBox, 'confirm').mockRejectedValue('cancel' as never)
    const wrapper = mount(EnterpriseEmployeeSettingsView, { global: { plugins: [ElementPlus] } })
    await flushPromises()
    await wrapper.get('[data-testid="revoke-session"]').trigger('click')
    await flushPromises()
    expect(revokeSession).not.toHaveBeenCalled()
  })

  it('logs out only the current session via the auth store, without revoking all devices', async () => {
    vi.spyOn(ElMessageBox, 'confirm').mockResolvedValue('confirm' as never)
    logout.mockResolvedValue(undefined)
    const wrapper = mount(EnterpriseEmployeeSettingsView, { global: { plugins: [ElementPlus] } })
    await flushPromises()
    await wrapper.get('[data-testid="logout-current"]').trigger('click')
    await flushPromises()
    expect(logout).toHaveBeenCalledTimes(1)
    expect(revokeAllSessions).not.toHaveBeenCalled()
    expect(replace).toHaveBeenCalledWith('/enterprise/login')
  })

  it('revokes all sessions and clears local auth state when logging out everywhere', async () => {
    vi.spyOn(ElMessageBox, 'confirm').mockResolvedValue('confirm' as never)
    revokeAllSessions.mockResolvedValue({ success: true })
    const wrapper = mount(EnterpriseEmployeeSettingsView, { global: { plugins: [ElementPlus] } })
    await flushPromises()
    await wrapper.get('[data-testid="logout-all"]').trigger('click')
    await flushPromises()
    expect(revokeAllSessions).toHaveBeenCalledTimes(1)
    expect(clear).toHaveBeenCalled()
    expect(replace).toHaveBeenCalledWith('/enterprise/login')
  })
})
