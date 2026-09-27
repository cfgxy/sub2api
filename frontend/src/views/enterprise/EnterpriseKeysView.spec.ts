import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessage, ElMessageBox } from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import EnterpriseKeysView from './EnterpriseKeysView.vue'

const { getCurrentKey, createKey, disableKey, rotateKey, fetchEnterpriseGuideModels, getPublicSettings, isEnterpriseKeyMutationStateConflict, onBeforeRouteLeave, push, driverMock } = vi.hoisted(() => ({
  getCurrentKey: vi.fn(),
  createKey: vi.fn(),
  disableKey: vi.fn(),
  rotateKey: vi.fn(),
  fetchEnterpriseGuideModels: vi.fn(),
  getPublicSettings: vi.fn(),
  isEnterpriseKeyMutationStateConflict: vi.fn(),
  onBeforeRouteLeave: vi.fn(),
  push: vi.fn(),
  driverMock: vi.fn(),
}))

vi.mock('@/api/enterprise', () => ({
  enterpriseAPI: {
    getCurrentKey,
    createKey,
    disableKey,
    rotateKey,
  },
  fetchEnterpriseGuideModels,
  isEnterpriseKeyMutationStateConflict,
}))
vi.mock('@/api/auth', () => ({ getPublicSettings }))

vi.mock('vue-router', async () => {
  const actual = await vi.importActual<typeof import('vue-router')>('vue-router')
  return { ...actual, onBeforeRouteLeave, useRouter: () => ({ push }) }
})

vi.mock('driver.js', () => ({
  driver: driverMock,
}))

