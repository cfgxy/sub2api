import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useAppStore } from '@/stores/app'
import EnterpriseAdminKeysView from './EnterpriseAdminKeysView.vue'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: (await import('@/views/admin/__tests__/enterpriseTestI18n')).useEnterpriseTestI18n,
}))

const { listAdminKeys, revokeAdminKey } = vi.hoisted(() => ({ listAdminKeys: vi.fn(), revokeAdminKey: vi.fn() }))
vi.mock('@/api/enterprise', () => ({ enterpriseAPI: { listAdminKeys, revokeAdminKey } }))

const now = new Date().toISOString()
const row = { assignment_id: 11, api_key_id: 1, employee_id: 9, employee_email: 'employee@example.com', generation: 2, status: 'active' as const, in_use: true, created_at: now, updated_at: now }
const archivedRow = { assignment_id: 10, api_key_id: 1, employee_id: 9, employee_email: 'employee@example.com', generation: 1, status: 'disabled' as const, in_use: false, created_at: now, updated_at: now }

function mountView() {
  const pinia = createPinia()
  setActivePinia(pinia)
  return mount(EnterpriseAdminKeysView, { global: { plugins: [pinia], stubs: { teleport: true } } })
}

describe('EnterpriseAdminKeysView', () => {
  beforeEach(() => { vi.resetAllMocks(); listAdminKeys.mockResolvedValue([row]) })

  it('shows employee identity and mapped key status without exposing credentials', async () => {
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('employee@example.com')
    expect(wrapper.text()).toContain('启用')
    expect(wrapper.text()).not.toContain('generation')
    expect(wrapper.text()).toContain('查看员工 Key 的归属与状态，可撤销已启用的 Key')
    expect(wrapper.findAll('th').map(header => header.text()).join(' ')).not.toContain('代次')
    expect(wrapper.text()).not.toMatch(/查看.*掩码|代轮换/)
    expect(wrapper.findAll('button').map(button => button.text()).join(' ')).not.toContain('轮换')
    wrapper.unmount()
  })

  it('marks the current binding and keeps ended bindings as history', async () => {
    listAdminKeys.mockResolvedValueOnce([row, archivedRow])
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.get('[data-testid="admin-key-in-use"]').text()).toContain('当前使用中')
    expect(wrapper.get('[data-testid="admin-key-archived"]').text()).toContain('历史记录')
    wrapper.unmount()
  })

  it('keeps generation in a read-only assignment history context', async () => {
    listAdminKeys.mockResolvedValueOnce([row, archivedRow])
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="admin-key-assignment-history"]').trigger('click')
    await flushPromises()
    const history = wrapper.get('[data-testid="admin-key-assignment-history-dialog"]')
    expect(history.text()).toContain('仅供审计')
    expect(history.text()).toContain('代次')
    expect(history.text()).toContain('不会影响当前使用')
    expect(history.text()).toContain('不能修改、切换或撤销绑定')
    expect(history.text()).toContain('当前使用中')
    expect(history.text()).toContain('历史记录')
    wrapper.unmount()
  })

  it('shows revoke only for the current usable Key, not a historical binding', async () => {
    listAdminKeys.mockResolvedValueOnce([row, { ...archivedRow, status: 'active' as const }])
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.findAll('[data-testid="admin-key-revoke"]')).toHaveLength(1)
    expect(wrapper.findAll('[data-testid="admin-key-assignment-history"]')).toHaveLength(2)
    wrapper.unmount()
  })

  it('shows a distinct load error and recovers on retry', async () => {
    listAdminKeys.mockRejectedValueOnce(new Error('network'))
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.get('[data-testid="admin-keys-load-error"]').text()).toContain('暂时不可用')
    await wrapper.get('[data-testid="admin-keys-load-error"] button').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('employee@example.com')
    wrapper.unmount()
  })

  it('renders the empty state without revoke actions', async () => {
    listAdminKeys.mockResolvedValueOnce([])
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('暂无员工 Key')
    expect(wrapper.find('[data-testid="admin-key-revoke"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('cancels confirmation without revoking and reloads after successful confirmation', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="admin-key-revoke"]').trigger('click')
    expect(wrapper.text()).toContain('撤销后该 Key 将立即无法调用')
    const buttons = wrapper.findAll('[role="dialog"] button')
    await buttons.find(item => item.text() === '取消')!.trigger('click')
    expect(revokeAdminKey).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="admin-key-revoke"]').trigger('click')
    await wrapper.findAll('[role="dialog"] button').find(item => item.text() === '确认')!.trigger('click')
    await flushPromises()
    expect(revokeAdminKey).toHaveBeenCalledWith(1, expect.any(String))
    expect(listAdminKeys).toHaveBeenCalledTimes(2)
    expect(useAppStore().toasts.map(toast => toast.message)).toContain('Key 已撤销')
    wrapper.unmount()
  })

  it('reports a rejected revoke without showing the raw server error', async () => {
    revokeAdminKey.mockRejectedValueOnce(new Error('sensitive message'))
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="admin-key-revoke"]').trigger('click')
    await wrapper.findAll('[role="dialog"] button').find(item => item.text() === '确认')!.trigger('click')
    await flushPromises()
    const messages = useAppStore().toasts.map(toast => toast.message)
    expect(messages).toContain('Key 撤销失败')
    expect(messages.join(' ')).not.toContain('sensitive message')
    wrapper.unmount()
  })
})
