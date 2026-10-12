import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { ref } from 'vue'
import AdminView from '../SubscriptionConversionView.vue'
import { subscriptionConversionAPI as api, type ConversionOverview } from '@/api/subscriptionConversion'
import * as usersAPI from '@/api/admin/users'
import type { AdminUser } from '@/types'

vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<main><slot /></main>' } }))
vi.mock('@/components/common/BaseDialog.vue', () => ({ default: { props: ['show', 'title'], template: '<section v-if="show" role="dialog"><h2>{{ title }}</h2><slot /><slot name="footer" /></section>' } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ locale: ref('en'), t: (key: string, args?: Record<string, unknown>) => args ? `${key} ${JSON.stringify(args)}` : key }) }))
vi.mock('@/api/subscriptionConversion', () => ({ subscriptionConversionAPI: { settings: vi.fn(), save: vi.fn(), overview: vi.fn(), convert: vi.fn() } }))
vi.mock('@/api/admin/users', () => ({ list: vi.fn(), getById: vi.fn() }))

const user = { id: 42, username: 'Buyer', email: 'buyer@example.test', balance: 10 } as AdminUser
const otherUser = { ...user, id: 43, email: 'other@example.test' }
const fixture = (): ConversionOverview => ({
  enabled: true, history: [],
  subscriptions: [{
    subscription_id: 1, group_id: 2, group_name: 'Monthly', starts_at: '2026-10-01', expires_at: '2026-10-31',
    total_seconds: 2592000, remaining_seconds: 1296000, price_usd: '30', monthly_quota_usd: '100',
    total_quota_usd: '100', used_quota_usd: '20', remaining_quota_usd: '80', time_ratio: '0.5',
    quota_ratio: '0.8', amount_usd: '19.5', order_ids: [3], eligible: true, reason: '', quote: 'a'.repeat(64)
  }]
})

beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(api.overview).mockResolvedValue(fixture())
  vi.mocked(api.settings).mockResolvedValue({ enabled: true })
  vi.mocked(api.save).mockImplementation(async value => value)
  vi.mocked(usersAPI.list).mockResolvedValue({ items: [user, otherUser], total: 2, page: 1, page_size: 30, pages: 1 })
  vi.mocked(usersAPI.getById).mockImplementation(async id => id === user.id ? user : otherUser)
})

async function openUser() {
  const wrapper = mount(AdminView)
  await flushPromises()
  await wrapper.get('[data-testid="user-search"]').setValue('buyer')
  await wrapper.get('[data-testid="search-form"]').trigger('submit')
  await flushPromises()
  await wrapper.get('[data-testid="user-select"]').setValue('42')
  await flushPromises()
  return wrapper
}

