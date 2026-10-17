import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ModelPricingView from '../ModelPricingView.vue'

const getModelPricing = vi.hoisted(() => vi.fn())
vi.mock('@/api/modelPricing', () => ({ getModelPricing }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/components/layout/AppLayout.vue', () => ({
  default: { template: '<main><slot /></main>' }
}))

const mountView = () => mount(ModelPricingView, {
  global: { stubs: { AppLayout: { template: '<main><slot /></main>' } } }
})

describe('登录后模型参考价格', () => {
  beforeEach(() => {
    getModelPricing.mockReset()
  })

  it('只在正文接口成功后展示内容并清理不安全标记', async () => {
    getModelPricing.mockResolvedValue('<h1>参考价</h1><script>invalid()</script><p>¥0.70</p>')
    const wrapper = mountView()
    expect(wrapper.text()).not.toContain('¥0.70')
    await flushPromises()
    expect(wrapper.text()).toContain('¥0.70')
    expect(wrapper.find('script').exists()).toBe(false)
    wrapper.unmount()
  })

  it('认证或网络失败时不展示正文及内部错误', async () => {
    getModelPricing.mockRejectedValue(new Error('内部错误详情'))
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('modelPricing.loadFailed')
    expect(wrapper.text()).not.toContain('内部错误详情')
    expect(wrapper.find('.apinoria-pricing').exists()).toBe(false)
    wrapper.unmount()
  })

  it('卸载时取消正文请求', () => {
    getModelPricing.mockReturnValue(new Promise(() => {}))
    const wrapper = mountView()
    const signal = getModelPricing.mock.calls[0][0] as AbortSignal
    expect(signal.aborted).toBe(false)
    wrapper.unmount()
    expect(signal.aborted).toBe(true)
  })
})
