import { apiClient } from '../client'

export interface AccountTimezoneState {
  timezone: string
  override: string
  detected_timezone: string
  ip: string
  detected_at?: string
  source: 'none' | 'proxy' | 'manual'
  stale: boolean
  has_proxy: boolean
}

export async function getAccountTimezone(id: number): Promise<AccountTimezoneState> {
  const { data } = await apiClient.get<AccountTimezoneState>(`/admin/accounts/${id}/timezone`)
  return data
}

export async function detectAccountTimezone(id: number, refresh = false): Promise<AccountTimezoneState> {
  const { data } = await apiClient.post<AccountTimezoneState>(`/admin/accounts/${id}/timezone/detect`, { refresh })
  return data
}

export async function setAccountTimezone(id: number, override: string): Promise<AccountTimezoneState> {
  const { data } = await apiClient.put<AccountTimezoneState>(`/admin/accounts/${id}/timezone`, { override })
  return data
}
