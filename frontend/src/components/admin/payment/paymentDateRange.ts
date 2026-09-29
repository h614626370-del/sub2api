import type { DailyPaymentStats } from '@/types/payment'

export const MAX_PAYMENT_RANGE_DAYS = 366
export const PAYMENT_PRESETS = ['last7', 'last30', 'last90', 'thisMonth', 'lastMonth', 'last6Months', 'thisYear', 'lastYear'] as const
export type PaymentPreset = typeof PAYMENT_PRESETS[number]
export interface PaymentDateRange { start_date: string; end_date: string }

// Calendar-only arithmetic deliberately uses UTC to avoid browser DST shifts.
function parseDate(value: string): Date | null {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return null
  const date = new Date(`${value}T00:00:00Z`)
  return Number.isFinite(date.getTime()) && date.toISOString().slice(0, 10) === value ? date : null
}

function formatDate(date: Date): string { return date.toISOString().slice(0, 10) }

export function paymentRangeDays(range: PaymentDateRange): number {
  const start = parseDate(range.start_date)
  const end = parseDate(range.end_date)
  return start && end ? Math.round((end.getTime() - start.getTime()) / 86400000) + 1 : 0
}

export function paymentRangeError(range: PaymentDateRange, today: string): string | null {
  if (!parseDate(range.start_date) || !parseDate(range.end_date)) return 'incomplete'
  if (range.start_date > range.end_date) return 'reversed'
  if (range.end_date > today) return 'future'
  if (paymentRangeDays(range) > MAX_PAYMENT_RANGE_DAYS) return 'tooLong'
  return null
}

export function paymentMonthRange(start: string, end: string, today: string): PaymentDateRange {
  const startDate = parseDate(`${start}-01`)
  const endDate = parseDate(`${end}-01`)
  if (!startDate || !endDate) return { start_date: '', end_date: '' }
  endDate.setUTCMonth(endDate.getUTCMonth() + 1, 0)
  const lastDay = formatDate(endDate)
  return { start_date: `${start}-01`, end_date: lastDay > today ? today : lastDay }
}

export function paymentPresetRange(preset: PaymentPreset, today: string): PaymentDateRange {
  const end = parseDate(today)
  if (!end) return { start_date: '', end_date: '' }
  const start = new Date(end)
  switch (preset) {
    case 'last7': start.setUTCDate(start.getUTCDate() - 6); break
    case 'last30': start.setUTCDate(start.getUTCDate() - 29); break
    case 'last90': start.setUTCDate(start.getUTCDate() - 89); break
    case 'thisMonth': start.setUTCDate(1); break
    case 'lastMonth':
      start.setUTCDate(1)
      start.setUTCMonth(start.getUTCMonth() - 1)
      end.setUTCDate(0)
      break
    case 'last6Months':
      start.setUTCDate(1)
      start.setUTCMonth(start.getUTCMonth() - 5)
      break
    case 'thisYear': start.setUTCMonth(0, 1); break
    case 'lastYear':
      start.setUTCFullYear(start.getUTCFullYear() - 1, 0, 1)
      end.setUTCMonth(0, 0)
      break
  }
  return { start_date: formatDate(start), end_date: formatDate(end) }
}

export function monthlyPaymentSeries(series: DailyPaymentStats[]): DailyPaymentStats[] {
  const months = new Map<string, DailyPaymentStats>()
  for (const day of series) {
    const key = day.date.slice(0, 7)
    let month = months.get(key)
    if (!month) {
      month = { date: key, amount: {}, count: 0 }
      months.set(key, month)
    }
    month.count += day.count
    for (const [currency, amount] of Object.entries(day.amount)) {
      month.amount[currency] = (month.amount[currency] || 0) + amount
    }
  }
  for (const month of months.values()) {
    for (const currency of Object.keys(month.amount)) month.amount[currency] = Number(month.amount[currency].toFixed(2))
  }
  return [...months.values()].sort((a, b) => a.date.localeCompare(b.date))
}
