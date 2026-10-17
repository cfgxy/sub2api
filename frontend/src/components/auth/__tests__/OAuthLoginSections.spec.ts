import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import LinuxDoOAuthSection from '@/components/auth/LinuxDoOAuthSection.vue'
import DingTalkOAuthSection from '@/components/auth/DingTalkOAuthSection.vue'
import OidcOAuthSection from '@/components/auth/OidcOAuthSection.vue'

const routeState = vi.hoisted(() => ({
  query: {} as Record<string, unknown>
}))

vi.mock('vue-router', () => ({
  useRoute: () => routeState
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: { providerName?: string }) => params?.providerName ? `${key}:${params.providerName}` : key
  })
}))

describe('OAuth login sections', () => {
  beforeEach(() => {
    routeState.query = { redirect: '/billing?plan=pro', aff: 'AFF123' }
    window.sessionStorage.clear()
  })

  it.each([
    ['linuxdo', LinuxDoOAuthSection],
    ['dingtalk', DingTalkOAuthSection],
    ['oidc', OidcOAuthSection]
  ] as const)('emits a %s start request from the original button', async (provider, component) => {
    const originalHref = window.location.href
    const wrapper = mount(component, { props: { affCode: 'AFF456' } })

    await wrapper.get('button').trigger('click')

    expect(wrapper.emitted('start')?.[0]?.[0]).toEqual({
      provider,
      params: { redirect: '/billing?plan=pro' }
    })
    expect(window.sessionStorage.getItem('oauth_aff_code')).toBe('AFF456')
    expect(window.location.href).toBe(originalHref)
  })

  it('通过原生 OIDC 入口显示 NodeLoc 并保留 callback 路由', async () => {
    const wrapper = mount(OidcOAuthSection, { props: { providerName: 'NodeLoc' } })

    expect(wrapper.get('button').text()).toContain('auth.oidc.signIn:NodeLoc')
    await wrapper.get('button').trigger('click')

    expect(wrapper.emitted('start')?.[0]?.[0]).toEqual({
      provider: 'oidc',
      params: { redirect: '/billing?plan=pro' }
    })
  })

  it('NodeLoc 禁用时不发起授权请求', async () => {
    const wrapper = mount(OidcOAuthSection, { props: { providerName: 'NodeLoc', disabled: true } })

    expect(wrapper.get('button').attributes('disabled')).toBeDefined()
    await wrapper.get('button').trigger('click')

    expect(wrapper.emitted('start')).toBeUndefined()
  })

  it('includes a trimmed promo code in the LinuxDo OAuth request', async () => {
    const wrapper = mount(LinuxDoOAuthSection, {
      props: {
        affCode: 'AFF456',
        promoCode: ' PROMO789 '
      }
    })

    await wrapper.get('button').trigger('click')

    expect(wrapper.emitted('start')?.[0]?.[0]).toEqual({
      provider: 'linuxdo',
      params: {
        redirect: '/billing?plan=pro',
        promo_code: 'PROMO789'
      }
    })
  })
})
