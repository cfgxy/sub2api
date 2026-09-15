import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'

import EnterpriseDetailView from '../EnterpriseDetailView.vue'

const { get } = vi.hoisted(() => ({
  get: vi.fn(),
}))

vi.mock('@/api/enterprisePlatform', () => ({
  enterprisePlatformAPI: { get, disable: vi.fn() },
}))

vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { id: '7' } }),
  useRouter: () => ({ back: vi.fn(), push: vi.fn() }),
}))

describe('EnterpriseDetailView 信息结构补齐', () => {
  it('概览与访问与账号分两个 Tab 展示，管理员邮箱只在访问与账号 Tab 中出现', async () => {
    get.mockResolvedValueOnce({
      id: 7,
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
      subscriptions: [],
    })
    const wrapper = mount(EnterpriseDetailView, {
      global: {
        plugins: [ElementPlus],
        stubs: { PlatformEnterpriseShell: { template: '<div><slot /></div>' } },
      },
    })
    await flushPromises()

    const tabLabels = wrapper.findAll('.el-tabs__item').map((el) => el.text())
    expect(tabLabels).toContain('概览')
    expect(tabLabels).toContain('访问与账号')
    expect(wrapper.text()).toContain('云川数据')

    await wrapper.findAll('.el-tabs__item').find((el) => el.text() === '访问与账号')?.trigger('click')
    await flushPromises()
    const accessPane = wrapper.findAll('.el-tab-pane').find((el) => el.text().includes('admin@yunchuan.example.com'))
    expect(accessPane?.attributes('style')).not.toContain('display: none')
  })
})
