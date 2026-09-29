import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { i18n, loadLocaleMessages } from '@/i18n'
import { enterpriseTestLocale } from './enterpriseTestI18n'

import EnterpriseDetailView from '../EnterpriseDetailView.vue'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: (await import('./enterpriseTestI18n')).useEnterpriseTestI18n,
}))

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

// ConfirmDialog 通过 Teleport 渲染到 body，就地展开后断言才能落在 wrapper 内
function mountView() {
  return mount(EnterpriseDetailView, {
    global: {
      plugins: [i18n],
      stubs: { teleport: true, PlatformEnterpriseShell: { template: '<div><slot /></div>' } },
    },
  })
}

describe('EnterpriseDetailView 信息结构补齐', () => {
  beforeEach(async () => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    await loadLocaleMessages('zh')
    await loadLocaleMessages('en')
    i18n.global.locale.value = 'zh'
    enterpriseTestLocale.value = 'zh'
  })

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
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="enterprise-detail-tab-overview"]').text()).toBe('概览')
    expect(wrapper.get('[data-testid="enterprise-detail-tab-access"]').text()).toBe('访问与账号')
    expect(wrapper.text()).toContain('云川数据')
    expect(wrapper.text()).toContain('启用')
    expect(wrapper.text()).not.toContain('2026-01-05T08:00:00Z')

    expect(wrapper.find('[data-testid="enterprise-detail-access"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('admin@yunchuan.example.com')

    await wrapper.get('[data-testid="enterprise-detail-tab-access"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="enterprise-detail-access"]').text()).toContain('admin@yunchuan.example.com')
    expect(wrapper.find('[data-testid="enterprise-detail-overview"]').exists()).toBe(false)

    i18n.global.locale.value = 'en'
    enterpriseTestLocale.value = 'en'
    await flushPromises()
    expect(wrapper.text()).toContain('Active Keys')
    expect(wrapper.text()).not.toContain('活动 Key')
  })

  it('订阅状态与有效期均显示为本地化文案，未知值不暴露原枚举', async () => {
    get.mockResolvedValueOnce({
      id: 7, name: '云川数据', portal_host: 'yunchuan.example.com', status: 'disabled',
      created_at: '2026-01-05T08:00:00Z', admin_email: '', dedicated_upstream_user_id: 9,
      employee_count: 3, active_employee_count: 2, active_session_count: 0, active_key_count: 0,
      subscriptions: [
        { id: 1, plan: 'Business', status: 'suspended', weekly_limit: '100', expires_at: '2026-02-01T08:00:00Z' },
        { id: 2, plan: 'Starter', status: 'unexpected_api_status', weekly_limit: '', expires_at: '2026-03-01T08:00:00Z' },
      ],
    })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('Business（已暂停，周上限 100')
    expect(wrapper.text()).toContain('Starter（未知状态，周上限 未配置')
    expect(wrapper.text()).not.toContain('unexpected_api_status')
    expect(wrapper.text()).not.toContain('2026-02-01T08:00:00Z')
    i18n.global.locale.value = 'en'
    enterpriseTestLocale.value = 'en'
    await flushPromises()
    expect(wrapper.text()).toContain('Business (Suspended, weekly limit 100')
    expect(wrapper.text()).toContain('Starter (Unknown status, weekly limit Not configured')
  })

  it('详情来源不可用时展示失败卡片、隐藏停用入口，重试后恢复', async () => {
    get.mockRejectedValueOnce(new Error('service unavailable'))
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-testid="enterprise-detail-unavailable"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="enterprise-detail-disable"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="enterprise-detail-overview"]').exists()).toBe(false)

    get.mockResolvedValueOnce({
      id: 7, name: '云川数据', portal_host: 'yunchuan.example.com', status: 'active',
      created_at: '2026-01-05T08:00:00Z', admin_email: '', dedicated_upstream_user_id: 9,
      employee_count: 0, active_employee_count: 0, active_session_count: 0, active_key_count: 0,
      subscriptions: [],
    })
    await wrapper.get('[data-testid="enterprise-detail-retry"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="enterprise-detail-unavailable"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="enterprise-detail-overview"]').text()).toContain('云川数据')
  })
})
