import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessage } from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import EnterpriseKeyGuideView from './EnterpriseKeyGuideView.vue'

const { getPublicSettings, fetchEnterpriseGuideModels, push } = vi.hoisted(() => ({
  getPublicSettings: vi.fn(),
  fetchEnterpriseGuideModels: vi.fn(),
  push: vi.fn(),
}))

vi.mock('@/api/auth', () => ({ getPublicSettings }))
vi.mock('@/api/enterprise', () => ({
  fetchEnterpriseGuideModels,
}))
vi.mock('vue-router', () => ({ useRouter: () => ({ push }) }))

describe('EnterpriseKeyGuideView', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    getPublicSettings.mockResolvedValue({ api_base_url: 'https://api.first.example/v1' })
    fetchEnterpriseGuideModels.mockResolvedValue([
      { id: 'enterprise-model-a', display_name: 'Model A' },
      { id: 'enterprise-model-b', description: 'Model B description' },
    ])
    vi.spyOn(ElMessage, 'success').mockImplementation(() => undefined as never)
    vi.spyOn(ElMessage, 'warning').mockImplementation(() => undefined as never)
  })

  it('uses the configured Base URL and returned model catalogue in all examples', async () => {
    const wrapper = mount(EnterpriseKeyGuideView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    expect(wrapper.get('[data-testid="base-url-value"]').text()).toContain('https://api.first.example/v1')
    expect(wrapper.get('[data-testid="models-list"]').text()).toContain('enterprise-model-a')
    expect(wrapper.get('[data-testid="examples-section"]').text()).toContain('https://api.first.example/v1/chat/completions')
    expect(wrapper.get('[data-testid="examples-section"]').text()).toContain('enterprise-model-a')
    expect(fetchEnterpriseGuideModels).toHaveBeenCalledWith(expect.any(AbortSignal))
    wrapper.unmount()
  })

  it('adds the OpenAI API suffix when a deployment config provides only the root address', async () => {
    getPublicSettings.mockResolvedValueOnce({ api_base_url: 'https://new-deployment.example.test/tenant' })
    const wrapper = mount(EnterpriseKeyGuideView, { global: { plugins: [ElementPlus] } })
    await flushPromises()
    expect(wrapper.get('[data-testid="examples-section"]').text()).toContain('https://new-deployment.example.test/tenant/v1/chat/completions')
    wrapper.unmount()
  })

  it('refreshes an invalid configured URL without silently using a browser-origin fallback', async () => {
    getPublicSettings
      .mockResolvedValueOnce({ api_base_url: 'javascript:invalid' })
      .mockResolvedValueOnce({ api_base_url: 'https://api.second.example/v1' })
    const wrapper = mount(EnterpriseKeyGuideView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    expect(wrapper.find('[data-testid="base-url-error"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain(window.location.origin)

    await wrapper.get('[data-testid="retry-base-url"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="base-url-value"]').text()).toContain('https://api.second.example/v1')
    expect(fetchEnterpriseGuideModels).toHaveBeenCalledWith(expect.any(AbortSignal))
    wrapper.unmount()
  })

  it('loads models again on a later visit without access to the original API key', async () => {
    const first = mount(EnterpriseKeyGuideView, { global: { plugins: [ElementPlus] } })
    await flushPromises()
    first.unmount()
    const second = mount(EnterpriseKeyGuideView, { global: { plugins: [ElementPlus] } })
    await flushPromises()
    expect(second.get('[data-testid="models-list"]').text()).toContain('enterprise-model-a')
    expect(fetchEnterpriseGuideModels).toHaveBeenCalledTimes(2)
    second.unmount()
  })

  it('uses the current deployment origin when no API base URL is configured', async () => {
    getPublicSettings.mockResolvedValueOnce({ api_base_url: '' })
    const wrapper = mount(EnterpriseKeyGuideView, { global: { plugins: [ElementPlus] } })
    await flushPromises()
    expect(wrapper.get('[data-testid="base-url-value"]').text()).toContain(`${window.location.origin}/v1`)
    expect(wrapper.get('[data-testid="examples-section"]').text()).toContain(`${window.location.origin}/v1/chat/completions`)
    wrapper.unmount()
  })

  it('distinguishes an empty subscription catalogue from a failed gateway request', async () => {
    fetchEnterpriseGuideModels.mockResolvedValueOnce([])
    const wrapper = mount(EnterpriseKeyGuideView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    expect(wrapper.get('[data-testid="models-empty"]').text()).toContain('暂无可调用的示例模型')
    expect(wrapper.find('[data-testid="models-list"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="copy-example"]').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('reports a failed model request and recovers on retry', async () => {
    fetchEnterpriseGuideModels.mockRejectedValueOnce({ status: 503 })
    const wrapper = mount(EnterpriseKeyGuideView, { global: { plugins: [ElementPlus] } })
    await flushPromises()
    expect(wrapper.get('[data-testid="models-error"]').text()).toContain('暂时无法加载')

    await wrapper.get('[data-testid="retry-models"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="models-list"]').text()).toContain('enterprise-model-a')
    wrapper.unmount()
  })

  it('changes the selected model and copies examples with placeholders intact', async () => {
    const clipboard = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: clipboard } })
    const wrapper = mount(EnterpriseKeyGuideView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    await wrapper.get('[data-testid="models-list"] .model-item:nth-child(2)').trigger('click')
    expect(wrapper.get('[data-testid="examples-section"]').text()).toContain('enterprise-model-b')
    await wrapper.get('[data-testid="copy-example"]').trigger('click')
    await flushPromises()
    expect(clipboard).toHaveBeenCalledWith(expect.stringContaining('{{API_KEY}}'))
    expect(wrapper.get('.code-panel pre').text()).toContain('enterprise-model-b')

    const tabs = wrapper.findAll('[role="tab"]')
    await tabs[1].trigger('click')
    expect(wrapper.get('.code-panel pre').text()).toContain('from urllib.request import Request, urlopen')
    expect(wrapper.get('.code-panel pre').text()).toContain('https://api.first.example/v1/chat/completions')
    await tabs[2].trigger('click')
    expect(wrapper.get('.code-panel pre').text()).toContain('await fetch(')
    expect(wrapper.get('.code-panel pre').text()).toContain('enterprise-model-b')
    expect(wrapper.get('.code-panel pre').text()).not.toContain('enterprise_access_token')
    wrapper.unmount()
  })
})
