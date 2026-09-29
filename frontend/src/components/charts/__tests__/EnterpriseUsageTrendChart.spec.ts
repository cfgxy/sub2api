import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import EnterpriseUsageTrendChart from '../EnterpriseUsageTrendChart.vue'

vi.mock('vue-chartjs', () => ({
  Line: { name: 'Line', props: ['data', 'options'], template: '<div class="chart" />' },
}))

describe('EnterpriseUsageTrendChart', () => {
  afterEach(() => document.documentElement.classList.remove('dark'))

  it('updates mounted chart options on light-dark-light switches', async () => {
    document.documentElement.classList.remove('dark')
    const wrapper = mount(EnterpriseUsageTrendChart, { props: { labels: ['周一'], values: [3], datasetLabel: '用量' } })
    const ticks = () => (wrapper.getComponent({ name: 'Line' }).props('options') as { scales: { x: { ticks: { color: string } } } }).scales.x.ticks.color
    expect(ticks()).toBe('#374151')
    document.documentElement.classList.add('dark')
    await new Promise((resolve) => setTimeout(resolve, 0))
    await nextTick()
    expect(ticks()).toBe('#e5e7eb')
    document.documentElement.classList.remove('dark')
    await new Promise((resolve) => setTimeout(resolve, 0))
    await nextTick()
    expect(ticks()).toBe('#374151')
    wrapper.unmount()
  })
})
