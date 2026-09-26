import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'

import EnterpriseLayout from '../EnterpriseLayout.vue'
import { useAppStore } from '@/stores/app'
import { useEnterpriseAuthStore } from '@/stores/enterpriseAuth'
import type { EnterprisePrincipal } from '@/types/enterprise'

const Empty = { template: '<div />' }

const NAV_PATHS = [
  '/enterprise/admin/workbench',
  '/enterprise/admin/usage',
  '/enterprise/admin/audit',
  '/enterprise/admin/allocation',
  '/enterprise/admin/employees',
  '/enterprise/admin/keys',
  '/enterprise/admin/departments',
  '/enterprise/admin/brand',
  '/enterprise/admin/sessions',
  '/enterprise/home',
  '/enterprise/usage',
  '/enterprise/keys',
  '/enterprise/settings',
]

function buildRouter(): Router {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      ...NAV_PATHS.map((path) => ({ path, component: Empty, meta: { title: '页面' } })),
      { path: '/enterprise/login', component: Empty },
    ],
  })
}

function mountShell(role: EnterprisePrincipal['role']) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const auth = useEnterpriseAuthStore()
  auth.principal = { id: 1, email: 'owner@example.com', name: '示例用户', role } as EnterprisePrincipal
  const app = useAppStore()
  const router = buildRouter()
  const wrapper = mount(EnterpriseLayout, { global: { plugins: [pinia, router] } })
  return { wrapper, app, auth, router }
}

beforeEach(() => {
  localStorage.clear()
  document.documentElement.classList.remove('dark')
  // setup.ts 的 matchMedia polyfill 对一切查询返回 matches: true；
  // 这里覆盖为「桌面视口 true、prefers-color-scheme: dark false」，
  // 使主题初始化与亮色默认行为可断言。
  window.matchMedia = ((query: string) => ({
    matches: !query.includes('prefers-color-scheme'),
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(),
  })) as unknown as typeof window.matchMedia
})

describe('EnterpriseLayout（L1 外壳重制）', () => {
  it('管理员侧边栏渲染 9 项导航，员工渲染 5 项', () => {
    const admin = mountShell('enterprise_admin')
    expect(admin.wrapper.findAll('nav a')).toHaveLength(9)
    admin.wrapper.unmount()

    const employee = mountShell('employee')
    expect(employee.wrapper.findAll('nav a')).toHaveLength(5)
    employee.wrapper.unmount()
  })

  it('点击折叠按钮通过 appStore 切换宽度（72px ↔ 256px）', async () => {
    const { wrapper, app } = mountShell('enterprise_admin')
    const aside = wrapper.find('aside')
    expect(aside.classes()).toContain('w-64')

    await wrapper.find('[data-testid="sidebar-collapse"]').trigger('click')
    expect(app.sidebarCollapsed).toBe(true)
    expect(aside.classes()).toContain('w-[72px]')
    expect(wrapper.find('[data-testid="main-area"]').classes()).toContain('lg:ml-[72px]')

    await wrapper.find('[data-testid="sidebar-collapse"]').trigger('click')
    expect(app.sidebarCollapsed).toBe(false)
    expect(aside.classes()).toContain('w-64')
  })

  it('移动端遮罩点击后关闭抽屉', async () => {
    const { wrapper, app } = mountShell('employee')
    app.mobileOpen = true
    await wrapper.vm.$nextTick()

    const overlay = wrapper.find('[data-testid="mobile-overlay"]')
    expect(overlay.exists()).toBe(true)
    await overlay.trigger('click')
    expect(app.mobileOpen).toBe(false)
  })

  it('顶栏移动端菜单按钮切换抽屉，桌面端隐藏', async () => {
    const { wrapper, app } = mountShell('employee')
    const menuBtn = wrapper.find('[data-testid="mobile-menu-toggle"]')
    expect(menuBtn.classes()).toContain('lg:hidden')

    await menuBtn.trigger('click')
    expect(app.mobileOpen).toBe(true)
  })

  it('退出登录调用 auth.logout 并跳转 /enterprise/login', async () => {
    const { wrapper, auth, router } = mountShell('enterprise_admin')
    const logoutSpy = vi.spyOn(auth, 'logout').mockResolvedValue()

    await wrapper.find('[data-testid="user-dropdown-toggle"]').trigger('click')
    await wrapper.find('[data-testid="logout"]').trigger('click')
    await vi.waitFor(() => {
      expect(router.currentRoute.value.path).toBe('/enterprise/login')
    })
    expect(logoutSpy).toHaveBeenCalledTimes(1)
  })

  it('侧边栏底部提供主题切换（亮暗双态）', async () => {
    const { wrapper } = mountShell('employee')
    expect(document.documentElement.classList.contains('dark')).toBe(false)

    await wrapper.find('[data-testid="theme-toggle"]').trigger('click')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(localStorage.getItem('theme')).toBe('dark')
  })
})
