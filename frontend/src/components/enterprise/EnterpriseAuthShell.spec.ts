import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import EnterpriseAuthShell from './EnterpriseAuthShell.vue'

const getBrand = vi.hoisted(() => vi.fn())

vi.mock('@/api/enterprise', () => ({
  enterpriseAPI: { getBrand },
}))

class FakeImage {
  onload: (() => void) | null = null
  onerror: (() => void) | null = null
  private _src = ''
  get src() { return this._src }
  set src(value: string) {
    this._src = value
    queueMicrotask(() => FakeImage.behavior(this))
  }
  static behavior: (image: FakeImage) => void = (image) => image.onload?.()
}

const brandResponse = {
  enterprise_name: 'Acme',
  title: 'Acme 工作台',
  body: '企业访问',
  slogan: '安全入口',
  background_url: '/api/v1/enterprise/brand/background',
  background_content_type: 'image/png',
  background_sha256: 'a'.repeat(64),
  background_size_bytes: 128,
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('EnterpriseAuthShell brand background', () => {
  it('applies the controlled background resource and its content hash once the image is verified loadable', async () => {
    FakeImage.behavior = (image) => image.onload?.()
    vi.stubGlobal('Image', FakeImage)
    getBrand.mockResolvedValue(brandResponse)

    const wrapper = mount(EnterpriseAuthShell, { slots: { default: '<div>登录表单</div>' } })
    await flushPromises()
    await flushPromises()

    expect(wrapper.get('.auth-background').attributes('style')).toContain(
      'url("/api/v1/enterprise/brand/background?v=' + 'a'.repeat(64) + '")',
    )
    expect(wrapper.text()).toContain('Acme 工作台')
    wrapper.unmount()
  })

  it('falls back to the default background when the stored object fails to load (deleted/unreadable in object storage)', async () => {
    FakeImage.behavior = (image) => image.onerror?.()
    vi.stubGlobal('Image', FakeImage)
    getBrand.mockResolvedValue(brandResponse)

    const wrapper = mount(EnterpriseAuthShell, { slots: { default: '<div>登录表单</div>' } })
    await flushPromises()
    await flushPromises()

    const style = wrapper.get('.auth-background').attributes('style') || ''
    expect(style).toContain('url("/logo.svg")')
    expect(style).not.toContain('/api/v1/enterprise/brand/background')
    wrapper.unmount()
  })
})
