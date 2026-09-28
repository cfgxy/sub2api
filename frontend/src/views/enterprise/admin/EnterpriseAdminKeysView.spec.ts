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

const row = { api_key_id: 1, employee_id: 9, employee_email: 'employee@example.com', generation: 2, status: 'active' as const, created_at: new Date().toISOString(), updated_at: new Date().toISOString() }

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
