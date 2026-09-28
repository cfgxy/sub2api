import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { i18n, loadLocaleMessages } from '@/i18n'
import { enterpriseTestLocale } from './enterpriseTestI18n'

import EnterprisesView from '../EnterprisesView.vue'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: (await import('./enterpriseTestI18n')).useEnterpriseTestI18n,
}))

const { list } = vi.hoisted(() => ({
  list: vi.fn()
}))

vi.mock('@/api/enterprisePlatform', () => ({
  enterprisePlatformAPI: {
    list,
    get: vi.fn(),
    create: vi.fn(),
    disable: vi.fn()
  }
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() })
}))

vi.mock('element-plus', async () => {
  const actual = await vi.importActual<typeof import('element-plus')>('element-plus')
  return {
    ...actual,
    ElMessage: { error: vi.fn(), success: vi.fn() }
  }
})

describe('EnterprisesView 来源失败态', () => {
  it('列表加载失败时展示持久的来源不可用提示，且不渲染「暂无企业租户」空态', async () => {
    await loadLocaleMessages('zh')
    i18n.global.locale.value = 'zh'
    enterpriseTestLocale.value = 'zh'
    list.mockRejectedValueOnce(new Error('network down'))
    const wrapper = mount(EnterprisesView, {
      global: {
        plugins: [ElementPlus, i18n],
        stubs: { PlatformEnterpriseShell: { template: '<div><slot /></div>' } }
      }
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="enterprises-source-unavailable"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('暂无企业租户')
  })

  it('列表加载成功且为空时展示「暂无企业租户」空态，不展示来源不可用提示', async () => {
    await loadLocaleMessages('zh')
    i18n.global.locale.value = 'zh'
    enterpriseTestLocale.value = 'zh'
    list.mockResolvedValueOnce([])
    const wrapper = mount(EnterprisesView, {
      global: {
        plugins: [ElementPlus, i18n],
        stubs: { PlatformEnterpriseShell: { template: '<div><slot /></div>' } }
      }
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="enterprises-source-unavailable"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('暂无企业租户')
  })
})
