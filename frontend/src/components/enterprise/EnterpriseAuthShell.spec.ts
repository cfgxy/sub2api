import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import EnterpriseAuthShell from './EnterpriseAuthShell.vue'

const getBrand = vi.hoisted(() => vi.fn())

vi.mock('@/api/enterprise', () => ({
  enterpriseAPI: { getBrand },
}))

describe('EnterpriseAuthShell brand background', () => {
  it('applies the controlled background resource and its content hash', async () => {
    getBrand.mockResolvedValue({
      enterprise_name: 'Acme',
      title: 'Acme 工作台',
      body: '企业访问',
      slogan: '安全入口',
      background_url: '/api/v1/enterprise/brand/background',
      background_content_type: 'image/png',
      background_sha256: 'a'.repeat(64),
      background_size_bytes: 128,
    })

    const wrapper = mount(EnterpriseAuthShell, { slots: { default: '<div>登录表单</div>' } })
    await flushPromises()

    expect(wrapper.get('.auth-background').attributes('style')).toContain(
      'url("/api/v1/enterprise/brand/background?v=' + 'a'.repeat(64) + '")',
    )
    expect(wrapper.text()).toContain('Acme 工作台')
    wrapper.unmount()
  })
})
