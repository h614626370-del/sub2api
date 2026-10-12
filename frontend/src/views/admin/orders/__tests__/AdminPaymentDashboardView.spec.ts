import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AdminPaymentDashboardView from '../AdminPaymentDashboardView.vue'
import PaymentDateFilter from '@/components/admin/payment/PaymentDateFilter.vue'

const { getDashboard, showError } = vi.hoisted(() => ({ getDashboard: vi.fn(), showError: vi.fn() }))
vi.mock('@/api/admin/payment', () => ({ adminPaymentAPI: { getDashboard }, default: { getDashboard } }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<main><slot /></main>' } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError }) }))
vi.mock('vue-i18n', async importOriginal => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))
const stats = (start = '2026-08-31', end = '2026-09-29') => ({
  start_date: start, end_date: end, today: '2026-09-29', timezone: 'America/Los_Angeles',
  today_amount: {}, total_amount: {}, avg_amount: {}, today_count: 0, total_count: 0,
  daily_series: [], payment_methods: [], top_users: {},
})
function setup() {
  return mount(AdminPaymentDashboardView, { global: { stubs: {
    AppLayout: { template: '<main><slot /></main>' }, PaymentDateFilter: true,
    OrderStatsCards: true, DailyRevenueChart: true, LoadingSpinner: true,
  } } })
}

describe('payment dashboard requests', () => {
  beforeEach(() => { getDashboard.mockReset(); showError.mockReset() })
  it('refreshes the selected historical range instead of reverting to thirty days', async () => {
    getDashboard.mockResolvedValue({ data: stats() })
    const wrapper = setup()
    await flushPromises()
    expect(getDashboard).toHaveBeenLastCalledWith({ days: 30 })
    const query = { start_date: '2024-02-01', end_date: '2024-02-29' }
    wrapper.findComponent(PaymentDateFilter).vm.$emit('change', query)
    await flushPromises()
    wrapper.findComponent(PaymentDateFilter).vm.$emit('refresh')
    await flushPromises()
    expect(getDashboard).toHaveBeenLastCalledWith(query)
    wrapper.unmount()
  })
  it('ignores a slow previous request after switching periods', async () => {
    let finishOld!: (value: unknown) => void
    getDashboard.mockImplementationOnce(() => new Promise(resolve => { finishOld = resolve }))
      .mockResolvedValueOnce({ data: stats('2024-02-01', '2024-02-29') })
    const wrapper = setup()
    wrapper.findComponent(PaymentDateFilter).vm.$emit('change', { start_date: '2024-02-01', end_date: '2024-02-29' })
    await flushPromises()
    finishOld({ data: stats() })
    await flushPromises()
    expect(wrapper.findComponent(PaymentDateFilter).props('appliedStart')).toBe('2024-02-01')
    expect(wrapper.findComponent(PaymentDateFilter).props('loading')).toBe(false)
    wrapper.unmount()
  })
  it('shows an error and retries without presenting old statistics as new results', async () => {
    getDashboard.mockResolvedValueOnce({ data: stats() }).mockRejectedValueOnce(new Error('Connection failed')).mockResolvedValueOnce({ data: stats('2025-01-01', '2025-12-31') })
    const wrapper = setup()
    await flushPromises()
    const query = { start_date: '2025-01-01', end_date: '2025-12-31' }
    wrapper.findComponent(PaymentDateFilter).vm.$emit('change', query)
    await flushPromises()
    expect(wrapper.get('[role=alert]').text()).toContain('Connection failed')
    expect(wrapper.find('order-stats-cards-stub').exists()).toBe(false)
    await wrapper.get('[role=alert] button').trigger('click')
    await flushPromises()
    expect(getDashboard).toHaveBeenLastCalledWith(query)
    expect(wrapper.find('[role=alert]').exists()).toBe(false)
    wrapper.unmount()
  })
})
