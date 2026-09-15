import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'

import EnterpriseCreateView from '../EnterpriseCreateView.vue'

const { create } = vi.hoisted(() => ({
  create: vi.fn(),
}))

vi.mock('@/api/enterprisePlatform', () => ({
  enterprisePlatformAPI: { create },
}))

const push = vi.fn()
vi.mock('vue-router', () => ({
  useRouter: () => ({ push }),
  RouterLink: { template: '<a><slot /></a>' },
}))

vi.mock('element-plus', async () => {
  const actual = await vi.importActual<typeof import('element-plus')>('element-plus')
  return { ...actual, ElMessage: { error: vi.fn(), success: vi.fn() } }
})

describe('EnterpriseCreateView 信息结构补齐', () => {
  it('创建成功后展示真实返回的企业名称、域名、主账号及查看详情入口', async () => {
    create.mockResolvedValueOnce({
      id: 7,
      name: '云川数据',
      portal_host: 'yunchuan.example.com',
      dedicated_upstream_user_id: 9,
      status: 'active',
      created_at: '2026-01-05T08:00:00Z',
      admin_email: 'admin@yunchuan.example.com',
      employee_count: 0,
      active_employee_count: 0,
      active_session_count: 0,
      active_key_count: 0,
      subscriptions: [],
    })
    const wrapper = mount(EnterpriseCreateView, { global: { plugins: [ElementPlus] } })
    wrapper.vm.form.name = '云川数据'
    wrapper.vm.form.portal_host = 'yunchuan.example.com'
    wrapper.vm.form.dedicated_upstream_user_id = 9
    wrapper.vm.form.reason = '业务扩展开通'
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(wrapper.find('[data-testid="enterprise-create-success"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('云川数据')
    expect(wrapper.text()).toContain('yunchuan.example.com')
    expect(wrapper.text()).toContain('admin@yunchuan.example.com')

    await wrapper.get('[data-testid="enterprise-create-success"] .el-button').trigger('click')
    expect(push).toHaveBeenCalledWith('/admin/enterprises/7')
  })
})
