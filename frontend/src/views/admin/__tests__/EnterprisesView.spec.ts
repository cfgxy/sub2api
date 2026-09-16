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

describe('EnterprisesView 信息结构补齐', () => {
  it('展示创建时间、订阅摘要列，以及客户端统计的企业数量', async () => {
    list.mockResolvedValueOnce([
      {
        id: 1,
        name: '云川数据',
        portal_host: 'yunchuan.example.com',
        dedicated_upstream_user_id: 9,
        status: 'active',
        created_at: '2026-01-05T08:00:00Z',
        admin_email: 'admin@yunchuan.example.com',
        employee_count: 3,
        active_employee_count: 2,
        active_session_count: 1,
        active_key_count: 1,
        subscriptions: [{ id: 1, plan: 'Business', status: 'active', weekly_limit: '100', expires_at: '2026-02-01' }],
      },
    ])
    const wrapper = mount(EnterprisesView, {
      global: {
        plugins: [ElementPlus],
        stubs: { PlatformEnterpriseShell: { template: '<div><slot /></div>' } },
      },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('2026-01-05T08:00:00Z')
    expect(wrapper.text()).toContain('Business')
    expect(wrapper.find('[data-testid="enterprises-count"]').text()).toContain('共 1 家企业')
  })
})
