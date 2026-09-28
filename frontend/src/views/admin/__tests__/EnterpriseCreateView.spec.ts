import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { i18n, loadLocaleMessages } from '@/i18n'
import { enterpriseTestLocale } from './enterpriseTestI18n'

import EnterpriseCreateView from '../EnterpriseCreateView.vue'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: (await import('./enterpriseTestI18n')).useEnterpriseTestI18n,
}))

const { create, listUsers, listEnterprises } = vi.hoisted(() => ({
  create: vi.fn(),
  listUsers: vi.fn(),
  listEnterprises: vi.fn(),
}))

vi.mock('@/api/enterprisePlatform', () => ({
  enterprisePlatformAPI: { create, list: listEnterprises },
}))
vi.mock('@/api/admin/users', () => ({ list: listUsers }))

const push = vi.fn()
vi.mock('vue-router', () => ({
  useRouter: () => ({ push }),
  RouterLink: { template: '<a><slot /></a>' },
}))

vi.mock('element-plus', async () => {
  const actual = await vi.importActual<typeof import('element-plus')>('element-plus')
  return { ...actual, ElMessage: { error: vi.fn(), success: vi.fn() } }
})

const eligible = {
  id: 9, email: 'member@example.test', username: '测试成员', status: 'active',
  subscriptions: [{ status: 'active', starts_at: '2025-01-01T00:00:00Z', expires_at: '2099-01-01T00:00:00Z', group: { status: 'active', weekly_limit_usd: 10 } }],
}

function fillForm(form: { name: string; portal_host: string; dedicated_upstream_user_id: number | ''; reason: string }) {
  form.name = '测试企业'
  form.portal_host = 'company.example.test'
  form.dedicated_upstream_user_id = 9
  form.reason = '测试开通'
}

describe('EnterpriseCreateView 搜索选人', () => {
  beforeEach(async () => {
    vi.clearAllMocks()
    create.mockReset()
    vi.stubGlobal('scrollTo', vi.fn())
    await loadLocaleMessages('zh')
    await loadLocaleMessages('en')
    i18n.global.locale.value = 'zh'
    enterpriseTestLocale.value = 'zh'
    listEnterprises.mockResolvedValue([])
    listUsers.mockResolvedValue({ items: [eligible], total: 1, page: 1, page_size: 20, pages: 1 })
  })

  it('初始空态搜索、选人、清空和重新选择均只提交真实用户 ID', async () => {
    const wrapper = mount(EnterpriseCreateView, { global: { plugins: [ElementPlus, i18n] } })
    await flushPromises()
    const select = wrapper.findComponent({ name: 'ElSelect' })
    expect(select.props('modelValue')).toBe('')
    expect(wrapper.find('.el-select__placeholder').text()).toBe('按邮箱或昵称搜索')
    expect(wrapper.vm.selectedUser).toBeUndefined()

    await wrapper.vm.searchUsers('member')
    await flushPromises()
    expect(wrapper.vm.userOptions.map((option: { user: { id: number } }) => option.user.id)).toEqual([9])
    select.vm.$emit('update:modelValue', 9)
    select.vm.$emit('change', 9)
    await flushPromises()
    expect(wrapper.vm.selectedUser?.id).toBe(9)

    select.vm.$emit('update:modelValue', '')
    select.vm.$emit('change', '')
    await flushPromises()
    expect(wrapper.vm.form.dedicated_upstream_user_id).toBe('')
    expect(wrapper.vm.selectedUser).toBeUndefined()
    fillForm(wrapper.vm.form)
    await wrapper.vm.submit()
    expect(create).not.toHaveBeenCalled()

    await wrapper.vm.searchUsers('member')
    select.vm.$emit('update:modelValue', 9)
    select.vm.$emit('change', 9)
    await flushPromises()
    create.mockResolvedValueOnce({ id: 7, dedicated_upstream_user_id: 9 })
    await wrapper.vm.submit()
    expect(create).toHaveBeenCalledWith(expect.objectContaining({ dedicated_upstream_user_id: 9 }))
  })

  it('无效的 0 不成为候选或提交值', async () => {
    listUsers.mockResolvedValueOnce({ items: [{ ...eligible, id: 0 }, eligible], total: 2, page: 1, page_size: 20, pages: 1 })
    const wrapper = mount(EnterpriseCreateView, { global: { plugins: [ElementPlus, i18n] } })
    await flushPromises()
    await wrapper.vm.searchUsers('member')
    expect(wrapper.vm.userOptions.map((option: { user: { id: number } }) => option.user.id)).toEqual([9])
    fillForm(wrapper.vm.form)
    wrapper.vm.form.dedicated_upstream_user_id = 0
    wrapper.vm.selectUser(0)
    await wrapper.vm.submit()
    expect(create).not.toHaveBeenCalled()
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
    const wrapper = mount(EnterpriseCreateView, { global: { plugins: [ElementPlus, i18n] } })
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

    await wrapper.get('[data-testid="enterprise-create-success"] .el-button').trigger('click')
    expect(push).toHaveBeenCalledWith('/admin/enterprises/7')
    expect(create).toHaveBeenCalledWith(expect.objectContaining({ dedicated_upstream_user_id: 9 }))
    i18n.global.locale.value = 'en'
    enterpriseTestLocale.value = 'en'
    await flushPromises()
    expect(wrapper.text()).toContain('Create enterprise')
    expect(wrapper.text()).not.toContain('企业基础信息')
  })

  it.each([
    ['停用账号', { ...eligible, status: 'disabled' }, '账号已停用'],
    ['无可用订阅', { ...eligible, subscriptions: [] }, '没有当前可用的周订阅'],
  ])('%s不可提交并展示原因', async (_label, candidate, reason) => {
    listUsers.mockResolvedValueOnce({ items: [candidate], total: 1, page: 1, page_size: 20, pages: 1 })
    const wrapper = mount(EnterpriseCreateView, { global: { plugins: [ElementPlus, i18n] } })
    await flushPromises()
    await wrapper.vm.searchUsers('member')
    await flushPromises()
    expect(wrapper.vm.userOptions[0].reason).toBe(reason)
    fillForm(wrapper.vm.form)
    wrapper.vm.selectUser(9)
    await wrapper.vm.submit()
    expect(create).not.toHaveBeenCalled()
  })

  it('已有企业主账号不可复用', async () => {
    listEnterprises.mockResolvedValueOnce([{ dedicated_upstream_user_id: 9 }])
    const wrapper = mount(EnterpriseCreateView, { global: { plugins: [ElementPlus, i18n] } })
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
    const wrapper = mount(EnterpriseCreateView, { global: { plugins: [ElementPlus, i18n] } })
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
    const wrapper = mount(EnterpriseCreateView, { global: { plugins: [ElementPlus, i18n] } })
    await flushPromises()
    await wrapper.vm.searchUsers('member')
    await flushPromises()
    expect(wrapper.text()).toContain('用户搜索失败')
    expect(wrapper.vm.userOptions).toEqual([])
  })
})
