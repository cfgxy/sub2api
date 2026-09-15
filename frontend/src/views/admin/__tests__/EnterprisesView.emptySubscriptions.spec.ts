import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'

import EnterprisesView from '../EnterprisesView.vue'

const { list } = vi.hoisted(() => ({
  list: vi.fn(),
}))

vi.mock('@/api/enterprisePlatform', () => ({
  enterprisePlatformAPI: {
    list,
    get: vi.fn(),
    create: vi.fn(),
    disable: vi.fn(),
  },
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() }),
}))

describe('EnterprisesView 订阅摘要空态', () => {
  it('后端返回 subscriptions 为 null 时渲染「无订阅」且不抛错', async () => {
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
        plugins: [ElementPlus],
        stubs: { PlatformEnterpriseShell: { template: '<div><slot /></div>' } },
      },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('孤岭设计')
    expect(wrapper.find('.muted').text()).toBe('无订阅')
  })
})
