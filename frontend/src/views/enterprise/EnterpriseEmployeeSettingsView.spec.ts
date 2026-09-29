import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import EnterpriseEmployeeSettingsView from './EnterpriseEmployeeSettingsView.vue'

vi.mock('vue-i18n', async () => ({
  ...(await vi.importActual<typeof import('vue-i18n')>('vue-i18n')),
  useI18n: (await import('@/views/admin/__tests__/enterpriseTestI18n')).useEnterpriseTestI18n,
}))

// BaseDialog 通过 Teleport 渲染到 body，就地展开后断言才能落在 wrapper 内
const mountOptions = { global: { stubs: { teleport: true } } }
const confirmDialog = (wrapper: ReturnType<typeof mount>) => wrapper.findComponent(ConfirmDialog)

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
    setActivePinia(createPinia())
    vi.resetAllMocks()
    getEmployeeProfile.mockResolvedValue({ id: 1, email: 'employee@example.com', status: 'active', must_change_password: false, version: 1 })
    listSessions.mockResolvedValue([currentSession, otherSession])
  })

  it('masks IP and user-agent for other devices and tags the current one, never rendering raw values', async () => {
    const wrapper = mount(EnterpriseEmployeeSettingsView, mountOptions)
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
    const wrapper = mount(EnterpriseEmployeeSettingsView, mountOptions)
    await flushPromises()
    expect(wrapper.find('[data-testid="sessions-error"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="sessions-empty"]').exists()).toBe(false)
  })

  it('blocks password submission until length, character-class and confirmation rules all pass', async () => {
    const wrapper = mount(EnterpriseEmployeeSettingsView, mountOptions)
    await flushPromises()
    await wrapper.get('[data-testid="current-password"] input').setValue('old-password-1')
    await wrapper.get('[data-testid="new-password"] input').setValue('short1')
    await wrapper.get('[data-testid="confirm-password"] input').setValue('short1')
    expect(wrapper.get('[data-testid="password-validation-error"]').text()).toContain('至少需要 8 个字符')

    // 无组成复杂度要求：纯字母 18 位直接满足策略
    await wrapper.get('[data-testid="new-password"] input').setValue('longenoughpassword')
    await wrapper.get('[data-testid="confirm-password"] input').setValue('longenoughpassword')
    expect(wrapper.get('[data-testid="password-policy-ok"]').exists()).toBe(true)

    await wrapper.get('[data-testid="new-password"] input').setValue('longenough12345')
    await wrapper.get('[data-testid="confirm-password"] input').setValue('mismatch12345')
    expect(wrapper.get('[data-testid="password-validation-error"]').text()).toContain('不一致')

    await wrapper.get('[data-testid="new-password"] input').setValue('longenough12345')
    await wrapper.get('[data-testid="confirm-password"] input').setValue('longenough12345')
    expect(wrapper.find('[data-testid="password-validation-error"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="password-policy-ok"]').exists()).toBe(true)

    changePassword.mockResolvedValue({ success: true })
    // 提交按钮是表单 submit 按钮，jsdom 不会由点击派生 submit 事件，直接驱动表单提交
    expect(wrapper.get('[data-testid="submit-password"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(changePassword).toHaveBeenCalledWith('old-password-1', 'longenough12345')
    expect(clear).toHaveBeenCalled()
    expect(replace).toHaveBeenCalledWith('/enterprise/login')
  })

  it('revokes a single non-current session after confirmation and reloads the list', async () => {
    revokeSession.mockResolvedValue({ success: true })
    const wrapper = mount(EnterpriseEmployeeSettingsView, mountOptions)
    await flushPromises()
    await wrapper.get('[data-testid="revoke-session"]').trigger('click')
    await flushPromises()
    expect(revokeSession).not.toHaveBeenCalled()

    confirmDialog(wrapper).vm.$emit('confirm')
    await flushPromises()
    expect(revokeSession).toHaveBeenCalledWith('sess-other')
    expect(listSessions).toHaveBeenCalledTimes(2)
  })

  it('does not call revoke when the confirmation dialog is cancelled', async () => {
    const wrapper = mount(EnterpriseEmployeeSettingsView, mountOptions)
    await flushPromises()
    await wrapper.get('[data-testid="revoke-session"]').trigger('click')
    await flushPromises()
    expect(confirmDialog(wrapper).props('show')).toBe(true)

    confirmDialog(wrapper).vm.$emit('cancel')
    await flushPromises()
    expect(confirmDialog(wrapper).props('show')).toBe(false)
    expect(revokeSession).not.toHaveBeenCalled()
  })

  it('logs out only the current session via the auth store, without revoking all devices', async () => {
    logout.mockResolvedValue(undefined)
    const wrapper = mount(EnterpriseEmployeeSettingsView, mountOptions)
    await flushPromises()
    await wrapper.get('[data-testid="logout-current"]').trigger('click')
    await flushPromises()
    confirmDialog(wrapper).vm.$emit('confirm')
    await flushPromises()
    expect(logout).toHaveBeenCalledTimes(1)
    expect(revokeAllSessions).not.toHaveBeenCalled()
    expect(replace).toHaveBeenCalledWith('/enterprise/login')
  })

  it('revokes all sessions and clears local auth state when logging out everywhere', async () => {
    revokeAllSessions.mockResolvedValue({ success: true })
    const wrapper = mount(EnterpriseEmployeeSettingsView, mountOptions)
    await flushPromises()
    await wrapper.get('[data-testid="logout-all"]').trigger('click')
    await flushPromises()
    confirmDialog(wrapper).vm.$emit('confirm')
    await flushPromises()
    expect(revokeAllSessions).toHaveBeenCalledTimes(1)
    expect(clear).toHaveBeenCalled()
    expect(replace).toHaveBeenCalledWith('/enterprise/login')
  })

  it('shows the sessions loading state before the session list resolves', async () => {
    let resolveSessions: (value: unknown) => void = () => undefined
    listSessions.mockReturnValue(new Promise((resolve) => { resolveSessions = resolve }))
    const wrapper = mount(EnterpriseEmployeeSettingsView, mountOptions)
    await nextTick()

    expect(wrapper.find('[data-testid="sessions-loading"]').exists()).toBe(true)

    resolveSessions([])
    await flushPromises()
    expect(wrapper.find('[data-testid="sessions-loading"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="sessions-empty"]').exists()).toBe(true)
  })

  it('recovers the session list through the retry action after a failure', async () => {
    listSessions.mockRejectedValueOnce(new Error('boom')).mockResolvedValueOnce([currentSession])
    const wrapper = mount(EnterpriseEmployeeSettingsView, mountOptions)
    await flushPromises()
    expect(wrapper.find('[data-testid="sessions-error"]').exists()).toBe(true)

    await wrapper.get('[data-testid="sessions-retry"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="sessions-error"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-testid="session-row"]')).toHaveLength(1)
  })

  it('surfaces a fixed message instead of echoing the backend error when the profile source fails', async () => {
    const backendInternals = 'INTERNAL-TRACE-ID-90aa · pg: relation "enterprise_employees" does not exist'
    getEmployeeProfile.mockRejectedValueOnce(new Error(backendInternals))
    const wrapper = mount(EnterpriseEmployeeSettingsView, mountOptions)
    await flushPromises()

    // 正向对照：同一断言方式确实能检出已渲染文本，证明下面的负向断言不是空断言
    expect(wrapper.text()).toContain('个人设置')
    expect(wrapper.text()).not.toContain(backendInternals)
    expect(wrapper.text()).not.toContain('INTERNAL-TRACE-ID-90aa')
  })
})
