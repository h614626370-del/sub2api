import { beforeEach, describe, expect, it, vi } from 'vitest'
import { apiClient } from '../client'
import { subscriptionConversionAPI } from '../subscriptionConversion'

vi.mock('../client', () => ({ apiClient: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))

describe('Admin subscription conversion endpoints', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(apiClient.get).mockResolvedValue({ data: {} })
    vi.mocked(apiClient.post).mockResolvedValue({ data: {} })
    vi.mocked(apiClient.put).mockResolvedValue({ data: {} })
  })

  it('uses admin-only routes with an explicit target account', async () => {
    await subscriptionConversionAPI.settings()
    await subscriptionConversionAPI.save({ enabled: true })
    await subscriptionConversionAPI.overview(42)
    await subscriptionConversionAPI.convert(42, 9, 'quote')
    expect(apiClient.get).toHaveBeenCalledWith('/admin/subscription-conversion')
    expect(apiClient.put).toHaveBeenCalledWith('/admin/subscription-conversion', { enabled: true })
    expect(apiClient.get).toHaveBeenCalledWith('/admin/subscription-conversion/users/42')
    expect(apiClient.post).toHaveBeenCalledWith('/admin/subscription-conversion/users/42/subscriptions/9/convert', { quote: 'quote' })
  })
})
