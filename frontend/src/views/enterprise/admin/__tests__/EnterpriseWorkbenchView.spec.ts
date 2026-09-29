import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import EnterpriseWorkbenchView from '../EnterpriseWorkbenchView.vue'
import { enterpriseTestLocale } from '@/views/admin/__tests__/enterpriseTestI18n'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: (await import('@/views/admin/__tests__/enterpriseTestI18n')).useEnterpriseTestI18n,
}))

const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div/>' } }, { path: '/enterprise/admin/audit', component: { template: '<div/>' } }] })

const { getWorkbenchSummary, listDepartments, listEmployees, listWorkbenchAuditEvents } = vi.hoisted(() => ({
  getWorkbenchSummary: vi.fn(),
  listDepartments: vi.fn(),
  listEmployees: vi.fn(),
  listWorkbenchAuditEvents: vi.fn(),
}))

vi.mock('@/api/enterprise', () => ({
  enterpriseAPI: { getWorkbenchSummary, listDepartments, listEmployees, listWorkbenchAuditEvents },
}))

function mountView() {
  return mount(EnterpriseWorkbenchView, {
    global: {
      plugins: [router, createPinia()],
      stubs: { EnterpriseUsageTrendChart: { template: '<div class="chart-stub" />' } },
    },
  })
}

describe('EnterpriseWorkbenchView', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    enterpriseTestLocale.value = 'zh'
    listWorkbenchAuditEvents.mockResolvedValue({
      items: [{ id: 1, event_type: 'allocation.update', entity_type: 'employee', result: 'success', payload: {}, actor_ref: 'admin@example.com', created_at: new Date().toISOString() }],
      total: 1, page: 1, page_size: 5, pages: 1,
    })
    getWorkbenchSummary.mockResolvedValue({
      total_usage_credit: '2.75', total_requests: 1, employee_count: 1, active_employee_count: 1,
      subscription_status: 'active', subscription_plan: 'Business', enterprise_pool_limit: '100', enterprise_pool_used: '2.75', enterprise_pool_remaining: '97.25', enterprise_pool_exhausted: false, pool_source_status: 'available', pool_source: 'upstream subscription', pool_window_type: 'week', pool_window_anchor: new Date().toISOString(),
      employee_summaries: [{ employee_id: 22, email: 'employee@example.com', requests: 1, configured_credit: '12.50', usage_credit: '2.75', remaining_credit: '9.75', overage_credit: '0', recommendation: '当前额度范围内' }], usage_trend: [],
    })
    listDepartments.mockResolvedValue([])
    listEmployees.mockResolvedValue([])
  })

  it('uses weekly as the explicit MVP query and renders the employee allocation overview', async () => {
    const wrapper = mountView()
    await flushPromises()

    const summaryParams = getWorkbenchSummary.mock.calls[0]?.[0] as Record<string, unknown>
    expect(summaryParams).not.toHaveProperty('window_type')
    expect(wrapper.text()).toContain('employee@example.com')
    expect(wrapper.text()).toContain('调整额度')
    expect(wrapper.text()).toContain('当前额度分配范围内')
    expect(wrapper.text()).not.toContain('当前额度范围内')
    expect(wrapper.text()).not.toContain('allocation.update')
    expect(wrapper.text()).toContain('admin@example.com')
    wrapper.unmount()
  })

  it('shows unknown audit types as localized labels with inspectable technical identifiers', async () => {
    listWorkbenchAuditEvents.mockResolvedValueOnce({
      items: [{ id: 2, event_type: 'custom.future', entity_type: 'custom_entity', result: 'success', payload: {}, actor_ref: 'admin@example.com', created_at: new Date().toISOString() }],
      total: 1, page: 1, page_size: 5, pages: 1,
    })
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('未知事件')
    expect(wrapper.text()).toContain('未知对象')
    expect(wrapper.get('[title="custom.future · custom_entity"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('renders the glossary-mapped column headers instead of raw English enum words', async () => {
    const wrapper = mountView()
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('剩余额度')
    expect(text).toContain('超用量')
    expect(text).toContain('实际费用')
    expect(text).not.toContain('actual cost')
    wrapper.unmount()
  })

  it('shows the recent-activity source-unavailable state without blocking the summary panels', async () => {
    listWorkbenchAuditEvents.mockRejectedValue(new Error('audit source unavailable'))
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('审计数据源暂时不可用')
    expect(wrapper.text()).toContain('employee@example.com')
    wrapper.unmount()
  })

  it('clears prior results and marks the source unavailable after a reload failure', async () => {
    const wrapper = mountView()
    await flushPromises()
    getWorkbenchSummary.mockRejectedValueOnce(new Error('source unavailable'))
    await wrapper.get('[data-testid="workbench-refresh"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('部分数据源暂时不可用')
    expect(wrapper.text()).not.toContain('employee@example.com')
    wrapper.unmount()
  })

  describe('en locale renders no hard-coded Chinese', () => {
    const CJK = /[\u3400-\u9fff\uff00-\uffef]/
    const renderedHtml = (wrapper: ReturnType<typeof mountView>) => wrapper.html().replace(/<!--[\s\S]*?-->/g, '')

    async function mountEnglish(options: { failSummary?: boolean; empty?: boolean } = {}) {
      enterpriseTestLocale.value = 'en'
      if (options.failSummary) {
        getWorkbenchSummary.mockRejectedValue(new Error('source unavailable'))
        listDepartments.mockRejectedValue(new Error('source unavailable'))
      }
      if (options.empty) {
        listWorkbenchAuditEvents.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 5, pages: 0 })
        getWorkbenchSummary.mockResolvedValue({
          total_usage_credit: '0', total_requests: 0, employee_count: 0, active_employee_count: 0,
          subscription_status: 'active', subscription_plan: 'Business', enterprise_pool_limit: '100', enterprise_pool_used: '100',
          enterprise_pool_remaining: '0', enterprise_pool_exhausted: true, pool_source_status: 'available', pool_window_type: 'week',
          pool_window_anchor: new Date().toISOString(), pool_observed_at: new Date().toISOString(),
          scheduled_subscription_since: new Date().toISOString(), scheduled_subscription_plan: '',
          employee_summaries: [], usage_trend: [],
        })
      }
      const wrapper = mountView()
      await flushPromises()
      return wrapper
    }

    it('populated state: headings, table headers, cards and dates are English', async () => {
      const wrapper = await mountEnglish()
      const text = wrapper.text()
      expect(text).toContain('Recommended action')
      expect(text).toContain('Employee allocation overview')
      expect(text).toContain('Usage trend')
      expect(text).toContain('Admin workbench')
      expect(renderedHtml(wrapper)).not.toMatch(CJK)
      wrapper.unmount()
    })

    it('empty and exhausted state (incl. aria-label, empty-state copy, scheduled plan) has no Chinese', async () => {
      const wrapper = await mountEnglish({ empty: true })
      const text = wrapper.text()
      expect(text).toContain('No usage for the current filter')
      expect(text).toContain('No trend data for the current filter')
      expect(text).toContain('No recent activity')
      expect(text).toContain('Unknown plan')
      expect(renderedHtml(wrapper)).not.toMatch(CJK)
      wrapper.unmount()
    })

    it('source-unavailable state and its toast are English', async () => {
      const wrapper = await mountEnglish({ failSummary: true })
      expect(wrapper.text()).toContain('Some data sources are temporarily unavailable')
      expect(wrapper.text()).toContain('Summary')
      expect(wrapper.text()).toContain('Organization directory')
      expect(renderedHtml(wrapper)).not.toMatch(CJK)
      wrapper.unmount()
    })
  })
})
