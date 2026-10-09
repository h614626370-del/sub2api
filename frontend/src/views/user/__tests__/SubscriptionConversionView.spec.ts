import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { ref } from 'vue'
import UserView from '../SubscriptionConversionView.vue'
import AdminView from '@/views/admin/SubscriptionConversionView.vue'
import { subscriptionConversionAPI as api, type ConversionOverview } from '@/api/subscriptionConversion'

vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<main><slot /></main>' } }))
vi.mock('@/components/common/BaseDialog.vue', () => ({ default: { props: ['show', 'title'], template: '<section v-if="show" role="dialog"><h2>{{ title }}</h2><slot /><slot name="footer" /></section>' } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ locale: ref('en'), t: (key: string, args?: Record<string, unknown>) => args ? `${key} ${JSON.stringify(args)}` : key }) }))
vi.mock('@/api/subscriptionConversion', () => ({ subscriptionConversionAPI: { settings: vi.fn(), save: vi.fn(), overview: vi.fn(), convert: vi.fn() } }))
const refresh = vi.hoisted(() => vi.fn().mockResolvedValue({}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ refreshUser: refresh }) }))
vi.mock('@/stores/subscriptions', () => ({ useSubscriptionStore: () => ({ fetchActiveSubscriptions: refresh }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ fetchPublicSettings: refresh }) }))
const fixture = (): ConversionOverview => ({ enabled: true, history: [], subscriptions: [{ subscription_id: 1, group_id: 2, group_name: 'Monthly', starts_at: '2026-10-01', expires_at: '2026-10-31', total_seconds: 2592000, remaining_seconds: 1296000, price_usd: '30', monthly_quota_usd: '100', total_quota_usd: '100', used_quota_usd: '20', remaining_quota_usd: '80', time_ratio: '0.5', quota_ratio: '0.8', amount_usd: '19.5', order_ids: [3], eligible: true, reason: '', quote: 'abc' }] })
beforeEach(() => { vi.clearAllMocks(); vi.mocked(api.overview).mockResolvedValue(fixture()); vi.mocked(api.settings).mockResolvedValue({ enabled: false }); vi.mocked(api.save).mockImplementation(async v => v) })
describe('Subscription conversion', () => {
  it('shows formula, breakdown, and estimated amount', async () => {
    const w = mount(UserView); await flushPromises()
    expect(w.text()).toContain('$19.50'); expect(w.text()).toContain('50.00% × 50% + 80.00% × 50%'); expect(w.text()).toContain('subscriptionConversion.formula')
    w.unmount()
  })
  it('requires explicit acknowledgment and suppresses duplicate submissions', async () => {
    let finish!: (r: Awaited<ReturnType<typeof api.convert>>) => void
    vi.mocked(api.convert).mockImplementation(() => new Promise(resolve => { finish = resolve }))
    const w = mount(UserView); await flushPromises(); await w.get('[data-testid="convert"]').trigger('click')
    expect(w.get('[data-testid="confirm"]').attributes('disabled')).toBeDefined()
    await w.get('[data-testid="acknowledge"]').setValue(true); await w.get('[data-testid="confirm"]').trigger('click'); await w.get('[data-testid="confirm"]').trigger('click')
    expect(api.convert).toHaveBeenCalledTimes(1); expect(api.convert).toHaveBeenCalledWith(1, 'abc')
    vi.mocked(api.overview).mockResolvedValue({ enabled: true, subscriptions: [], history: [] })
    finish({ subscription_id: 1, group_id: 2, group_name: 'Monthly', amount_usd: '19.5', calculation: fixture().subscriptions[0], converted_at: '2026-10-09' }); await flushPromises()
    expect(w.find('[role="dialog"]').exists()).toBe(false); expect(w.get('[role="status"]').text()).toContain('subscriptionConversion.success'); expect(refresh).toHaveBeenCalled()
    w.unmount()
  })
  it.each(['not_purchased', 'pending_requests', 'unsettled_usage'])('shows %s without a convert button', async (reason) => {
    const f = fixture(); f.subscriptions[0].eligible = false; f.subscriptions[0].reason = reason; vi.mocked(api.overview).mockResolvedValue(f)
    const w = mount(UserView); await flushPromises(); expect(w.find('[data-testid="convert"]').exists()).toBe(false); expect(w.text()).toContain(`reasons.${reason}`); w.unmount()
  })
  it('handles disabled and empty states', async () => {
    vi.mocked(api.overview).mockResolvedValue({ enabled: false, subscriptions: [], history: [] })
    const w = mount(UserView); await flushPromises(); expect(w.text()).toContain('subscriptionConversion.closed'); expect(w.find('[data-testid="convert"]').exists()).toBe(false); w.unmount()
  })
  it('keeps a rejected confirmation visible without claiming success', async () => {
    vi.mocked(api.convert).mockRejectedValue(new Error('Rule changed'))
    const w = mount(UserView); await flushPromises(); await w.get('[data-testid="convert"]').trigger('click'); await w.get('[data-testid="acknowledge"]').setValue(true); await w.get('[data-testid="confirm"]').trigger('click'); await flushPromises()
    expect(w.get('[role="dialog"] [role="alert"]').text()).toBe('Rule changed'); expect(w.find('[role="status"]').exists()).toBe(false); w.unmount()
  })
  it('loads disabled configuration and saves only the visibility switch', async () => {
    const w = mount(AdminView); await flushPromises(); expect((w.get('input[type="checkbox"]').element as HTMLInputElement).checked).toBe(false)
    expect(w.find('input[type="number"]').exists()).toBe(false)
    await w.get('input[type="checkbox"]').setValue(true); await w.get('form').trigger('submit'); await flushPromises()
    expect(api.save).toHaveBeenCalledWith({ enabled: true }); expect(w.get('[role="status"]').text()).toContain('subscriptionConversion.saved'); w.unmount()
  })
  it('shows load errors and allows retry without exposing a save form', async () => {
    vi.mocked(api.settings).mockRejectedValue(new Error('Unavailable'))
    const w = mount(AdminView); await flushPromises(); expect(w.get('[role="alert"]').text()).toBe('Unavailable'); expect(w.find('form').exists()).toBe(false); w.unmount()
  })
})
