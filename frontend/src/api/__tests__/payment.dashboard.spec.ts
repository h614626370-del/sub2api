import { describe, expect, it, vi } from 'vitest'
import { adminPaymentAPI } from '../admin/payment'
const { get } = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('../client', () => ({ apiClient: { get } }))

describe('payment dashboard query contract', () => {
  it('preserves legacy days and passes explicit inclusive dates unchanged', () => {
    adminPaymentAPI.getDashboard(90)
    expect(get).toHaveBeenLastCalledWith('/admin/payment/dashboard', { params: { days: 90 } })
    const range = { start_date: '2024-02-01', end_date: '2024-02-29' }
    adminPaymentAPI.getDashboard(range)
    expect(get).toHaveBeenLastCalledWith('/admin/payment/dashboard', { params: range })
  })
})
