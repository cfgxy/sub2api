import { afterEach, describe, expect, it, vi } from 'vitest'
import { i18n, loadLocaleMessages } from '@/i18n'
import { resolveRouteDocumentTitle } from '@/router/title'

vi.mock('@/i18n', async () => {
  const [{ default: zh }, { default: en }] = await Promise.all([
    import('@/i18n/locales/zh'),
    import('@/i18n/locales/en'),
  ])
  const locale = { value: 'en' }
  return {
    i18n: {
      global: {
        locale,
        t: (key: string) => {
          const messages = locale.value === 'zh' ? zh : en
          const value = key.split('.').reduce<unknown>((entry, part) =>
            entry && typeof entry === 'object' ? (entry as Record<string, unknown>)[part] : undefined, messages)
          return typeof value === 'string' ? value : key
        },
      },
    },
    loadLocaleMessages: async () => {},
  }
})

vi.mock('@/composables/useNavigationLoading', () => ({
  useNavigationLoadingState: () => ({ startNavigation: vi.fn(), endNavigation: vi.fn() }),
}))

vi.mock('@/composables/useRoutePrefetch', () => ({
  useRoutePrefetch: () => ({ triggerPrefetch: vi.fn(), cancelPendingPrefetch: vi.fn() }),
}))

const originalLocale = i18n.global.locale.value

afterEach(() => {
  i18n.global.locale.value = originalLocale
})

describe('平台企业页面页签标题', () => {
  it('列表、创建和详情页在中英文切换后使用对应语言标题', async () => {
    const { default: router } = await import('@/router')
    await Promise.all([loadLocaleMessages('zh'), loadLocaleMessages('en')])

    const pages = [
      ['/admin/enterprises', '企业租户', 'Enterprise tenants'],
      ['/admin/enterprises/new', '创建企业', 'Create enterprise'],
      ['/admin/enterprises/enterprise-1', '企业详情', 'Enterprise details'],
    ] as const

    for (const [path, zhTitle, enTitle] of pages) {
      const route = router.resolve(path)
      i18n.global.locale.value = 'zh'
      expect(resolveRouteDocumentTitle(route, 'Sub2API')).toBe(`${zhTitle} - Sub2API`)
      i18n.global.locale.value = 'en'
      expect(resolveRouteDocumentTitle(route, 'Sub2API')).toBe(`${enTitle} - Sub2API`)
    }
  })
})
