import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import EnterpriseKeyGuideView from './EnterpriseKeyGuideView.vue'

vi.mock('vue-i18n', async () => ({
  ...(await vi.importActual<typeof import('vue-i18n')>('vue-i18n')),
  useI18n: (await import('@/views/admin/__tests__/enterpriseTestI18n')).useEnterpriseTestI18n,
}))

const { getCurrentKey, fetchEnterpriseGuideModels, getPublicSettings, copyToClipboard } = vi.hoisted(() => ({
  getCurrentKey: vi.fn(),
  fetchEnterpriseGuideModels: vi.fn(),
  getPublicSettings: vi.fn(),
  copyToClipboard: vi.fn(),
}))

vi.mock('@/api/enterprise', () => ({ enterpriseAPI: { getCurrentKey }, fetchEnterpriseGuideModels }))
vi.mock('@/api/auth', () => ({ getPublicSettings }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard }) }))

const FULL_KEY = `sk-guide-${'a1b2c3d4'.repeat(4)}`
const employeeKey = (overrides: Record<string, unknown> = {}) => ({
  id: 3, masked_key: 'sk-gui...c3d4', key: FULL_KEY, name: 'Enterprise employee key', status: 'active',
  quota: 25, quota_used: 1, rate_limit_5h: 0, rate_limit_1d: 0, rate_limit_7d: 0,
  usage_5h: 0, usage_1d: 0, usage_7d: 0, created_at: '2026-09-01T00:00:00Z', updated_at: '2026-09-01T00:00:00Z',
  ...overrides,
})
const mountView = () => mount(EnterpriseKeyGuideView, {
  global: { stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } } },
})

