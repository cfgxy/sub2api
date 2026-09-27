import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import EnterpriseLoginView from './EnterpriseLoginView.vue'
import viewSource from './EnterpriseLoginView.vue?raw'
import layoutSource from '@/components/layout/AuthLayout.vue?raw'

const { login, replace, routeQuery, getBrand } = vi.hoisted(() => ({
  login: vi.fn(),
  replace: vi.fn(),
  routeQuery: { redirect: undefined as string | undefined },
  getBrand: vi.fn(),
}))

vi.mock('@/stores/enterpriseAuth', () => ({
  useEnterpriseAuthStore: () => ({ login }),
}))
vi.mock('@/stores', () => ({
  useAppStore: () => ({
    siteName: 'Sub2API', siteLogo: '', cachedPublicSettings: null,
    publicSettingsLoaded: true, fetchPublicSettings: vi.fn(),
  }),
}))
vi.mock('@/api/enterprise', () => ({ enterpriseAPI: { getBrand } }))
vi.mock('vue-router', async () => {
  const actual = await vi.importActual<typeof import('vue-router')>('vue-router')
  return { ...actual, useRoute: () => ({ query: routeQuery }), useRouter: () => ({ replace }) }
})

const mountView = () => mount(EnterpriseLoginView, {
  global: {
    stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } },
  },
})

async function fillAndSubmit(wrapper: ReturnType<typeof mountView>) {
  await wrapper.get('input[type="email"]').setValue('member@example.test')
  await wrapper.get('input[type="password"]').setValue('example-password')
  await wrapper.get('form').trigger('submit')
}

beforeEach(() => {
  vi.clearAllMocks()
  routeQuery.redirect = undefined
  getBrand.mockResolvedValue({ enterprise_name: '示例企业', slogan: '企业访问' })
  replace.mockResolvedValue(undefined)
})

describe('EnterpriseLoginView', () => {
  it('uses the original AuthLayout structure and keeps enterprise branding, form and recovery link', async () => {
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.findAll('.auth-layout')).toHaveLength(1)
    expect(wrapper.findAll('.auth-panel')).toHaveLength(1)
    expect(wrapper.get('.auth-brand').text()).toContain('示例企业')
    expect(wrapper.get('.enterprise-auth-background').attributes('style')).toContain('/logo.svg')
    expect(wrapper.get('input[type="email"]').attributes('autocomplete')).toBe('username')
    expect(wrapper.get('input[type="password"]').attributes('autocomplete')).toBe('current-password')
    expect(wrapper.get('a[href="/enterprise/forgot-password"]').text()).toBe('忘记密码')
    expect(layoutSource).toContain('max-width:440px')
    expect(layoutSource).toContain('@media(max-width:480px)')
    expect(layoutSource).toContain('dark:bg-dark-950')
    expect(layoutSource).toContain('.auth-layout-enterprise .auth-brand h1{@apply text-gray-900 dark:text-gray-100')
    expect(layoutSource).toContain('.auth-layout-enterprise .auth-brand p{@apply text-gray-500 dark:text-dark-400')
    expect(viewSource).toContain('dark:text-gray-100')
    expect(viewSource).toContain('dark:text-dark-400')
    expect(viewSource).not.toMatch(/#409eff|#2563eb/i)
    wrapper.unmount()
  })

  it('redirects successfully to the existing role home or mandatory password change', async () => {
    const wrapper = mountView()
    login.mockResolvedValue({ role: 'enterprise_admin', force_password_change: false })
    await fillAndSubmit(wrapper)
    await flushPromises()
    expect(login).toHaveBeenCalledWith({ email: 'member@example.test', password: 'example-password' })
    expect(replace).toHaveBeenCalledWith('/enterprise/admin/workbench')
    wrapper.unmount()

    const next = mountView()
    routeQuery.redirect = '/enterprise/admin/workbench'
    login.mockResolvedValue({ role: 'employee', force_password_change: true })
    await fillAndSubmit(next)
    await flushPromises()
    expect(replace).toHaveBeenCalledWith('/enterprise/change-password')
    next.unmount()
  })

  it('disables inputs while loading, ignores repeated submits and rejects an external redirect', async () => {
    let resolveLogin!: (value: { role: 'employee'; force_password_change: false }) => void
    login.mockReturnValue(new Promise((resolve) => { resolveLogin = resolve }))
    routeQuery.redirect = '//outside.test/enterprise/home'
    const wrapper = mountView()
    await fillAndSubmit(wrapper)
    await wrapper.get('form').trigger('submit')
    expect(login).toHaveBeenCalledTimes(1)
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('input[type="password"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('button[type="submit"]').text()).toContain('登录中')

    resolveLogin({ role: 'employee', force_password_change: false })
    await flushPromises()
    expect(replace).toHaveBeenCalledWith('/enterprise/home')
    wrapper.unmount()
  })

  it('shows an actionable local error without displaying backend details', async () => {
    login.mockRejectedValue({ reason: 'INVALID_CREDENTIALS', message: 'sensitive backend detail' })
    const wrapper = mountView()
    await fillAndSubmit(wrapper)
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('邮箱或密码错误')
    expect(wrapper.text()).not.toContain('sensitive backend detail')
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })
})
