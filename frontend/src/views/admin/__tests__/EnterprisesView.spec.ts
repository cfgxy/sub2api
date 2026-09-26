import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'

import EnterprisesView from '../EnterprisesView.vue'

const { list, get, enable, disable, updateHost } = vi.hoisted(() => ({
  list: vi.fn(),
  get: vi.fn(),
  enable: vi.fn(),
  disable: vi.fn(),
  updateHost: vi.fn(),
}))

vi.mock('@/api/enterprisePlatform', () => ({
  enterprisePlatformAPI: {
    list,
    get,
    create: vi.fn(),
    disable,
    enable,
    updateHost,
  },
}))

const { confirmBox, promptBox } = vi.hoisted(() => ({
  confirmBox: vi.fn().mockResolvedValue('confirm'),
  promptBox: vi.fn().mockResolvedValue({ value: 'renamed.example.com' }),
}))

vi.mock('element-plus', async (importOriginal) => {
  const actual = await importOriginal<typeof import('element-plus')>()
  return {
    default: actual.default,
    ElMessage: actual.ElMessage,
    ElMessageBox: { confirm: confirmBox, prompt: promptBox },
  }
})

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() }),
}))

function mountView() {
  return mount(EnterprisesView, {
    global: {
      plugins: [ElementPlus],
      stubs: { PlatformEnterpriseShell: { template: '<div><slot /></div>' } },
    },
  })
}

function enterpriseFixture(overrides: Record<string, unknown> = {}) {
  return {
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
    ...overrides,
  }
}

describe('EnterprisesView 信息结构补齐', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('展示创建时间、订阅摘要列，以及客户端统计的企业数量', async () => {
    list.mockResolvedValueOnce([enterpriseFixture()])
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('2026-01-05T08:00:00Z')
    expect(wrapper.text()).toContain('Business')
    expect(wrapper.find('[data-testid="enterprises-count"]').text()).toContain('共 1 家企业')
  })

  it('活动企业展示停用入口、不展示启用入口', async () => {
    list.mockResolvedValueOnce([enterpriseFixture()])
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-testid="enterprise-enable"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('停用')
  })

  it('停用企业展示启用入口，确认后调用启用接口并刷新列表', async () => {
    list.mockResolvedValue([enterpriseFixture({ status: 'disabled' })])
    enable.mockResolvedValueOnce({ success: true })
    const wrapper = mountView()
    await flushPromises()

    const enableButton = wrapper.find('[data-testid="enterprise-enable"]')
    expect(enableButton.exists()).toBe(true)
    await enableButton.trigger('click')
    await flushPromises()

    expect(confirmBox).toHaveBeenCalledWith(expect.stringContaining('启用 云川数据'), '启用企业', expect.anything())
    expect(enable).toHaveBeenCalledWith(1, '平台运营确认启用')
    expect(list).toHaveBeenCalledTimes(2)
  })

  it('改域名入口经确认弹窗输入新域名后调用修改接口', async () => {
    list.mockResolvedValueOnce([enterpriseFixture()])
    updateHost.mockResolvedValueOnce({ success: true })
    const wrapper = mountView()
    await flushPromises()

    const hostButton = wrapper.find('[data-testid="enterprise-update-host"]')
    expect(hostButton.exists()).toBe(true)
    await hostButton.trigger('click')
    await flushPromises()

    expect(promptBox).toHaveBeenCalledWith(expect.stringContaining('yunchuan.example.com'), '修改入口域名', expect.anything())
    expect(updateHost).toHaveBeenCalledWith(1, 'renamed.example.com', '平台运营修改入口域名')
  })
})