describe('EnterpriseKeyGuideView', () => {
  let open: ReturnType<typeof vi.fn>
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    setActivePinia(createPinia())
    vi.resetAllMocks()
    open = vi.fn()
    vi.stubGlobal('open', open)
    getCurrentKey.mockResolvedValue(employeeKey())
    getPublicSettings.mockResolvedValue({ api_base_url: 'https://gw.example.com/v1' })
    fetchEnterpriseGuideModels.mockResolvedValue([
      { id: 'gpt-guide', platform: 'openai' },
      { id: 'claude-guide', platform: 'anthropic' },
    ])
    copyToClipboard.mockResolvedValue(true)
  })

  it('shows the loading state before the key resolves', () => {
    getCurrentKey.mockReturnValue(new Promise(() => undefined))
    const wrapper = mountView()
    expect(wrapper.find('[data-testid="guide-loading"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="guide-example"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows a retryable error state when the key cannot be loaded', async () => {
    getCurrentKey.mockRejectedValueOnce(new Error('boom'))
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.find('[data-testid="guide-load-error"]').exists()).toBe(true)
    getCurrentKey.mockResolvedValueOnce(employeeKey())
    await wrapper.get('[data-testid="guide-load-retry"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="guide-load-error"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="guide-base-url"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('shows the empty state and never asks for models when no key exists', async () => {
    getCurrentKey.mockResolvedValue(null)
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.find('[data-testid="guide-no-key"]').exists()).toBe(true)
    expect(fetchEnterpriseGuideModels).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('shows dynamic base urls: openai with /v1, anthropic without', async () => {
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.get('[data-testid="guide-base-url-openai"]').text()).toBe('https://gw.example.com/v1')
    expect(wrapper.get('[data-testid="guide-base-url-anthropic"]').text()).toBe('https://gw.example.com')
    await wrapper.get('[data-testid="guide-copy-base-url-openai"]').trigger('click')
    expect(copyToClipboard).toHaveBeenCalledWith('https://gw.example.com/v1', expect.any(String))
    wrapper.unmount()
  })

  it('falls back to the current origin only when no deployment address is configured', async () => {
    getPublicSettings.mockResolvedValue({ api_base_url: '' })
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.get('[data-testid="guide-base-url-openai"]').text()).toBe(`${window.location.origin}/v1`)
    wrapper.unmount()
  })

  it('reports an unusable deployment address instead of inventing one', async () => {
    getPublicSettings.mockResolvedValue({ api_base_url: 'ftp://gw.example.com' })
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.find('[data-testid="guide-base-url-error"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="guide-example-code"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('lists only the catalogue returned for this key and renders the example for the selected model', async () => {
    const wrapper = mountView()
    await flushPromises()
    const options = wrapper.findAll('[data-testid="guide-model-option"]')
    expect(options).toHaveLength(2)
    expect(wrapper.get('[data-testid="guide-example-code"]').text()).toContain('https://gw.example.com/v1/chat/completions')
    expect(wrapper.get('[data-testid="guide-example-code"]').text()).toContain('gpt-guide')

    await options[1].setValue(true)
    const example = wrapper.get('[data-testid="guide-example-code"]').text()
    expect(example).toContain('https://gw.example.com/v1/messages')
    expect(example).not.toContain('/v1/v1')
    expect(example).toContain('claude-guide')

    await wrapper.get('[data-testid="guide-example-tab-python"]').trigger('click')
    expect(wrapper.get('[data-testid="guide-example-code"]').text()).toContain('urlopen')
    await wrapper.get('[data-testid="guide-copy-example"]').trigger('click')
    expect(copyToClipboard).toHaveBeenCalledWith(expect.stringContaining('claude-guide'), expect.any(String))
    wrapper.unmount()
  })

  it('keeps the real key out of examples and clipboard examples', async () => {
    const wrapper = mountView()
    await flushPromises()
    for (const kind of ['curl', 'python', 'javascript']) {
      await wrapper.get(`[data-testid="guide-example-tab-${kind}"]`).trigger('click')
      expect(wrapper.get('[data-testid="guide-example-code"]').text()).toContain('{{API_KEY}}')
      expect(wrapper.get('[data-testid="guide-example-code"]').text()).not.toContain(FULL_KEY)
    }
    await wrapper.get('[data-testid="guide-copy-example"]').trigger('click')
    expect(JSON.stringify(copyToClipboard.mock.calls)).not.toContain(FULL_KEY)
    wrapper.unmount()
  })

  it('shows an empty state and no default model when the key has no usable model', async () => {
    fetchEnterpriseGuideModels.mockResolvedValue([])
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.find('[data-testid="guide-models-empty"]').exists()).toBe(true)
    expect(wrapper.findAll('[data-testid="guide-model-option"]')).toHaveLength(0)
    expect(wrapper.find('[data-testid="guide-example-code"]').exists()).toBe(false)
    expect(wrapper.text()).not.toMatch(/gpt-|claude-|gemini-/)
    expect((wrapper.get('[data-testid="guide-import-codex"]').element as HTMLButtonElement).disabled).toBe(true)
    expect((wrapper.get('[data-testid="guide-import-ccs"]').element as HTMLButtonElement).disabled).toBe(true)
    wrapper.unmount()
  })

  it('shows a retryable error for the model catalogue without falling back to defaults', async () => {
    fetchEnterpriseGuideModels.mockRejectedValueOnce(new Error('boom'))
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.find('[data-testid="guide-models-error"]').exists()).toBe(true)
    expect(wrapper.findAll('[data-testid="guide-model-option"]')).toHaveLength(0)
    fetchEnterpriseGuideModels.mockResolvedValueOnce([{ id: 'gpt-guide', platform: 'openai' }])
    await wrapper.get('[data-testid="guide-models-retry"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('[data-testid="guide-model-option"]')).toHaveLength(1)
    wrapper.unmount()
  })

  it('imports the selected model into Codex++ and CCSwitch without logging, storing or routing the key', async () => {
    const logs = ['log', 'info', 'warn', 'error', 'debug'].map((method) =>
      vi.spyOn(console, method as 'log').mockImplementation(() => undefined))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="guide-import-codex"]').trigger('click')
    const codexUri = String(open.mock.calls[0][0])
    expect(codexUri.startsWith('codexplusplus://v1/import/provider?')).toBe(true)
    expect(decodeURIComponent(codexUri)).toContain('gpt-guide')
    expect(open.mock.calls[0][1]).toBe('_self')

    await wrapper.findAll('[data-testid="guide-model-option"]')[1].setValue(true)
    expect((wrapper.get('[data-testid="guide-import-codex"]').element as HTMLButtonElement).disabled).toBe(true)
    await wrapper.get('[data-testid="guide-import-ccs"]').trigger('click')
    const ccsUri = String(open.mock.calls[1][0])
    expect(ccsUri.startsWith('ccswitch://v1/import?')).toBe(true)
    expect(new URL(ccsUri).searchParams.get('model')).toBe('claude-guide')
    expect(new URL(ccsUri).searchParams.get('endpoint')).toBe('https://gw.example.com')

    expect(JSON.stringify(logs.map((spy) => spy.mock.calls))).not.toContain(FULL_KEY)
    expect(JSON.stringify(localStorage)).not.toContain(FULL_KEY)
    expect(JSON.stringify(sessionStorage)).not.toContain(FULL_KEY)
    expect(window.location.href).not.toContain(FULL_KEY)
    expect(wrapper.html()).not.toContain(FULL_KEY)
    wrapper.unmount()
  })

  it('disables import and explains why when only a masked key is available', async () => {
    getCurrentKey.mockResolvedValue(employeeKey({ key: undefined }))
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.find('[data-testid="guide-copy-key"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="guide-key-unavailable"]').exists()).toBe(true)
    expect((wrapper.get('[data-testid="guide-import-codex"]').element as HTMLButtonElement).disabled).toBe(true)
    expect(wrapper.find('[data-testid="guide-import-hint"]').exists()).toBe(true)
    await wrapper.get('[data-testid="guide-import-ccs"]').trigger('click')
    expect(open).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('disables import for a key that is not active', async () => {
    getCurrentKey.mockResolvedValue(employeeKey({ status: 'disabled' }))
    const wrapper = mountView()
    await flushPromises()
    expect((wrapper.get('[data-testid="guide-import-codex"]').element as HTMLButtonElement).disabled).toBe(true)
    wrapper.unmount()
  })

  it('copies the full key only through the clipboard helper', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="guide-copy-key"]').trigger('click')
    expect(copyToClipboard).toHaveBeenCalledWith(FULL_KEY, expect.any(String))
    expect(wrapper.html()).not.toContain(FULL_KEY)
    wrapper.unmount()
  })
})
