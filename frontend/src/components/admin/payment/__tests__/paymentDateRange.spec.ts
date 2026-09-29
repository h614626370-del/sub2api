import { describe, expect, it } from 'vitest'
import { monthlyPaymentSeries, paymentMonthRange, paymentPresetRange, paymentRangeDays, paymentRangeError } from '../paymentDateRange'

describe('payment calendar ranges', () => {
  it('resolves a historical leap month and a cross-year span', () => {
    expect(paymentMonthRange('2024-02', '2024-02', '2026-09-29')).toEqual({ start_date: '2024-02-01', end_date: '2024-02-29' })
    expect(paymentMonthRange('2025-12', '2026-02', '2026-09-29')).toEqual({ start_date: '2025-12-01', end_date: '2026-02-28' })
  })
  it('clamps the current month to the reporting timezone today', () => {
    expect(paymentMonthRange('2026-09', '2026-09', '2026-09-29')).toEqual({ start_date: '2026-09-01', end_date: '2026-09-29' })
  })
  it.each([
    ['last7', '2026-09-23', '2026-09-29'], ['last30', '2026-08-31', '2026-09-29'],
    ['last90', '2026-07-02', '2026-09-29'], ['thisMonth', '2026-09-01', '2026-09-29'],
    ['lastMonth', '2026-08-01', '2026-08-31'], ['last6Months', '2026-04-01', '2026-09-29'],
    ['thisYear', '2026-01-01', '2026-09-29'], ['lastYear', '2025-01-01', '2025-12-31'],
  ] as const)('resolves %s', (preset, start_date, end_date) => {
    expect(paymentPresetRange(preset, '2026-09-29')).toEqual({ start_date, end_date })
  })
  it('handles last month in January and month-end without overflow', () => {
    expect(paymentPresetRange('lastMonth', '2026-01-31')).toEqual({ start_date: '2025-12-01', end_date: '2025-12-31' })
    expect(paymentPresetRange('last6Months', '2026-03-31')).toEqual({ start_date: '2025-10-01', end_date: '2026-03-31' })
  })
  it('counts calendar days independently of daylight saving transitions', () => {
    expect(paymentRangeDays({ start_date: '2026-03-07', end_date: '2026-03-09' })).toBe(3)
    expect(paymentRangeDays({ start_date: '2024-01-01', end_date: '2024-12-31' })).toBe(366)
  })
  it.each([
    ['', '', 'incomplete'], ['2026-02-30', '2026-03-01', 'incomplete'],
    ['2026-03-10', '2026-03-01', 'reversed'], ['2026-09-01', '2026-09-30', 'future'],
    ['2024-01-01', '2025-01-01', 'tooLong'], ['2024-01-01', '2024-12-31', null],
  ])('validates %s to %s', (start_date, end_date, error) => {
    expect(paymentRangeError({ start_date, end_date }, '2026-09-29')).toBe(error)
  })
  it('preserves currency totals and empty months in monthly charts', () => {
    const source = [
      { date: '2025-12-01', amount: { USD: 0.1, CNY: 8 }, count: 2 },
      { date: '2025-12-31', amount: { USD: 0.2 }, count: 1 },
      { date: '2026-01-01', amount: {}, count: 0 },
    ]
    expect(monthlyPaymentSeries(source)).toEqual([
      { date: '2025-12', amount: { USD: 0.3, CNY: 8 }, count: 3 },
      { date: '2026-01', amount: {}, count: 0 },
    ])
    expect(source[0].amount.USD).toBe(0.1)
  })
})
