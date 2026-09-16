import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessage, ElMessageBox } from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import EnterpriseAdminKeysView from './EnterpriseAdminKeysView.vue'

const { listAdminKeys, revokeAdminKey } = vi.hoisted(() => ({
  listAdminKeys: vi.fn(),
  revokeAdminKey: vi.fn(),
}))

vi.mock('@/api/enterprise', () => ({
  enterpriseAPI: { listAdminKeys, revokeAdminKey },
}))

const row = {
  api_key_id: 1,
  employee_id: 9,
  employee_email: 'employee@example.com',
  generation: 2,
  status: 'active' as const,
  created_at: new Date().toISOString(),
  updated_at: new Date().toISOString(),
}

describe('EnterpriseAdminKeysView', () => {
  beforeEach(() => {
    vi.resetAllMocks()
  })

  it('shows a distinct load-error state (not the empty state) when the key source is unavailable, and recovers on retry', async () => {
    listAdminKeys.mockRejectedValueOnce(new Error('network')).mockResolvedValueOnce([row])
    vi.spyOn(ElMessage, 'error').mockImplementation(() => undefined as never)
    const wrapper = mount(EnterpriseAdminKeysView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    expect(wrapper.find('[data-testid="admin-keys-load-error"]').exists()).toBe(true)
    expect(wrapper.find('.el-empty').exists()).toBe(false)

    await wrapper.get('[data-testid="admin-keys-load-retry"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="admin-keys-load-error"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('employee@example.com')
    wrapper.unmount()
  })

  it('shows the empty state (not the load-error state) when there are simply no keys', async () => {
    listAdminKeys.mockResolvedValueOnce([])
    const wrapper = mount(EnterpriseAdminKeysView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    expect(wrapper.find('.el-empty').exists()).toBe(true)
    expect(wrapper.find('[data-testid="admin-keys-load-error"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('does not call revoke when the admin cancels the confirmation', async () => {
    listAdminKeys.mockResolvedValueOnce([row])
    vi.spyOn(ElMessageBox, 'confirm').mockRejectedValueOnce(new Error('cancel'))
    const wrapper = mount(EnterpriseAdminKeysView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    await wrapper.get('[data-testid="admin-key-revoke"]').trigger('click')
    await flushPromises()

    expect(revokeAdminKey).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('shows a per-row revoking (processing) state while the revoke request is in flight, then reloads', async () => {
    listAdminKeys.mockResolvedValueOnce([row]).mockResolvedValueOnce([{ ...row, status: 'disabled' as const }])
    vi.spyOn(ElMessageBox, 'confirm').mockResolvedValueOnce('confirm' as never)
    vi.spyOn(ElMessage, 'success').mockImplementation(() => undefined as never)
    let resolveRevoke: (() => void) | undefined
    revokeAdminKey.mockReturnValueOnce(new Promise<void>((resolve) => { resolveRevoke = resolve }))
    const wrapper = mount(EnterpriseAdminKeysView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    const button = wrapper.get('[data-testid="admin-key-revoke"]')
    await button.trigger('click')
    await flushPromises()

    expect(button.classes()).toContain('is-loading')

    resolveRevoke?.()
    await flushPromises()

    expect(revokeAdminKey).toHaveBeenCalledTimes(1)
    expect(listAdminKeys).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('blocks a second revoke click while one is already in flight', async () => {
    listAdminKeys.mockResolvedValueOnce([row])
    vi.spyOn(ElMessageBox, 'confirm').mockResolvedValue('confirm' as never)
    vi.spyOn(ElMessage, 'success').mockImplementation(() => undefined as never)
    revokeAdminKey.mockReturnValueOnce(new Promise<void>(() => {}))
    const wrapper = mount(EnterpriseAdminKeysView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    const button = wrapper.get('[data-testid="admin-key-revoke"]')
    await button.trigger('click')
    await flushPromises()
    await button.trigger('click')
    await flushPromises()

    expect(revokeAdminKey).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('reports failure and clears the revoking state when the revoke call fails', async () => {
    listAdminKeys.mockResolvedValueOnce([row])
    vi.spyOn(ElMessageBox, 'confirm').mockResolvedValueOnce('confirm' as never)
    vi.spyOn(ElMessage, 'error').mockImplementation(() => undefined as never)
    revokeAdminKey.mockRejectedValueOnce(new Error('failed'))
    const wrapper = mount(EnterpriseAdminKeysView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    await wrapper.get('[data-testid="admin-key-revoke"]').trigger('click')
    await flushPromises()

    expect(ElMessage.error).toHaveBeenCalledWith('Key 撤销失败')
    expect(wrapper.get('[data-testid="admin-key-revoke"]').classes()).not.toContain('is-loading')
    wrapper.unmount()
  })
})
