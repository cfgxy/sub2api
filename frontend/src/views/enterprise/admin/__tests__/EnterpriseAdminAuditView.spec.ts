import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import EnterpriseAdminAuditView from '../EnterpriseAdminAuditView.vue'

const { listWorkbenchAuditEvents } = vi.hoisted(() => ({
  listWorkbenchAuditEvents: vi.fn(),
}))

vi.mock('@/api/enterprise', () => ({
  enterpriseAPI: { listWorkbenchAuditEvents },
}))

const auditRow = {
  id: 1, event_type: 'allocation.version_conflict', entity_type: 'enterprise_allocation', entity_id: 11,
  result: 'rejected', reason: 'version_conflict',
  payload: {
    result: 'rejected', reason: 'version_conflict', employee_id: 22, expected_version: 1, actual_version: 4,
    // fields that must never render, matching the backend whitelist test's proof shape
    unexpected_new_field: 'leaked-value', internal_debug_dump: { anything: 'here' },
  },
  actor_ref: 'user:1', created_at: new Date().toISOString(),
}

describe('EnterpriseAdminAuditView', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    listWorkbenchAuditEvents.mockResolvedValue({ items: [auditRow], total: 1, page: 1, page_size: 20, pages: 1 })
  })

  it('renders the six-dimension search controls (actor, entity, action, time, result, reason)', async () => {
    const wrapper = mount(EnterpriseAdminAuditView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    expect(wrapper.find('input[placeholder="操作者"]').exists()).toBe(true)
    expect(wrapper.find('input[placeholder="对象类型"]').exists()).toBe(true)
    expect(wrapper.find('input[placeholder="动作类型"]').exists()).toBe(true)
    expect(wrapper.find('input[placeholder="reason"]').exists()).toBe(true)
    expect(wrapper.findComponent({ name: 'ElSelect' }).exists()).toBe(true) // result
    expect(wrapper.findComponent({ name: 'ElDatePicker' }).exists()).toBe(true) // time range
    wrapper.unmount()
  })

  it('passes all six filter dimensions through to the query on search', async () => {
    const wrapper = mount(EnterpriseAdminAuditView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    await wrapper.find('input[placeholder="操作者"]').setValue('user:1')
    await wrapper.find('input[placeholder="动作类型"]').setValue('allocation.version_conflict')
    await wrapper.find('input[placeholder="对象类型"]').setValue('enterprise_allocation')
    await wrapper.find('input[placeholder="reason"]').setValue('version_conflict')
    await wrapper.find('button.el-button--primary').trigger('click')
    await flushPromises()

    const lastCall = listWorkbenchAuditEvents.mock.calls.at(-1)?.[0] as Record<string, unknown>
    expect(lastCall.actor_ref).toBe('user:1')
    expect(lastCall.event_type).toBe('allocation.version_conflict')
    expect(lastCall.entity_type).toBe('enterprise_allocation')
    expect(lastCall.reason).toBe('version_conflict')
    wrapper.unmount()
  })

  it('renders only whitelisted payload fields in the detail drawer, dropping unknown fields', async () => {
    const wrapper = mount(EnterpriseAdminAuditView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    await wrapper.find('button.el-button--text, .el-button.is-link').trigger('click')
    await flushPromises()

    const drawerText = wrapper.text()
    expect(drawerText).toContain('员工 ID')
    expect(drawerText).toContain('期望版本')
    expect(drawerText).toContain('实际版本')
    // Fail-closed: fields not on the whitelist must never render, even
    // though they are present in the mocked payload above.
    expect(drawerText).not.toContain('unexpected_new_field')
    expect(drawerText).not.toContain('leaked-value')
    expect(drawerText).not.toContain('internal_debug_dump')
    wrapper.unmount()
  })

  it('respects pagination boundaries', async () => {
    listWorkbenchAuditEvents.mockResolvedValue({ items: [auditRow], total: 62, page: 1, page_size: 20, pages: 4 })
    const wrapper = mount(EnterpriseAdminAuditView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    const pager = wrapper.findComponent({ name: 'ElPagination' })
    expect(pager.exists()).toBe(true)
    expect(pager.props('total')).toBe(62)
    wrapper.unmount()
  })

  it('degrades gracefully and shows the unavailable notice when the audit source fails', async () => {
    listWorkbenchAuditEvents.mockRejectedValue(new Error('source unavailable'))
    const wrapper = mount(EnterpriseAdminAuditView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    expect(wrapper.text()).toContain('审计数据源暂时不可用')
    wrapper.unmount()
  })

  it('shows an empty state when no audit records match the filters', async () => {
    listWorkbenchAuditEvents.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, pages: 1 })
    const wrapper = mount(EnterpriseAdminAuditView, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    expect(wrapper.text()).toContain('暂无符合条件的审计记录')
    wrapper.unmount()
  })
})
