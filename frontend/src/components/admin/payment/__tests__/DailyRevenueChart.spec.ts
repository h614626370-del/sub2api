import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import DailyRevenueChart from '../DailyRevenueChart.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('vue-chartjs', () => ({
  Line: { name: 'Line', props: ['data', 'options'], template: '<div class="chart-stub" />' },
}))

const data = [
  { date: '2025-12-01', amount: { USD: 1, CNY: 8 }, count: 2 },
  { date: '2025-12-31', amount: { USD: 2 }, count: 1 },
  { date: '2026-01-01', amount: {}, count: 0 },
]

describe('payment revenue granularity', () => {
  it('switches to currency-preserving monthly sums and back to daily values', async () => {
    const wrapper = mount(DailyRevenueChart, { props: { data } })
    const chart = () => wrapper.findComponent({ name: 'Line' })
    expect(chart().props('data').labels).toEqual(data.map(day => day.date))
    await wrapper.findAll('button')[1].trigger('click')
    expect(chart().props('data').labels).toEqual(['2025-12', '2026-01'])
    expect(chart().props('data').datasets.map((set: { data: number[] }) => set.data))
      .toEqual([[8, 0], [3, 0], [3, 0]])
    await wrapper.findAll('button')[0].trigger('click')
    expect(chart().props('data').labels).toEqual(data.map(day => day.date))
  })

  it('uses monthly granularity for long ranges and compact daily ticks with full tooltip dates', () => {
    const wrapper = mount(DailyRevenueChart, { props: { data: Array(91).fill(data[0]) } })
    expect(wrapper.findAll('button')[1].attributes('aria-pressed')).toBe('true')
    const daily = mount(DailyRevenueChart, { props: { data } })
    const options = daily.findComponent({ name: 'Line' }).props('options')
    expect(options.scales.x.ticks.callback(0)).toBe('12-01')
    expect(options.plugins.tooltip.callbacks.title([{ dataIndex: 0 }])).toBe('2025-12-01')
  })
})
