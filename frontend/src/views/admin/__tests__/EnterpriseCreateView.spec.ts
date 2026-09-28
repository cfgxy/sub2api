import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { i18n, loadLocaleMessages } from '@/i18n'
import { enterpriseTestLocale } from './enterpriseTestI18n'

import EnterpriseCreateView from '../EnterpriseCreateView.vue'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: (await import('./enterpriseTestI18n')).useEnterpriseTestI18n,
}))

const { create, listUsers, listEnterprises, showError, showSuccess } = vi.hoisted(() => ({
  create: vi.fn(),
  listUsers: vi.fn(),
  listEnterprises: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api/enterprisePlatform', () => ({
  enterprisePlatformAPI: { create, list: listEnterprises },
}))
vi.mock('@/api/admin/users', () => ({ list: listUsers }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess, showWarning: vi.fn() }) }))

const push = vi.fn()
vi.mock('vue-router', () => ({
  useRouter: () => ({ push }),
  RouterLink: { template: '<a><slot /></a>' },
}))

const eligible = {
  id: 9, email: 'member@example.test', username: '测试成员', status: 'active',
  subscriptions: [{ status: 'active', starts_at: '2025-01-01T00:00:00Z', expires_at: '2099-01-01T00:00:00Z', group: { status: 'active', weekly_limit_usd: 10 } }],
}

function fillForm(form: { name: string; portal_host: string; dedicated_upstream_user_id: number; reason: string }) {
  form.name = '测试企业'
  form.portal_host = 'company.example.test'
  form.dedicated_upstream_user_id = 9
  form.reason = '测试开通'
}

// Select 的下拉面板走 Teleport 渲染到 body，就地展开后断言才能落在 wrapper 内
function mountView() {
  return mount(EnterpriseCreateView, {
    global: {
      plugins: [i18n],
      stubs: { teleport: true, PlatformEnterpriseShell: { template: '<div><slot /></div>' } },
    },
  })
}

describe('EnterpriseCreateView 搜索选人', () => {
  beforeEach(async () => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    vi.stubGlobal('scrollTo', vi.fn())
    await loadLocaleMessages('zh')
    await loadLocaleMessages('en')
    i18n.global.locale.value = 'zh'
    enterpriseTestLocale.value = 'zh'
    listEnterprises.mockResolvedValue([])
    listUsers.mockResolvedValue({ items: [eligible], total: 1, page: 1, page_size: 20, pages: 1 })
  })

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
    const wrapper = mountView()
    await flushPromises()
    await wrapper.vm.searchUsers('测试成员')
    await flushPromises()
    expect(listUsers).toHaveBeenCalledWith(1, 20, { search: '测试成员', include_subscriptions: true })
    expect(wrapper.vm.userOptions[0].user.email).toBe('member@example.test')
    fillForm(wrapper.vm.form)
    wrapper.vm.form.name = '云川数据'
    wrapper.vm.form.portal_host = 'yunchuan.example.com'
    wrapper.vm.selectUser(9)
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(wrapper.find('[data-testid="enterprise-create-success"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('云川数据')
    expect(wrapper.text()).toContain('yunchuan.example.com')
    expect(wrapper.text()).toContain('admin@yunchuan.example.com')

    await wrapper.get('[data-testid="enterprise-create-view-detail"]').trigger('click')
    expect(push).toHaveBeenCalledWith('/admin/enterprises/7')
    expect(create).toHaveBeenCalledWith(expect.objectContaining({ dedicated_upstream_user_id: 9 }))
    i18n.global.locale.value = 'en'
    enterpriseTestLocale.value = 'en'
    await flushPromises()
    expect(wrapper.text()).toContain('Create enterprise')
    expect(wrapper.text()).not.toContain('企业基础信息')
  })

  it('搜索到的候选账号通过下拉选项呈现，选中后写入表单', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.vm.searchUsers('测试成员')
    await flushPromises()

    expect(wrapper.vm.accountOptions).toEqual([
      { value: 9, label: '测试成员 · member@example.test', disabled: false },
    ])
    wrapper.vm.selectUser(9)
    expect(wrapper.vm.form.dedicated_upstream_user_id).toBe(9)
  })

  it.each([
    ['停用账号', { ...eligible, status: 'disabled' }, '账号已停用'],
    ['无可用订阅', { ...eligible, subscriptions: [] }, '没有当前可用的周订阅'],
  ])('%s不可提交并展示原因', async (_label, candidate, reason) => {
    listUsers.mockResolvedValueOnce({ items: [candidate], total: 1, page: 1, page_size: 20, pages: 1 })
    const wrapper = mountView()
    await flushPromises()
    await wrapper.vm.searchUsers('member')
    await flushPromises()
    expect(wrapper.vm.userOptions[0].reason).toBe(reason)
    expect(wrapper.vm.accountOptions[0].disabled).toBe(true)
    fillForm(wrapper.vm.form)
    wrapper.vm.selectUser(9)
    await wrapper.vm.submit()
    expect(create).not.toHaveBeenCalled()
  })

  it('已有企业主账号不可复用', async () => {
    listEnterprises.mockResolvedValueOnce([{ dedicated_upstream_user_id: 9 }])
    const wrapper = mountView()
    await flushPromises()
    await wrapper.vm.searchUsers('member')
    await flushPromises()
    expect(wrapper.vm.userOptions[0].reason).toBe('已关联其他企业')
    fillForm(wrapper.vm.form)
    wrapper.vm.selectUser(9)
    await wrapper.vm.submit()
    expect(create).not.toHaveBeenCalled()
  })

  it('企业列表加载失败时禁用搜索并拒绝提交', async () => {
    listEnterprises.mockRejectedValueOnce(new Error('service unavailable'))
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('企业列表加载失败')
    await wrapper.vm.searchUsers('member')
    expect(listUsers).not.toHaveBeenCalled()
    fillForm(wrapper.vm.form)
    await wrapper.vm.submit()
    expect(create).not.toHaveBeenCalled()
  })

  it('用户搜索失败时展示提示且无候选用户', async () => {
    listUsers.mockRejectedValueOnce(new Error('service unavailable'))
    const wrapper = mountView()
    await flushPromises()
    await wrapper.vm.searchUsers('member')
    await flushPromises()
    expect(wrapper.text()).toContain('用户搜索失败')
    expect(wrapper.vm.userOptions).toEqual([])
  })

  it('创建失败时只提示固定文案，不回显后端原始错误或内部标识', async () => {
    const backendTrace = 'INTERNAL-TRACE-ID-51cd · pg: duplicate key value violates unique constraint "enterprises_portal_host_key"'
    create.mockRejectedValueOnce(new Error(backendTrace))
    const wrapper = mountView()
    await flushPromises()
    await wrapper.vm.searchUsers('测试成员')
    await flushPromises()
    fillForm(wrapper.vm.form)
    wrapper.vm.selectUser(9)
    await wrapper.vm.submit()
    await flushPromises()

    expect(create).toHaveBeenCalled()
    // 正向对照：同一断言方式确实能检出已渲染文本，证明下面的负向断言不是空断言
    expect(wrapper.text()).toContain('创建企业')
    expect(wrapper.text()).not.toContain(backendTrace)
    expect(wrapper.text()).not.toContain('INTERNAL-TRACE-ID-51cd')
    expect(showError).toHaveBeenCalledWith('企业开通失败，请检查主账号和订阅状态')
    expect(showError.mock.calls.flat().join(' ')).not.toContain('INTERNAL-TRACE-ID-51cd')
  })
})
