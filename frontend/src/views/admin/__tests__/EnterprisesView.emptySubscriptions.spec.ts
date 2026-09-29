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
  list: vi.fn(),
}))

vi.mock('@/api/enterprisePlatform', () => ({
  enterprisePlatformAPI: {
    list,
    get: vi.fn(),
    create: vi.fn(),
    disable: vi.fn(),
    enable: vi.fn(),
    updateHost: vi.fn(),
  },
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() }),
}))

describe('EnterprisesView 订阅摘要空态', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('后端返回 subscriptions 为 null 时渲染「无订阅」且不抛错', async () => {
    await loadLocaleMessages('zh')
    i18n.global.locale.value = 'zh'
    enterpriseTestLocale.value = 'zh'
    list.mockResolvedValueOnce([
      {
        id: 2,
        name: '孤岭设计',
        portal_host: 'none.example.com',
        dedicated_upstream_user_id: 10,
        status: 'active',
        created_at: '2026-01-06T08:00:00Z',
        admin_email: 'admin@none.example.com',
        subscriptions: null,
      },
    ])
    const wrapper = mount(EnterprisesView, {
      global: {
        plugins: [i18n],
        stubs: { teleport: true, PlatformEnterpriseShell: { template: '<div><slot /></div>' } },
      },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('孤岭设计')
    expect(wrapper.find('[data-testid="enterprise-no-subscriptions"]').text()).toBe('无订阅')
  })
})
