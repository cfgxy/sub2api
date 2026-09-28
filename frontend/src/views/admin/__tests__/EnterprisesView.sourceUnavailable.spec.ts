import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
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
    disable: vi.fn(),
    enable: vi.fn(),
    updateHost: vi.fn()
  }
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() })
}))

function mountView() {
  return mount(EnterprisesView, {
    global: {
      plugins: [i18n],
      stubs: { teleport: true, PlatformEnterpriseShell: { template: '<div><slot /></div>' } }
    }
  })
}

describe('EnterprisesView 来源失败态', () => {
  beforeEach(async () => {
    setActivePinia(createPinia())
    await loadLocaleMessages('zh')
    i18n.global.locale.value = 'zh'
    enterpriseTestLocale.value = 'zh'
  })

  it('列表加载失败时展示持久的来源不可用提示，且不渲染「暂无企业租户」空态', async () => {
    list.mockRejectedValueOnce(new Error('network down'))
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-testid="enterprises-source-unavailable"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('暂无企业租户')
  })

  it('列表加载成功且为空时展示「暂无企业租户」空态，不展示来源不可用提示', async () => {
    list.mockResolvedValueOnce([])
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-testid="enterprises-source-unavailable"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('暂无企业租户')
  })
})
