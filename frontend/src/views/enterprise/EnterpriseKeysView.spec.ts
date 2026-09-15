import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessage, ElMessageBox } from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import EnterpriseKeysView from './EnterpriseKeysView.vue'

const { getCurrentKey, createKey, disableKey, rotateKey, isEnterpriseKeyMutationStateConflict, onBeforeRouteLeave } = vi.hoisted(() => ({
  getCurrentKey: vi.fn(),
  createKey: vi.fn(),
  disableKey: vi.fn(),
  rotateKey: vi.fn(),
  isEnterpriseKeyMutationStateConflict: vi.fn(),
  onBeforeRouteLeave: vi.fn(),
}))

vi.mock('@/api/enterprise', () => ({
  enterpriseAPI: {
    getCurrentKey,
    createKey,
    disableKey,
    rotateKey,
  },
  isEnterpriseKeyMutationStateConflict,
}))

vi.mock('vue-router', async () => {
  const actual = await vi.importActual<typeof import('vue-router')>('vue-router')
  return { ...actual, onBeforeRouteLeave }
})

describe('EnterpriseKeysView plaintext lifecycle', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    vi.resetAllMocks()
    getCurrentKey.mockResolvedValue(null)
    isEnterpriseKeyMutationStateConflict.mockReturnValue(false)
  })

  it('shows plaintext once and clears it on dialog close without storage or log persistence', async () => {
    const plaintext = `sk-runtime-${Math.random().toString(16).slice(2)}`
    const log = vi.spyOn(console, 'log').mockImplementation(() => undefined)
    const info = vi.spyOn(console, 'info').mockImplementation(() => undefined)
    const key = {
      id: 9, masked_key: 'sk-run...abcd', name: 'Enterprise employee key', status: 'active',
      quota: 25, quota_used: 7.5, rate_limit_5h: 5, rate_limit_1d: 10, rate_limit_7d: 20,
      usage_5h: 1, usage_1d: 2, usage_7d: 3, created_at: new Date().toISOString(), updated_at: new Date().toISOString(),
    }
    createKey.mockResolvedValue({ key, plaintext, replayed: false })
    const wrapper = mount(EnterpriseKeysView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    await wrapper.get('[data-testid="create-key"]').trigger('click')
    await flushPromises()
    expect((wrapper.get('[data-testid="plaintext-key"]').element as HTMLInputElement).value).toBe(plaintext)
    expect(localStorage.length).toBe(0)
    expect(sessionStorage.length).toBe(0)
    expect(JSON.stringify(log.mock.calls)).not.toContain(plaintext)
    expect(JSON.stringify(info.mock.calls)).not.toContain(plaintext)

    await wrapper.get('[data-testid="close-secret"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).not.toContain(plaintext)
    expect((wrapper.vm as unknown as { secret: string }).secret).toBe('')
    wrapper.unmount()
    expect(document.body.textContent).not.toContain(plaintext)
    expect(JSON.stringify(localStorage)).not.toContain(plaintext)
    expect(JSON.stringify(sessionStorage)).not.toContain(plaintext)
  })

  it('does not expose plaintext when an idempotent response is replayed', async () => {
    createKey.mockResolvedValue({
      key: {
        id: 10, masked_key: 'sk-rep...abcd', name: 'Enterprise employee key', status: 'active',
        quota: 0, quota_used: 0, rate_limit_5h: 0, rate_limit_1d: 0, rate_limit_7d: 0,
        usage_5h: 0, usage_1d: 0, usage_7d: 0, created_at: new Date().toISOString(), updated_at: new Date().toISOString(),
      },
      replayed: true,
      plaintext: 'sk-replayed-secret-must-not-render',
    })
    const wrapper = mount(EnterpriseKeysView, { global: { plugins: [ElementPlus] } })
    await flushPromises()
    await wrapper.get('[data-testid="create-key"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="plaintext-key"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('sk-rep...abcd')
    expect(wrapper.text()).not.toContain('sk-replayed-secret-must-not-render')
    wrapper.unmount()
  })

  it('keeps the enterprise key page on the weekly contract', async () => {
    getCurrentKey.mockResolvedValue({
      id: 10, masked_key: 'sk-zer...abcd', name: 'Enterprise employee key', status: 'active',
      quota: 25, quota_used: 0, rate_limit_5h: 5, rate_limit_1d: 10, rate_limit_7d: 20,
      usage_5h: 0, usage_1d: 0, usage_7d: 0, created_at: new Date().toISOString(), updated_at: new Date().toISOString(),
    })
    const wrapper = mount(EnterpriseKeysView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    expect(wrapper.text()).toContain('已使用 $0.00')
    expect(wrapper.text()).not.toContain('5 小时')
    expect(wrapper.text()).not.toContain('1 天')
    expect(wrapper.text()).not.toContain('7 天')
    wrapper.unmount()
  })

  it('shows rotated plaintext once and clears it when the dialog closes', async () => {
    const plaintext = 'sk-rotated-secret-must-not-remain'
    const key = {
      id: 12, masked_key: 'sk-old...abcd', name: 'Enterprise employee key', status: 'active' as const,
      quota: 25, quota_used: 7.5, rate_limit_5h: 5, rate_limit_1d: 10, rate_limit_7d: 20,
      usage_5h: 1.25, usage_1d: 2.5, usage_7d: 6.75, created_at: new Date().toISOString(), updated_at: new Date().toISOString(),
    }
    const successor = { ...key, id: 13, masked_key: 'sk-new...abcd' }
    getCurrentKey.mockResolvedValue(key)
    rotateKey.mockResolvedValue({ key: successor, plaintext, replayed: false })
    vi.spyOn(ElMessageBox, 'confirm').mockResolvedValue('confirm' as never)
    const wrapper = mount(EnterpriseKeysView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    await wrapper.get('[data-testid="rotate-key"]').trigger('click')
    await flushPromises()
    expect((wrapper.get('[data-testid="plaintext-key"]').element as HTMLInputElement).value).toBe(plaintext)

    await wrapper.get('[data-testid="close-secret"]').trigger('click')
    await flushPromises()
    expect((wrapper.vm as unknown as { secret: string }).secret).toBe('')
    expect(wrapper.text()).not.toContain(plaintext)
    expect(document.body.textContent).not.toContain(plaintext)
    expect(JSON.stringify(localStorage)).not.toContain(plaintext)
    expect(JSON.stringify(sessionStorage)).not.toContain(plaintext)
    wrapper.unmount()
  })

  it('does not call the lifecycle API when the employee cancels a disable action', async () => {
    getCurrentKey.mockResolvedValue({
      id: 14, masked_key: 'sk-dis...abcd', name: 'Enterprise employee key', status: 'active',
      quota: 25, quota_used: 7.5, rate_limit_5h: 5, rate_limit_1d: 10, rate_limit_7d: 20,
      usage_5h: 1.25, usage_1d: 2.5, usage_7d: 6.75, created_at: new Date().toISOString(), updated_at: new Date().toISOString(),
    })
    vi.spyOn(ElMessageBox, 'confirm').mockRejectedValue(new Error('cancelled'))
    const wrapper = mount(EnterpriseKeysView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    await wrapper.get('[data-testid="disable-key"]').trigger('click')
    await flushPromises()

    expect(disableKey).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('sk-dis...abcd')
    wrapper.unmount()
  })

  it('does not call the lifecycle API when the employee cancels a rotation', async () => {
    getCurrentKey.mockResolvedValue({
      id: 15, masked_key: 'sk-rot...abcd', name: 'Enterprise employee key', status: 'active',
      quota: 25, quota_used: 7.5, rate_limit_5h: 5, rate_limit_1d: 10, rate_limit_7d: 20,
      usage_5h: 1.25, usage_1d: 2.5, usage_7d: 6.75, created_at: new Date().toISOString(), updated_at: new Date().toISOString(),
    })
    vi.spyOn(ElMessageBox, 'confirm').mockRejectedValue(new Error('cancelled'))
    const wrapper = mount(EnterpriseKeysView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    await wrapper.get('[data-testid="rotate-key"]').trigger('click')
    await flushPromises()

    expect(rotateKey).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('sk-rot...abcd')
    wrapper.unmount()
  })

  it('reuses the same operation key after a failed create and blocks same-tick duplicate clicks', async () => {
    const key = {
      id: 16, masked_key: 'sk-ret...abcd', name: 'Enterprise employee key', status: 'active' as const,
      quota: 0, quota_used: 0, rate_limit_5h: 0, rate_limit_1d: 0, rate_limit_7d: 0,
      usage_5h: 0, usage_1d: 0, usage_7d: 0, created_at: new Date().toISOString(), updated_at: new Date().toISOString(),
    }
    createKey
      .mockRejectedValueOnce({ message: '网络异常，请重试' })
      .mockResolvedValueOnce({ key, replayed: true })
    vi.spyOn(ElMessage, 'error').mockImplementation(() => undefined as never)
    const wrapper = mount(EnterpriseKeysView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    const button = wrapper.get('[data-testid="create-key"]')
    await Promise.all([button.trigger('click'), button.trigger('click')])
    await flushPromises()
    expect(createKey).toHaveBeenCalledTimes(1)
    const firstOperationKey = createKey.mock.calls[0]?.[0]

    await button.trigger('click')
    await flushPromises()
    expect(createKey).toHaveBeenCalledTimes(2)
    expect(createKey.mock.calls[1]?.[0]).toBe(firstOperationKey)
    expect(wrapper.find('[data-testid="plaintext-key"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('refreshes the current key after a definite lifecycle conflict', async () => {
    const current = {
      id: 17, masked_key: 'sk-cur...abcd', name: 'Enterprise employee key', status: 'active' as const,
      quota: 0, quota_used: 0, rate_limit_5h: 0, rate_limit_1d: 0, rate_limit_7d: 0,
      usage_5h: 0, usage_1d: 0, usage_7d: 0, created_at: new Date().toISOString(), updated_at: new Date().toISOString(),
    }
    const successor = { ...current, id: 18, masked_key: 'sk-new...abcd' }
    getCurrentKey.mockResolvedValueOnce(current).mockResolvedValueOnce(successor)
    rotateKey.mockRejectedValueOnce({ reason: 'ENTERPRISE_KEY_VERSION_CONFLICT', message: 'changed' })
    isEnterpriseKeyMutationStateConflict.mockReturnValue(true)
    vi.spyOn(ElMessageBox, 'confirm').mockResolvedValue('confirm' as never)
    vi.spyOn(ElMessage, 'warning').mockImplementation(() => undefined as never)
    const wrapper = mount(EnterpriseKeysView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    await wrapper.get('[data-testid="rotate-key"]').trigger('click')
    await flushPromises()

    expect(getCurrentKey).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('sk-new...abcd')
    wrapper.unmount()
  })

  it('clears plaintext from local state and the DOM before route leave', async () => {
    const plaintext = 'sk-route-leave-secret-must-not-remain'
    createKey.mockResolvedValue({
      key: {
        id: 11, masked_key: 'sk-rou...abcd', name: 'Enterprise employee key', status: 'active',
        quota: 0, quota_used: 0, rate_limit_5h: 0, rate_limit_1d: 0, rate_limit_7d: 0,
        usage_5h: 0, usage_1d: 0, usage_7d: 0, created_at: new Date().toISOString(), updated_at: new Date().toISOString(),
      },
      plaintext,
      replayed: false,
    })
    const wrapper = mount(EnterpriseKeysView, { global: { plugins: [ElementPlus] } })
    await flushPromises()
    await wrapper.get('[data-testid="create-key"]').trigger('click')
    await flushPromises()
    expect((wrapper.get('[data-testid="plaintext-key"]').element as HTMLInputElement).value).toBe(plaintext)

    const routeLeave = onBeforeRouteLeave.mock.calls[0]?.[0] as (() => void) | undefined
    expect(routeLeave).toBeTypeOf('function')
    routeLeave?.()
    await flushPromises()

    expect((wrapper.vm as unknown as { secret: string }).secret).toBe('')
    expect(wrapper.text()).not.toContain(plaintext)
    expect(document.body.textContent).not.toContain(plaintext)
    expect(JSON.stringify(localStorage)).not.toContain(plaintext)
    expect(JSON.stringify(sessionStorage)).not.toContain(plaintext)
    wrapper.unmount()
  })
})
