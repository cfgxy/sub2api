import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'

import PlatformEnterpriseShell from '../PlatformEnterpriseShell.vue'
import { useAppStore } from '@/stores/app'

const Empty = { template: '<div />' }

function mountShell() {
  const pinia = createPinia()
  setActivePinia(pinia)
  const app = useAppStore()
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/admin/enterprises', component: Empty, meta: { title: '企业管理' } },
      { path: '/admin/audit-logs', component: Empty },
    ],
  })
  const wrapper = mount(PlatformEnterpriseShell, { global: { plugins: [pinia, router] } })
  return { wrapper, app }
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

describe('PlatformEnterpriseShell（L1 外壳重制）', () => {
  it('侧边栏渲染平台管理两项导航', () => {
    const { wrapper } = mountShell()
    const links = wrapper.findAll('nav a')
    expect(links).toHaveLength(2)
    expect(links[0].text()).toContain('企业管理')
    expect(links[1].text()).toContain('平台审计')
  })

  it('点击折叠按钮通过 appStore 切换宽度（72px ↔ 256px）', async () => {
    const { wrapper, app } = mountShell()
    const aside = wrapper.find('aside')
    expect(aside.classes()).toContain('w-64')

    await wrapper.find('[data-testid="sidebar-collapse"]').trigger('click')
    expect(app.sidebarCollapsed).toBe(true)
    expect(aside.classes()).toContain('w-[72px]')
    expect(wrapper.find('[data-testid="main-area"]').classes()).toContain('lg:ml-[72px]')
  })

  it('移动端遮罩点击后关闭抽屉', async () => {
    const { wrapper, app } = mountShell()
    app.mobileOpen = true
    await wrapper.vm.$nextTick()

    const overlay = wrapper.find('[data-testid="mobile-overlay"]')
    expect(overlay.exists()).toBe(true)
    await overlay.trigger('click')
    expect(app.mobileOpen).toBe(false)
  })

  it('侧边栏底部提供主题切换（亮暗双态）', async () => {
    const { wrapper } = mountShell()
    await wrapper.find('[data-testid="theme-toggle"]').trigger('click')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
  })
})
