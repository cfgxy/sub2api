import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import DeveloperToolsModal from '../DeveloperToolsModal.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const stubs = {
  BaseDialog: {
    props: ['show'],
    template: '<div v-if="show" role="dialog"><slot /><slot name="footer" /></div>'
  },
  Icon: { template: '<span />' }
}

describe('开发工具弹窗', () => {
  it('显示 Codex++ 下载和关闭的无凭据导入入口', () => {
    const wrapper = mount(DeveloperToolsModal, {
      props: { show: true, canImportCcs: true, codexCandidate: null },
      global: { stubs }
    })
    expect(wrapper.get('a[href="https://github.com/cfgxy/CodexPlusPlus"]').attributes('target')).toBe('_blank')
    expect(wrapper.get('[data-testid="codex-import"]').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('keys.developerTools.codexUnavailable')
  })

  it('保留 CCSwitch 下载与旧导入动作', async () => {
    const wrapper = mount(DeveloperToolsModal, {
      props: { show: true, canImportCcs: true, codexCandidate: null },
      global: { stubs }
    })
    await wrapper.get('[data-testid="ccs-tab"]').trigger('click')
    expect(wrapper.get('a[href="https://github.com/farion1231/cc-switch"]').attributes('rel'))
      .toContain('noopener')
    await wrapper.get('[data-testid="ccs-import"]').trigger('click')
    expect(wrapper.emitted('importCcs')).toHaveLength(1)
  })

  it('原 CCSwitch 设置隐藏时不提供其导入 TAB', () => {
    const wrapper = mount(DeveloperToolsModal, {
      props: { show: true, canImportCcs: false, codexCandidate: null },
      global: { stubs }
    })
    expect(wrapper.find('[data-testid="ccs-tab"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="ccs-import"]').exists()).toBe(false)
  })

  it('重新打开时复位为 Codex++ TAB，不复用上次状态', async () => {
    const wrapper = mount(DeveloperToolsModal, {
      props: { show: true, canImportCcs: true, codexCandidate: null },
      global: { stubs }
    })
    await wrapper.get('[data-testid="ccs-tab"]').trigger('click')
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    expect(wrapper.get('[data-testid="codex-tab"]').attributes('aria-selected')).toBe('true')
  })
})
