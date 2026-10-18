import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import VersionBadge from '../VersionBadge.vue'
import { useAppStore } from '@/stores/app'
import { checkUpdates } from '@/api/admin/system'

vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isAdmin: true }) }))
vi.mock('@/api/admin/system', () => ({
  checkUpdates: vi.fn(), update: vi.fn(), rollback: vi.fn(), restart: vi.fn(),
  getRollbackVersions: vi.fn(), default: {}
}))
vi.mock('@/api/auth', () => ({ getPublicSettings: vi.fn() }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))

function settings(enabled: boolean) {
  return { version: '0.2.15-ob.20261018-1', auto_update_check_enabled: enabled }
}

describe('版本徽标', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(checkUpdates).mockReset()
    delete window.__APP_CONFIG__
  })

  it('关闭自动检查时显示公共配置中的版本且不声明最新', async () => {
    const store = useAppStore()
    window.__APP_CONFIG__ = settings(false) as NonNullable<typeof window.__APP_CONFIG__>
    const wrapper = mount(VersionBadge)
    await flushPromises()
    expect(wrapper.get('button').text()).toContain('v0.2.15-ob.20261018-1')
    expect(wrapper.find('.animate-pulse').exists()).toBe(false)
    expect(wrapper.get('button').attributes('title')).toBe('version.notChecked')
    expect(checkUpdates).not.toHaveBeenCalled()
    expect(store.versionLoaded).toBe(false)
    wrapper.unmount()
  })

  it('检查失败后保留当前版本并允许手动重试', async () => {
    window.__APP_CONFIG__ = settings(true) as NonNullable<typeof window.__APP_CONFIG__>
    vi.mocked(checkUpdates).mockRejectedValueOnce(new Error('测试错误'))
    const wrapper = mount(VersionBadge)
    await flushPromises()
    expect(wrapper.get('button').text()).toContain('v0.2.15-ob.20261018-1')
    expect(wrapper.get('button').attributes('title')).toBe('version.checkFailed')
    vi.mocked(checkUpdates).mockResolvedValueOnce({
      current_version: '0.2.15-ob.20261018-1', latest_version: '0.2.15',
      has_update: false, cached: false, build_type: 'release'
    })
    await wrapper.get('button').trigger('click')
    await wrapper.get('button[title="version.refresh"]').trigger('click')
    await flushPromises()
    expect(checkUpdates).toHaveBeenLastCalledWith(true)
    expect(wrapper.get('button').attributes('title')).toBe('version.upToDate')
    wrapper.unmount()
  })

  it.each([
    { cached: false, has_update: false, warning: '测试上游不可用' },
    { cached: true, has_update: false, warning: 'Using cached data: 测试上游不可用' },
    { cached: true, has_update: true, warning: 'Using cached data: 测试上游不可用' }
  ])('上游降级响应显示失败且手动重试后恢复：$cached/$has_update', async (fallback) => {
    const config = settings(false)
    window.__APP_CONFIG__ = config as NonNullable<typeof window.__APP_CONFIG__>
    const wrapper = mount(VersionBadge)
    await flushPromises()
    vi.mocked(checkUpdates).mockResolvedValueOnce({
      current_version: config.version, latest_version: config.version,
      build_type: 'release', ...fallback
    })
    await wrapper.get('button').trigger('click')
    await wrapper.get('button[title="version.refresh"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('button').text()).toContain(`v${config.version}`)
    expect(wrapper.get('button').attributes('title')).toBe('version.checkFailed')
    expect(wrapper.text()).toContain('version.checkFailed')
    expect(wrapper.text()).not.toContain('version.upToDate')
    expect(wrapper.text()).not.toContain('version.updateAvailable')
    expect(wrapper.text()).not.toContain(fallback.warning)
    expect(wrapper.find('.bg-green-100').exists()).toBe(false)

    vi.mocked(checkUpdates).mockResolvedValueOnce({
      current_version: config.version, latest_version: config.version,
      has_update: false, cached: false, build_type: 'release'
    })
    await wrapper.get('button[title="version.refresh"]').trigger('click')
    await flushPromises()
    expect(checkUpdates).toHaveBeenLastCalledWith(true)
    expect(wrapper.get('button').attributes('title')).toBe('version.upToDate')
    expect(wrapper.find('.bg-green-100').exists()).toBe(true)
    wrapper.unmount()
  })

  it('公共配置不可用且没有检查结果时显示稳定的未知状态', async () => {
    const store = useAppStore()
    store.publicSettingsLoaded = true
    vi.mocked(checkUpdates).mockRejectedValueOnce(new Error('配置不可用'))
    const wrapper = mount(VersionBadge)
    await flushPromises()
    expect(wrapper.find('.animate-pulse').exists()).toBe(false)
    expect(wrapper.get('button').text()).toContain('version.unknown')
    expect(wrapper.get('button').attributes('title')).not.toBe('version.upToDate')
    wrapper.unmount()
  })
})