describe('Admin subscription conversion', () => {
  it('combines configuration, user search, formula, and preview', async () => {
    const w = await openUser()
    expect(usersAPI.list).toHaveBeenCalledWith(1, 30, { search: 'buyer' })
    expect(api.overview).toHaveBeenCalledWith(42)
    expect(w.find('[data-testid="settings-form"]').exists()).toBe(true)
    expect(w.text()).toContain('$19.50')
    expect(w.text()).toContain('50.00% × 50% + 80.00% × 50%')
    expect(w.text()).toContain('subscriptionConversion.formula')
    w.unmount()
  })

  it('confirms the target account, blocks switching and duplicate submissions, then refreshes', async () => {
    let finish!: (receipt: Awaited<ReturnType<typeof api.convert>>) => void
    vi.mocked(api.convert).mockImplementation(() => new Promise(resolve => { finish = resolve }))
    const w = await openUser()
    await w.get('[data-testid="convert"]').trigger('click')
    expect(w.get('[data-testid="confirmation-user"]').text()).toContain('buyer@example.test')
    expect(w.get('[data-testid="user-select"]').attributes('disabled')).toBeDefined()
    expect(w.get('[data-testid="confirm"]').attributes('disabled')).toBeDefined()
    await w.get('[data-testid="acknowledge"]').setValue(true)
    await w.get('[data-testid="confirm"]').trigger('click')
    await w.get('[data-testid="confirm"]').trigger('click')
    expect(api.convert).toHaveBeenCalledTimes(1)
    expect(api.convert).toHaveBeenCalledWith(42, 1, 'a'.repeat(64))
    const receipt = { subscription_id: 1, group_id: 2, group_name: 'Monthly', amount_usd: '19.5', calculation: fixture().subscriptions[0], converted_at: '2026-10-12' }
    vi.mocked(api.overview).mockResolvedValue({ enabled: true, subscriptions: [], history: [receipt] })
    vi.mocked(usersAPI.getById).mockResolvedValue({ ...user, balance: 29.5 })
    finish(receipt)
    await flushPromises()
    expect(w.find('[role="dialog"]').exists()).toBe(false)
    expect(w.get('[role="status"]').text()).toContain('subscriptionConversion.success')
    expect(api.overview).toHaveBeenCalledTimes(2)
    expect(w.text()).toContain('$29.50')
    expect(w.text()).toContain('Monthly')
    w.unmount()
  })

  it.each(['not_purchased', 'pending_requests', 'unsettled_usage'])('shows %s without a convert button', async reason => {
    const f = fixture(); f.subscriptions[0].eligible = false; f.subscriptions[0].reason = reason
    vi.mocked(api.overview).mockResolvedValue(f)
    const w = await openUser()
    expect(w.find('[data-testid="convert"]').exists()).toBe(false)
    expect(w.text()).toContain(`reasons.${reason}`)
    w.unmount()
  })

  it('allows inspection while disabled but requires saving before conversion', async () => {
    vi.mocked(api.settings).mockResolvedValue({ enabled: false })
    vi.mocked(api.overview).mockResolvedValue({ ...fixture(), enabled: false })
    const w = await openUser()
    expect(w.text()).toContain('$19.50')
    expect(w.get('[data-testid="convert"]').attributes('disabled')).toBeDefined()
    await w.get('[data-testid="conversion-enabled"]').setValue(true)
    expect(w.get('[data-testid="convert"]').attributes('disabled')).toBeDefined()
    vi.mocked(api.overview).mockResolvedValue(fixture())
    await w.get('[data-testid="settings-form"]').trigger('submit')
    await flushPromises()
    expect(api.save).toHaveBeenCalledWith({ enabled: true })
    expect(w.get('[data-testid="convert"]').attributes('disabled')).toBeUndefined()
    w.unmount()
  })

  it('keeps a rejected confirmation visible without claiming success', async () => {
    vi.mocked(api.convert).mockRejectedValue(new Error('Rule changed'))
    const w = await openUser()
    await w.get('[data-testid="convert"]').trigger('click')
    await w.get('[data-testid="acknowledge"]').setValue(true)
    await w.get('[data-testid="confirm"]').trigger('click')
    await flushPromises()
    expect(w.get('[role="dialog"] [role="alert"]').text()).toBe('Rule changed')
    expect(w.find('[role="status"]').exists()).toBe(false)
    w.unmount()
  })

  it('clears the previous quote when switching users, including after a load failure', async () => {
    const w = await openUser()
    vi.mocked(api.overview).mockRejectedValue(new Error('Unavailable'))
    await w.get('[data-testid="user-select"]').setValue('43')
    await flushPromises()
    expect(api.overview).toHaveBeenLastCalledWith(43)
    expect(w.find('[data-testid="convert"]').exists()).toBe(false)
    expect(w.text()).not.toContain('$19.50')
    expect(w.get('[role="alert"]').text()).toBe('Unavailable')
    w.unmount()
  })

  it('shows configuration errors without exposing operation forms', async () => {
    vi.mocked(api.settings).mockRejectedValue(new Error('Unavailable'))
    const w = mount(AdminView)
    await flushPromises()
    expect(w.get('[role="alert"]').text()).toBe('Unavailable')
    expect(w.find('form').exists()).toBe(false)
    w.unmount()
  })

  it('reports no matches without loading or converting a user', async () => {
    vi.mocked(usersAPI.list).mockResolvedValue({ items: [], total: 0, page: 1, page_size: 30, pages: 0 })
    const w = mount(AdminView)
    await flushPromises()
    await w.get('[data-testid="user-search"]').setValue('missing')
    await w.get('[data-testid="search-form"]').trigger('submit')
    await flushPromises()
    expect(w.text()).toContain('subscriptionConversion.noUsers')
    expect(api.overview).not.toHaveBeenCalled()
    expect(api.convert).not.toHaveBeenCalled()
    w.unmount()
  })
})
