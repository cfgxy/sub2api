import { beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'

import type { DashboardStats } from '@/types'
import DashboardView from '../DashboardView.vue'

const { getSnapshotV2, getUserUsageTrend, getUserSpendingRanking } = vi.hoisted(() => ({
  getSnapshotV2: vi.fn(),
  getUserUsageTrend: vi.fn(),
  getUserSpendingRanking: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    dashboard: {
      getSnapshotV2,
      getUserUsageTrend,
      getUserSpendingRanking
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn()
  })
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({
    push: vi.fn()
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

const formatLocalDate = (date: Date): string => {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

const createDashboardStats = (): DashboardStats => ({
  total_users: 0,
  today_new_users: 0,
  active_users: 0,
  hourly_active_users: 0,
  stats_updated_at: '',
  stats_stale: false,
  total_api_keys: 0,
  active_api_keys: 0,
  total_accounts: 0,
  normal_accounts: 0,
  error_accounts: 0,
  ratelimit_accounts: 0,
  overload_accounts: 0,
  total_requests: 0,
  total_input_tokens: 0,
  total_output_tokens: 0,
  total_cache_creation_tokens: 0,
  total_cache_read_tokens: 0,
  total_tokens: 0,
  total_cost: 0,
  total_actual_cost: 0,
  today_requests: 0,
  today_input_tokens: 0,
  today_output_tokens: 0,
  today_cache_creation_tokens: 0,
  today_cache_read_tokens: 0,
  today_tokens: 0,
  today_cost: 0,
  today_actual_cost: 0,
  average_duration_ms: 0,
  uptime: 0,
  rpm: 0,
  tpm: 0
})

describe('admin DashboardView', () => {
  const render = () => mount(DashboardView, {
    global: {
      stubs: {
        PlatformEnterpriseShell: { template: '<div data-testid="platform-shell"><slot /></div>' },
        AppLayout: { template: '<div data-testid="standard-layout"><slot /></div>' },
        LoadingSpinner: true,
        Icon: true,
        DateRangePicker: true,
        Select: true,
        ModelDistributionChart: true,
        TokenUsageTrend: true,
        Line: true
      }
    }
  })

  beforeEach(() => {
    setActivePinia(createPinia())

    getSnapshotV2.mockReset()
    getUserUsageTrend.mockReset()
    getUserSpendingRanking.mockReset()

    getSnapshotV2.mockResolvedValue({
      stats: createDashboardStats(),
      trend: [],
      models: []
    })
    getUserUsageTrend.mockResolvedValue({
      trend: [],
      start_date: '',
      end_date: '',
      granularity: 'hour'
    })
    getUserSpendingRanking.mockResolvedValue({
      ranking: [],
      total_actual_cost: 0,
      total_requests: 0,
      total_tokens: 0,
      start_date: '',
      end_date: ''
    })
  })

  it('uses last 24 hours as default dashboard range', async () => {
    render()

    await flushPromises()

    const now = new Date()
    const yesterday = new Date(now.getTime() - 24 * 60 * 60 * 1000)

    expect(getSnapshotV2).toHaveBeenCalledTimes(1)
    expect(getSnapshotV2).toHaveBeenCalledWith(expect.objectContaining({
      start_date: formatLocalDate(yesterday),
      end_date: formatLocalDate(now),
      granularity: 'hour'
    }))
  })

  it('uses the standard layout and distinguishes loading, empty and normal states', async () => {
    let resolveSnapshot!: (value: unknown) => void
    getSnapshotV2.mockReturnValueOnce(new Promise((resolve) => { resolveSnapshot = resolve }))
    const wrapper = render()
    await nextTick()

    expect(wrapper.find('[data-testid="standard-layout"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="platform-shell"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="dashboard-loading"]').exists()).toBe(true)
    resolveSnapshot({ stats: createDashboardStats(), trend: [], models: [] })
    await flushPromises()
    expect(wrapper.find('[data-testid="dashboard-empty"]').exists()).toBe(true)

    getSnapshotV2.mockResolvedValueOnce({ stats: { ...createDashboardStats(), total_users: 3 }, trend: [], models: [] })
    await wrapper.find('[data-testid="dashboard-refresh"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="dashboard-stats"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="dashboard-empty"]').exists()).toBe(false)
  })

  it('shows a retryable error when the initial statistics request fails', async () => {
    getSnapshotV2.mockRejectedValueOnce(new Error('upstream unavailable'))
    const wrapper = render()
    await flushPromises()
    expect(wrapper.find('[data-testid="dashboard-error"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('upstream unavailable')

    await wrapper.find('[data-testid="dashboard-retry"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="dashboard-error"]').exists()).toBe(false)
    expect(getSnapshotV2).toHaveBeenCalledTimes(2)
  })
})
