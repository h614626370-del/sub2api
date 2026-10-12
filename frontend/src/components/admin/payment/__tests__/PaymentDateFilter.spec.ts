import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import PaymentDateFilter from '../PaymentDateFilter.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const prefix = 'payment.admin.dateFilter.'
function setup() {
  return mount(PaymentDateFilter, { props: { today: '2026-09-29', loading: false, appliedStart: '2026-08-31', appliedEnd: '2026-09-29', timezone: 'America/Los_Angeles' }, global: { stubs: { Icon: true } } })
}
type Wrapper = ReturnType<typeof setup>
async function click(wrapper: Wrapper, key: string) {
  await wrapper.findAll('button').find(button => button.text() === prefix + key)!.trigger('click')
}

describe('PaymentDateFilter', () => {
  it('applies a historical month only after confirmation', async () => {
    const wrapper = setup()
    await click(wrapper, 'months')
    await wrapper.get('#payment-range-start').setValue('2024-02')
    await wrapper.get('#payment-range-end').setValue('2024-02')
    expect(wrapper.emitted('change')).toBeUndefined()
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('change')?.[0]).toEqual([{ start_date: '2024-02-01', end_date: '2024-02-29' }])
  })
  it('supports consecutive months spanning more than ninety days', async () => {
    const wrapper = setup()
    await click(wrapper, 'months')
    await wrapper.get('#payment-range-start').setValue('2025-12')
    await wrapper.get('#payment-range-end').setValue('2026-04')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('change')?.[0]).toEqual([{ start_date: '2025-12-01', end_date: '2026-04-30' }])
  })
  it('rejects reversed, oversized and future ranges without querying', async () => {
    const wrapper = setup()
    await click(wrapper, 'custom')
    for (const [start, end, error] of [['2026-08-01', '2026-07-01', 'reversed'], ['2024-01-01', '2025-01-01', 'tooLong'], ['2026-09-01', '2026-09-30', 'future']]) {
      await wrapper.get('#payment-range-start').setValue(start)
      await wrapper.get('#payment-range-end').setValue(end)
      expect(wrapper.get('[role=alert]').text()).toBe(prefix + error)
      expect(wrapper.get('button[type=submit]').attributes('disabled')).toBeDefined()
      await wrapper.get('form').trigger('submit')
    }
    expect(wrapper.emitted('change')).toBeUndefined()
  })
  it('retains days queries and provides complete calendar-year shortcuts', async () => {
    const wrapper = setup()
    await click(wrapper, 'last7')
    await click(wrapper, 'lastYear')
    expect(wrapper.emitted('change')).toEqual([[{ days: 7 }], [{ start_date: '2025-01-01', end_date: '2025-12-31' }]])
  })
  it('uses server today when applying the current month', async () => {
    const wrapper = setup()
    await click(wrapper, 'months')
    await wrapper.get('#payment-range-start').setValue('2026-09')
    await wrapper.get('#payment-range-end').setValue('2026-09')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('change')?.[0]).toEqual([{ start_date: '2026-09-01', end_date: '2026-09-29' }])
  })
})
