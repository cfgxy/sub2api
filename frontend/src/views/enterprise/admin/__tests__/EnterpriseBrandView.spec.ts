import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import EnterpriseBrandView from '../EnterpriseBrandView.vue'
import Icon from '@/components/icons/Icon.vue'

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

describe('EnterpriseBrandView', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    getAdminBrand.mockResolvedValue({ ...defaultBrand })
  })

  it('renders the preview without a shield icon and shows the static field limits', async () => {
    const wrapper = mount(EnterpriseBrandView, { global: { plugins: [ElementPlus] } })
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

  it('shows a saved-summary banner after saving, and clears it on the next reload (先失败后通过覆盖场景：banner 生命周期)', async () => {
    const wrapper = mount(EnterpriseBrandView, { global: { plugins: [ElementPlus] } })
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