describe('EnterpriseKeysView key lifecycle', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    vi.resetAllMocks()
    getCurrentKey.mockResolvedValue(null)
    getPublicSettings.mockResolvedValue({ api_base_url: 'https://tenant.example.test/v1', site_name: 'Test site' })
    fetchEnterpriseGuideModels.mockResolvedValue([
      { id: 'gpt-example', platform: 'openai' },
      { id: 'claude-example', platform: 'anthropic' },
    ])
    push.mockResolvedValue(undefined)
    driverMock.mockImplementation(() => ({ drive: vi.fn(), destroy: vi.fn(), getActiveIndex: () => 0, moveNext: vi.fn() }))
    isEnterpriseKeyMutationStateConflict.mockReturnValue(false)
  })

  it('clears the creation dialog without storing or logging the key', async () => {
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
    getCurrentKey.mockResolvedValueOnce(null).mockResolvedValue({ id: 10, masked_key: 'sk-rep...abcd', status: 'active', key: 'sk-test-FAKE-replayed',
      quota: 0, quota_used: 0, rate_limit_5h: 0, rate_limit_1d: 0, rate_limit_7d: 0,
      usage_5h: 0, usage_1d: 0, usage_7d: 0, created_at: new Date().toISOString(), updated_at: new Date().toISOString() })
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

  it('shows rotated plaintext and clears the dialog when it closes', async () => {
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

  it('shows a distinct load-error state (not the empty state) when the key source is unavailable, and recovers on retry', async () => {
    getCurrentKey.mockRejectedValueOnce(new Error('network')).mockResolvedValueOnce({
      id: 20, masked_key: 'sk-rec...abcd', name: 'Enterprise employee key', status: 'active',
      quota: 25, quota_used: 0, rate_limit_5h: 0, rate_limit_1d: 0, rate_limit_7d: 0,
      usage_5h: 0, usage_1d: 0, usage_7d: 0, created_at: new Date().toISOString(), updated_at: new Date().toISOString(),
    })
    vi.spyOn(ElMessage, 'error').mockImplementation(() => undefined as never)
    const wrapper = mount(EnterpriseKeysView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    expect(wrapper.find('[data-testid="keys-load-error"]').exists()).toBe(true)
    expect(wrapper.find('.el-empty').exists()).toBe(false)

    await wrapper.get('[data-testid="keys-load-retry"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="keys-load-error"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('sk-rec...abcd')
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

    const routeLeave = onBeforeRouteLeave.mock.calls[0]?.[0] as ((to: { name: string }) => void) | undefined
    expect(routeLeave).toBeTypeOf('function')
    routeLeave?.({ name: 'EnterpriseEmployeeHome' })
    await flushPromises()

    expect((wrapper.vm as unknown as { secret: string }).secret).toBe('')
    expect(wrapper.text()).not.toContain(plaintext)
    expect(document.body.textContent).not.toContain(plaintext)
    expect(JSON.stringify(localStorage)).not.toContain(plaintext)
    expect(JSON.stringify(sessionStorage)).not.toContain(plaintext)
    wrapper.unmount()
  })

  it('shows a first-visit tour without creating a key on a Next click and keeps the guide entry after skipping', async () => {
    vi.spyOn(ElMessage, 'info').mockImplementation(() => undefined as never)
    const wrapper = mount(EnterpriseKeysView, { attachTo: document.body, global: { plugins: [ElementPlus] } })
    await flushPromises()
    expect(driverMock).toHaveBeenCalledTimes(1)
    const options = driverMock.mock.calls[0][0]
    expect(options.steps).toHaveLength(3)
    options.onNextClick()
    expect(createKey).not.toHaveBeenCalled()
    options.onCloseClick()
    expect(localStorage.getItem('enterprise-key-guide-seen')).toBe('true')
    expect(wrapper.find('[data-testid="open-guide"]').exists()).toBe(true)
    wrapper.unmount()

    driverMock.mockClear()
    const nextVisit = mount(EnterpriseKeysView, { attachTo: document.body, global: { plugins: [ElementPlus] } })
    await flushPromises()
    expect(driverMock).not.toHaveBeenCalled()
    nextVisit.unmount()
  })

  it('keeps the guide entry visible for empty and populated key states', async () => {
    const wrapper = mount(EnterpriseKeysView, { global: { plugins: [ElementPlus] } })
    await flushPromises()
    expect(wrapper.get('[data-testid="open-guide"]').text()).toContain('接入指南')
    expect(wrapper.get('[data-testid="empty-open-guide"]').text()).toContain('查看接入指南')
    await wrapper.get('[data-testid="empty-open-guide"]').trigger('click')
    expect(push).toHaveBeenCalledWith({ name: 'EnterpriseKeyGuide' })
    wrapper.unmount()
  })

  it('copies the created secret and opens the guide without persisting plaintext', async () => {
    const clipboard = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: clipboard } })
    const plaintext = 'sk-temporary-guide-key'
    createKey.mockResolvedValue({
      key: {
        id: 21, masked_key: 'sk-tem...uide', name: 'Enterprise employee key', status: 'active',
        quota: 10, quota_used: 0, created_at: new Date().toISOString(), updated_at: new Date().toISOString(),
      },
      plaintext,
      replayed: false,
    })
    vi.spyOn(ElMessage, 'success').mockImplementation(() => undefined as never)
    const wrapper = mount(EnterpriseKeysView, { global: { plugins: [ElementPlus] } })
    await flushPromises()
    await wrapper.get('[data-testid="create-key"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="copy-secret"]').trigger('click')
    await flushPromises()
    expect(clipboard).toHaveBeenCalledWith(plaintext)
    await wrapper.get('[data-testid="dialog-open-guide"]').trigger('click')
    expect(push).toHaveBeenCalledWith({ name: 'EnterpriseKeyGuide' })
    expect(JSON.stringify(localStorage)).not.toContain(plaintext)
    expect(JSON.stringify(sessionStorage)).not.toContain(plaintext)
    wrapper.unmount()
    expect((wrapper.vm as unknown as { secret: string }).secret).toBe('')
  })

  it('shows a masked owner key while keeping full copy and developer tool import available after reload', async () => {
    const fullKey = 'sk-test-FAKE-owner-389'
    getCurrentKey.mockResolvedValue({
      id: 31, key: fullKey, masked_key: 'sk-tes...r389', name: 'Enterprise employee key', status: 'active',
      quota: 30, quota_used: 0, rate_limit_5h: 0, rate_limit_1d: 0, rate_limit_7d: 0,
      usage_5h: 0, usage_1d: 0, usage_7d: 0, created_at: new Date().toISOString(), updated_at: new Date().toISOString(),
    })
    const copy = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: copy } })
    const open = vi.spyOn(window, 'open').mockImplementation(() => null)
    const wrapper = mount(EnterpriseKeysView, { global: { plugins: [ElementPlus] } })
    await flushPromises()
    expect(wrapper.get('[data-testid="current-key"]').text()).toContain('sk-tes...r389')
    expect(wrapper.text()).not.toContain(fullKey)
    await wrapper.get('[data-testid="copy-current-key"]').trigger('click')
    expect(copy).toHaveBeenCalledWith(fullKey)
    await wrapper.get('[data-testid="developer-tools"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="tool-models"]').exists()).toBe(true)
    await wrapper.get('[data-testid="codex-import"]').trigger('click')
    const openai = new URL(open.mock.lastCall?.[0] as string)
    expect(openai.searchParams.get('baseUrl')).toBe('https://tenant.example.test/v1')
    expect(openai.searchParams.get('wireApi')).toBe('responses')
    expect(openai.searchParams.get('apiKey')).toBe(fullKey)
    ;(wrapper.vm as unknown as { selectedToolModel: string }).selectedToolModel = 'claude-example'
    await wrapper.vm.$nextTick()
    await wrapper.get('[data-testid="codex-import"]').trigger('click')
    const anthropic = new URL(open.mock.lastCall?.[0] as string)
    expect(anthropic.searchParams.get('baseUrl')).toBe('https://tenant.example.test')
    expect(anthropic.searchParams.get('wireApi')).toBe('messages')
    await wrapper.get('[data-testid="ccs-tab"]').trigger('click')
    await wrapper.get('[data-testid="ccs-import"]').trigger('click')
    expect(new URL(open.mock.lastCall?.[0] as string).searchParams.get('endpoint')).toBe('https://tenant.example.test')
    expect(JSON.stringify(localStorage)).not.toContain(fullKey)
    wrapper.unmount()
    open.mockRestore()
  })

  it('disables both imports without a callable model or a complete owner key', async () => {
    getCurrentKey.mockResolvedValue({
      id: 40, masked_key: 'sk-tes...r389', name: 'Enterprise employee key', status: 'active',
      quota: 10, quota_used: 0, rate_limit_5h: 0, rate_limit_1d: 0, rate_limit_7d: 0,
      usage_5h: 0, usage_1d: 0, usage_7d: 0, created_at: new Date().toISOString(), updated_at: new Date().toISOString(),
    })
    const open = vi.spyOn(window, 'open').mockImplementation(() => null)
    const wrapper = mount(EnterpriseKeysView, { global: { plugins: [ElementPlus] } })
    await flushPromises()
    await wrapper.get('[data-testid="developer-tools"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="codex-import"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="codex-import"]').trigger('click')
    expect(open).not.toHaveBeenCalled()
    wrapper.unmount()
    open.mockRestore()
  })
})
