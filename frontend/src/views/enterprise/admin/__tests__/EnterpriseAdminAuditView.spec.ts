import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import EnterpriseAdminAuditView from '../EnterpriseAdminAuditView.vue'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: (await import('@/views/admin/__tests__/enterpriseTestI18n')).useEnterpriseTestI18n,
}))

const { listWorkbenchAuditEvents } = vi.hoisted(() => ({ listWorkbenchAuditEvents: vi.fn() }))
vi.mock('@/api/enterprise', () => ({ enterpriseAPI: { listWorkbenchAuditEvents } }))

const auditRow = {
  id: 1, event_type: 'allocation.version_conflict', entity_type: 'enterprise_allocation', entity_id: 11,
  result: 'rejected', reason: 'version_conflict',
  payload: { result: 'rejected', reason: 'version_conflict', employee_id: 22, expected_version: 1, actual_version: 4, unexpected_new_field: 'leaked-value', internal_debug_dump: { anything: 'here' } },
  actor_ref: 'user:1', created_at: new Date().toISOString(),
}
const page = (items = [auditRow], total = items.length) => ({ items, total, page: 1, page_size: 20, pages: Math.ceil(total / 20) })
function mountView() { return mount(EnterpriseAdminAuditView, { global: { plugins: [createPinia()], stubs: { teleport: true } } }) }

describe('EnterpriseAdminAuditView', () => {
  beforeEach(() => { vi.resetAllMocks(); listWorkbenchAuditEvents.mockResolvedValue(page()) })

  it('maps event and entity labels without losing inspectable raw codes', async () => {
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('额度版本冲突')
    expect(wrapper.text()).toContain('员工额度')
    expect(wrapper.text()).not.toContain('allocation.version_conflict')
    expect(wrapper.get('[title="allocation.version_conflict"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('keeps all six supported filter dimensions in the query', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('input[placeholder="操作者"]').setValue('user:1')
    await wrapper.get('input[placeholder="动作类型"]').setValue('allocation.version_conflict')
    await wrapper.get('input[placeholder="对象类型"]').setValue('enterprise_allocation')
    await wrapper.get('input[placeholder="原因"]').setValue('version_conflict')
    await wrapper.findAll('button').find(item => item.text().includes('查询'))!.trigger('click')
    await flushPromises()
    expect(listWorkbenchAuditEvents.mock.calls.at(-1)?.[0]).toMatchObject({ actor_ref: 'user:1', event_type: 'allocation.version_conflict', entity_type: 'enterprise_allocation', reason: 'version_conflict' })
    wrapper.unmount()
  })

  it('shows only whitelisted payload fields in details, including the changed credit', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find(item => item.text() === '查看')!.trigger('click')
    expect(wrapper.text()).toContain('期望版本')
    expect(wrapper.text()).toContain('实际版本')
    expect(wrapper.text()).toContain('allocation.version_conflict')
    expect(wrapper.text()).not.toContain('leaked-value')
    expect(wrapper.text()).not.toContain('internal_debug_dump')
    wrapper.unmount()

    listWorkbenchAuditEvents.mockResolvedValueOnce(page([{ ...auditRow, id: 2, event_type: 'allocation.credit_changed', payload: { credit: '15.00' } }]))
    const changed = mountView()
    await flushPromises()
    await changed.findAll('button').find(item => item.text() === '查看')!.trigger('click')
    expect(changed.text()).toContain('调整后额度')
    expect(changed.text()).toContain('15.00')
    changed.unmount()
  })

  it('uses a localized fallback for unknown types with raw codes in the detail', async () => {
    listWorkbenchAuditEvents.mockResolvedValueOnce(page([{ ...auditRow, event_type: 'new.event', entity_type: 'new_entity' }]))
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('未知事件')
    expect(wrapper.text()).toContain('未知对象')
    await wrapper.findAll('button').find(item => item.text() === '查看')!.trigger('click')
    expect(wrapper.text()).toContain('new.event · new_entity')
    wrapper.unmount()
  })

  it('handles pagination, empty responses and the source-unavailable retry', async () => {
    listWorkbenchAuditEvents.mockResolvedValueOnce(page([auditRow], 62)).mockResolvedValueOnce(page([])).mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce(page())
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.getComponent({ name: 'Pagination' }).props('total')).toBe(62)
    await wrapper.findAll('button').find(item => item.text() === '刷新')!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('暂无符合条件的审计记录')
    await wrapper.findAll('button').find(item => item.text() === '刷新')!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('审计数据源暂时不可用')
    await wrapper.findAll('button').find(item => item.text() === '重试')!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('额度版本冲突')
    wrapper.unmount()
  })
})
