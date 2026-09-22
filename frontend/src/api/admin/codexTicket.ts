import { apiClient } from '../client'
import type { PaginatedResponse } from '@/types'

export interface CodexTicketAudit {
  id: number
  account_id: number
  model: string
  started_at: string
  finished_at: string
  duration_ms: number
  outcome: string
  reason: string
  http_status?: number | null
  ticket_length: number
  attempts: number
  ticket_expires_at?: string | null
  egress_ip?: string
  request_body?: string
  response_headers?: Record<string, string>
  ticket_hash?: string
  created_at: string
}

export interface DiscardCodexTicketsResult {
  cleared_accounts: number
  cleared_tickets: number
}

export async function listAudits(params: { page?: number; page_size?: number; account_id?: number; model?: string; outcome?: string; to?: string } = {}): Promise<PaginatedResponse<CodexTicketAudit>> {
  const { data } = await apiClient.get('/admin/codex-ticket/audits', { params })
  return data
}

export async function getAudit(id: number): Promise<CodexTicketAudit> {
  const { data } = await apiClient.get(`/admin/codex-ticket/audits/${id}`)
  return data
}

export async function clearAudits(): Promise<{ cleared: number }> {
  const { data } = await apiClient.post('/admin/codex-ticket/audits/clear')
  return data
}

export async function discard(accountId?: number): Promise<DiscardCodexTicketsResult> {
  const { data } = await apiClient.post('/admin/codex-ticket/discard', accountId === undefined ? {} : { account_id: accountId })
  return data
}

export interface CodexTicketStatistics {
  account_id: number
  model: string
  total_attempts: number
  successes: number
  total_duration_ms: number
  pending_attempts: number
  pending_since?: string
  last_success_at?: string
  last_success_duration_ms: number
  last_success_ip: string
  last_success_ip_source: string
}

export async function statistics(): Promise<CodexTicketStatistics[]> {
  const { data } = await apiClient.get('/admin/codex-ticket/statistics')
  return data
}

export const codexTicketAPI = { listAudits, getAudit, clearAudits, discard, statistics }
export default codexTicketAPI
