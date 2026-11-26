import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import Icon from '@/components/icons/Icon.vue'
import enterpriseBrandViewSource from '../EnterpriseBrandView.vue?raw'

const { getAdminBrand, updateBrand, uploadBrandBackground } = vi.hoisted(() => ({
  getAdminBrand: vi.fn(),
  updateBrand: vi.fn(),
  uploadBrandBackground: vi.fn(),
}))

vi.mock('@/api/enterprise', () => ({
  enterpriseAPI: { getAdminBrand, updateBrand, uploadBrandBackground },
}))

const defaultBrand = {
  enterprise_name: '',
  title: '',
  body: '',
  slogan: '',
  background_url: '/logo.svg',
  background_content_type: '',
  background_sha256: '',
  background_size_bytes: 0,
}

const originalLocation = window.location

async function mountBrandView(hostname = originalLocation.hostname) {
  Object.defineProperty(window, 'location', {
    configurable: true,
    value: { ...originalLocation, hostname },
  })
  vi.resetModules()
  const { default: EnterpriseBrandView } = await import('../EnterpriseBrandView.vue')
  return mount(EnterpriseBrandView, { global: { plugins: [ElementPlus] } })
}

describe('EnterpriseBrandView', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    getAdminBrand.mockResolvedValue({ ...defaultBrand })
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', { configurable: true, value: originalLocation })
    vi.resetModules()
  })

  it('renders the preview without a shield icon and shows the static field limits', async () => {
    const wrapper = await mountBrandView()
    expect(getAdminBrand).toHaveBeenCalledWith({ suppressUnavailableRedirect: true })
    await flushPromises()

    expect(wrapper.findComponent(Icon).exists()).toBe(false)
    expect(wrapper.text()).toContain('标题上限')
    expect(wrapper.text()).toContain('40 字符')
    expect(wrapper.text()).toContain('正文上限')
    expect(wrapper.text()).toContain('120 字符')
    expect(wrapper.text()).toContain('Slogan 上限')
    expect(wrapper.text()).toContain('60 字符')
    wrapper.unmount()
  })

  it('keeps the preview container and copy positioned for both the default and custom background', async () => {
    const wrapper = await mountBrandView('tenant.ainoriahub.com')
    await flushPromises()

    const assertPreviewLayout = () => {
      const preview = wrapper.get('.preview')
      expect(enterpriseBrandViewSource).toContain('.preview{position:relative;aspect-ratio:16/10;border-radius:8px;background:#173f3d center/cover;color:white;overflow:hidden}')
      expect(enterpriseBrandViewSource).toContain('.preview-copy{position:absolute;left:0;right:0;bottom:0;padding:28px}')
      expect(preview.classes()).toContain('preview')
    }

    expect(wrapper.get('.preview').classes()).toContain('painuoyun-default-background')
    assertPreviewLayout()

    getAdminBrand.mockResolvedValue({
      ...defaultBrand,
      background_url: '/api/v1/enterprise/brand/background',
      background_sha256: 'a'.repeat(64),
    })
    await wrapper.get('button').trigger('click')
    await flushPromises()

    expect(wrapper.get('.preview').classes()).not.toContain('painuoyun-default-background')
    expect(wrapper.get('.preview').attributes('style')).toContain('/api/v1/enterprise/brand/background')
    assertPreviewLayout()
    wrapper.unmount()
  })

  it('submits API-backed custom copy unchanged when another setting is saved on a 派诺云 host', async () => {
    const storedBrand = {
      ...defaultBrand,
      enterprise_name: 'Acme Sub2API 团队',
      title: 'Sub2API 专属工作台',
      body: '保留 Sub2API 作为企业业务文案',
      slogan: 'Sub2API 企业入口',
    }
    getAdminBrand.mockResolvedValue(storedBrand)
    updateBrand.mockResolvedValue(storedBrand)
    const wrapper = await mountBrandView('tenant.ainoriahub.com')
    await flushPromises()

    const textInputs = wrapper.findAll('input:not([type="file"])')
    await textInputs[1].setValue('已调整的企业标题')
    await wrapper.get('button.el-button--primary').trigger('click')
    await flushPromises()

    expect(updateBrand).toHaveBeenCalledWith({
      enterprise_name: storedBrand.enterprise_name,
      title: '已调整的企业标题',
      body: storedBrand.body,
      slogan: storedBrand.slogan,
      background_url: '/logo.svg',
    })
    wrapper.unmount()
  })

  it('shows a saved-summary banner after saving, and clears it on the next reload (先失败后通过覆盖场景：banner 生命周期)', async () => {
    const wrapper = await mountBrandView()
    await flushPromises()

    expect(wrapper.find('[data-testid="brand-saved-banner"]').exists()).toBe(false)

    updateBrand.mockResolvedValue({ ...defaultBrand, title: '自定义标题' })
    await wrapper.get('button.el-button--primary').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="brand-saved-banner"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('已自定义')

    getAdminBrand.mockResolvedValue({ ...defaultBrand })
    await wrapper.get('button').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="brand-saved-banner"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
