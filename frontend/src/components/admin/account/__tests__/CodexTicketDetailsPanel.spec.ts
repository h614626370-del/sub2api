import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import CodexTicketDetailsPanel from '../CodexTicketDetailsPanel.vue'

const { getSettings, listAccounts, listAudits, getAudit, showError } = vi.hoisted(() => ({
  getSettings: vi.fn(),
  listAccounts: vi.fn(),
  listAudits: vi.fn(),
  getAudit: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    settings: { getSettings },
    accounts: { list: listAccounts },
    codexTicket: { listAudits, getAudit },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError }),
}))

vi.mock('@/utils/apiError', () => ({
  extractApiErrorMessage: () => 'error',
}))

vi.mock('@/utils/format', () => ({
  formatDateTime: (value: string | null) => value || '-',
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

describe('CodexTicketDetailsPanel', () => {
  beforeEach(() => {
    getSettings.mockReset()
    listAccounts.mockReset()
    listAudits.mockReset()
    getAudit.mockReset()
    showError.mockReset()
  })

  it('loads and filters per-account, per-model ticket details', async () => {
    getSettings.mockResolvedValue({
      openai_codex_ticket_enabled: true,
      openai_codex_ticket_harvest_proxy_configured: true,
    })
    listAccounts.mockResolvedValue({
      items: [
        {
          id: 1,
          name: 'primary',
          platform: 'openai',
          type: 'oauth',
          schedulable: true,
          group_ids: [10],
          codex_turn_tickets: [
            { model: 'gpt-6-astra', length: 292, ready: true, remaining_seconds: 3600, blocked: false, expires_at: '2026-09-21T12:00:00Z' },
            { model: 'gpt-6-mini', length: 0, ready: false, remaining_seconds: 0, blocked: true },
          ],
        },
        {
          id: 2,
          name: 'setup-token',
          platform: 'openai',
          type: 'setup-token',
          schedulable: false,
          group_ids: [],
          codex_turn_tickets: [
            { model: 'gpt-6-astra', length: 0, ready: false, remaining_seconds: 0, blocked: false },
          ],
        },
      ],
      total: 2,
      page: 1,
      page_size: 1000,
      pages: 1,
    })
    listAudits.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 50, pages: 0 })

    const wrapper = mount(CodexTicketDetailsPanel, {
      global: {
        stubs: { Icon: true },
      },
    })
    await flushPromises()

    expect(listAccounts).toHaveBeenCalledWith(1, 1000, expect.objectContaining({ platform: 'openai', lite: '1' }))
    expect(wrapper.findAll('tbody tr')).toHaveLength(3)
    expect(wrapper.text()).toContain('gpt-6-mini')

    await wrapper.get('input[type="search"]').setValue('gpt-6-mini')
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)

    await wrapper.get('select').setValue('blocked')
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)

    await wrapper.get('input[type="search"]').setValue('')
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
  })

  it('loads raw request and response bodies when an audit record is expanded', async () => {
    getSettings.mockResolvedValue({
      openai_codex_ticket_enabled: true,
      openai_codex_ticket_harvest_proxy_configured: true,
    })
    listAccounts.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 1000, pages: 1 })
    listAudits.mockResolvedValue({
      items: [{
        id: 42,
        account_id: 7,
        model: 'gpt-6-astra',
        created_at: '2026-09-21T10:00:00Z',
        duration_ms: 123,
        outcome: 'success',
        reason: 'ticket_captured',
        attempts: 1,
        egress_ip: '203.0.113.10',
      }],
      total: 1,
      page: 1,
      page_size: 50,
      pages: 1,
    })
    getAudit.mockResolvedValue({
      id: 42,
      request_body: '{"model":"gpt-6-astra"}',
      response_headers: {
        'Content-Type': 'text/event-stream',
        'x-codex-turn-state': 'gAAAAA-original-ticket-value',
      },
    })

    const wrapper = mount(CodexTicketDetailsPanel, { global: { stubs: { Icon: true } } })
    await flushPromises()

    await wrapper.findAll('button').find((button) => button.text().includes('admin.accounts.ticketDetails.expand'))?.trigger('click')
    await flushPromises()

    expect(getAudit).toHaveBeenCalledWith(42)
    expect(wrapper.text()).toContain('"model": "gpt-6-astra"')
    expect(wrapper.text()).toContain('Content-Type: text/event-stream')
    expect(wrapper.text()).toContain('x-codex-turn-state: gAAAAA-original-ticket-value')
  })
})
