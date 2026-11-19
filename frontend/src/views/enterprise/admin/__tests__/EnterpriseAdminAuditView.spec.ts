import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import EnterpriseAdminAuditView from '../EnterpriseAdminAuditView.vue'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: (await import('@/views/admin/__tests__/enterpriseTestI18n')).useEnterpriseTestI18n,
}))

const { listWorkbenchAuditEvents, listEmployees, listDepartments, listAdminKeys } = vi.hoisted(() => ({
  listWorkbenchAuditEvents: vi.fn(),
  listEmployees: vi.fn(),
  listDepartments: vi.fn(),
  listAdminKeys: vi.fn(),
}))
vi.mock('@/api/enterprise', () => ({ enterpriseAPI: { listWorkbenchAuditEvents, listEmployees, listDepartments, listAdminKeys } }))

const auditRow = {
  id: 1, event_type: 'allocation.version_conflict', entity_type: 'enterprise_allocation', entity_id: 11,
  result: 'rejected', reason: 'version_conflict',
  payload: { result: 'rejected', reason: 'version_conflict', employee_id: 22, expected_version: 1, actual_version: 4, unexpected_new_field: 'leaked-value', internal_debug_dump: { anything: 'here' } },
  actor_ref: 'user:1', created_at: new Date().toISOString(),
}
const page = (items = [auditRow], total = items.length) => ({ items, total, page: 1, page_size: 20, pages: Math.ceil(total / 20) })
function mountView() { return mount(EnterpriseAdminAuditView, { global: { plugins: [createPinia()], stubs: { teleport: true } } }) }

describe('EnterpriseAdminAuditView', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    listWorkbenchAuditEvents.mockResolvedValue(page())
    listEmployees.mockResolvedValue([{ id: 22, email: 'employee@example.com', status: 'active', must_change_password: false, version: 1 }])
    listDepartments.mockResolvedValue([{ id: 3, name: '研发部', created_at: '2026-01-01T00:00:00Z' }])
    listAdminKeys.mockResolvedValue([{ assignment_id: 1, api_key_id: 9, employee_id: 22, employee_email: 'employee@example.com', generation: 1, status: 'active', in_use: true, created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' }])
  })

  it('maps event and entity labels without losing inspectable raw codes', async () => {
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('额度版本冲突')
    expect(wrapper.text()).toContain('员工额度')
    expect(wrapper.text()).not.toContain('allocation.version_conflict')
    expect(wrapper.get('[title="allocation.version_conflict"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('keeps all supported filter dimensions in the query and removes cleared directory filters', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('input[placeholder="操作者"]').setValue('user:1')
    await wrapper.get('input[placeholder="动作类型"]').setValue('allocation.version_conflict')
    await wrapper.get('input[placeholder="对象类型"]').setValue('enterprise_allocation')
    await wrapper.get('input[placeholder="原因"]').setValue('version_conflict')
    const selects = wrapper.findAllComponents({ name: 'Select' })
    await selects[0].vm.$emit('update:modelValue', 'rejected')
    await selects[1].vm.$emit('update:modelValue', 22)
    await selects[2].vm.$emit('update:modelValue', 3)
    await selects[3].vm.$emit('update:modelValue', 9)
    await wrapper.findAll('button').find(item => item.text().includes('查询'))!.trigger('click')
    await flushPromises()
    expect(listWorkbenchAuditEvents.mock.calls.at(-1)?.[0]).toMatchObject({
      actor_ref: 'user:1', event_type: 'allocation.version_conflict', entity_type: 'enterprise_allocation', reason: 'version_conflict',
      result: 'rejected', employee_id: 22, department_id: 3, api_key_id: 9,
    })

    await selects[1].vm.$emit('update:modelValue', null)
    await selects[2].vm.$emit('update:modelValue', null)
    await selects[3].vm.$emit('update:modelValue', null)
    await wrapper.findAll('button').find(item => item.text().includes('查询'))!.trigger('click')
    await flushPromises()
    expect(listWorkbenchAuditEvents.mock.calls.at(-1)?.[0]).not.toHaveProperty('employee_id')
    expect(listWorkbenchAuditEvents.mock.calls.at(-1)?.[0]).not.toHaveProperty('department_id')
    expect(listWorkbenchAuditEvents.mock.calls.at(-1)?.[0]).not.toHaveProperty('api_key_id')
    wrapper.unmount()
  })

  it('keeps the audit list usable when a directory option request fails', async () => {
    listEmployees.mockRejectedValueOnce(new Error('employee directory offline'))
    const wrapper = mountView()
    await flushPromises()
    expect(listWorkbenchAuditEvents).toHaveBeenCalled()
    expect(wrapper.text()).toContain('额度版本冲突')
    expect(wrapper.findAllComponents({ name: 'Select' })[1].props('options')).toEqual([])
    expect(listDepartments).toHaveBeenCalledWith({ suppressUnavailableRedirect: true })
    expect(listAdminKeys).toHaveBeenCalledWith({ suppressUnavailableRedirect: true })
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
