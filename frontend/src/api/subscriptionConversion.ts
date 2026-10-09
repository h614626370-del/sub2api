import { apiClient } from './client'

export interface ConversionSettings { enabled: boolean }
export interface ConversionPreview {
  subscription_id: number
  group_id: number
  group_name: string
  starts_at: string
  expires_at: string
  total_seconds: number
  remaining_seconds: number
  price_usd: string
  monthly_quota_usd: string
  total_quota_usd: string
  used_quota_usd: string
  remaining_quota_usd: string
  time_ratio: string
  quota_ratio: string
  amount_usd: string
  order_ids: number[]
  eligible: boolean
  reason: string
  quote: string
}
export interface ConversionReceipt {
  subscription_id: number
  group_id: number
  group_name: string
  amount_usd: string
  calculation: ConversionPreview
  converted_at: string
}
export interface ConversionOverview {
  enabled: boolean
  subscriptions: ConversionPreview[]
  history: ConversionReceipt[]
}
export const subscriptionConversionAPI = {
  async settings() { return (await apiClient.get<ConversionSettings>('/admin/subscription-conversion')).data },
  async save(settings: ConversionSettings) { return (await apiClient.put<ConversionSettings>('/admin/subscription-conversion', settings)).data },
  async overview() { return (await apiClient.get<ConversionOverview>('/subscriptions/conversion')).data },
  async convert(id: number, quote: string) { return (await apiClient.post<ConversionReceipt>(`/subscriptions/${id}/convert-to-balance`, { quote })).data }
}
