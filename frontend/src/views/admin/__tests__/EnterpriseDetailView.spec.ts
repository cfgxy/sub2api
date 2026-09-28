import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
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

describe('EnterpriseDetailView 信息结构补齐', () => {
  beforeEach(async () => {
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
    const wrapper = mount(EnterpriseDetailView, {
      global: {
        plugins: [ElementPlus, i18n],
        stubs: { PlatformEnterpriseShell: { template: '<div><slot /></div>' } },
      },
    })
    await flushPromises()

    const tabLabels = wrapper.findAll('.el-tabs__item').map((el) => el.text())
    expect(tabLabels).toContain('概览')
    expect(tabLabels).toContain('访问与账号')
    expect(wrapper.text()).toContain('云川数据')
    expect(wrapper.text()).toContain('启用')
    expect(wrapper.text()).not.toContain('2026-01-05T08:00:00Z')

    await wrapper.findAll('.el-tabs__item').find((el) => el.text() === '访问与账号')?.trigger('click')
    await flushPromises()
    const accessPane = wrapper.findAll('.el-tab-pane').find((el) => el.text().includes('admin@yunchuan.example.com'))
    expect(accessPane?.attributes('style')).not.toContain('display: none')
    i18n.global.locale.value = 'en'
    enterpriseTestLocale.value = 'en'
    await flushPromises()
    expect(wrapper.text()).toContain('Enabled')
    expect(wrapper.text()).not.toContain('创建时间')
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
    const wrapper = mount(EnterpriseDetailView, {
      global: { plugins: [ElementPlus, i18n], stubs: { PlatformEnterpriseShell: { template: '<div><slot /></div>' } } },
    })
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
})
