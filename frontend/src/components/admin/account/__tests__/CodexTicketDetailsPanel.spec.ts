import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import CodexTicketDetailsPanel from '../CodexTicketDetailsPanel.vue'
import Pagination from '@/components/common/Pagination.vue'

const { getSettings, listAccounts, listAudits, getAudit, statistics, showError } = vi.hoisted(() => ({
  getSettings: vi.fn(),
  listAccounts: vi.fn(),
  listAudits: vi.fn(),
  getAudit: vi.fn(),
  statistics: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    settings: { getSettings },
    accounts: { list: listAccounts },
    codexTicket: { listAudits, getAudit, statistics },
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
    statistics.mockReset().mockResolvedValue([])
    showError.mockReset()
  })

  it('shows real connection metadata and hides failure and separately-probed IPs', async () => {
    getSettings.mockResolvedValue({ openai_codex_ticket_enabled: true })
    listAccounts.mockResolvedValue({ items: [], total: 0 })
    listAudits.mockResolvedValue({ items: [
      { id: 1, account_id: 1, model: 'gpt-6-astra', outcome: 'validated', egress_ip: '203.0.113.22',
        response_headers: {
          'x-sub2api-connection-session': 'abcdef12-session',
          'x-sub2api-connection-reused': 'true',
          'x-sub2api-connection-retained': 'true',
          'x-sub2api-connection-age-seconds': '42',
          'x-sub2api-connection-successes': '3',
          'x-sub2api-egress-ip-source': 'same_connection_trace',
        } },
      { id: 2, account_id: 1, model: 'gpt-6-astra', outcome: 'invalid_ticket', egress_ip: '203.0.113.23' },
      { id: 3, account_id: 1, model: 'gpt-6-astra', outcome: 'success', egress_ip: '203.0.113.24',
        response_headers: { 'x-sub2api-egress-ip-source': 'separate_probe' } },
    ], total: 3 })
    const wrapper = mount(CodexTicketDetailsPanel, { global: { stubs: { Icon: true } } })
    await flushPromises()
    expect(wrapper.text()).toContain('abcdef12')
    expect(wrapper.text()).toContain('42s')
    expect(wrapper.text()).toContain('ticketDetails.connectionReused')
    expect(wrapper.text()).toContain('ticketDetails.validatedTicket')
    expect(wrapper.text()).toContain('203.0.113.22')
    expect(wrapper.text()).not.toContain('203.0.113.23')
    expect(wrapper.text()).not.toContain('203.0.113.24')
    expect(wrapper.text()).toContain('ticketDetails.ipSources.probe')
    wrapper.unmount()
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
  it('paginates audits on the server and closes expanded details on page changes', async () => {
    getSettings.mockResolvedValue({ openai_codex_ticket_enabled: true })
    listAccounts.mockResolvedValue({ items: [], total: 0 })
    listAudits.mockResolvedValue({ items: [{ id: 1, account_id: 1, model: 'gpt-6-astra' }], total: 25 })
    getAudit.mockResolvedValue({ id: 1, request_body: 'page-one-detail' })
    const wrapper = mount(CodexTicketDetailsPanel, { global: { stubs: { Icon: true } } })
    await flushPromises()
    expect(listAudits).toHaveBeenLastCalledWith({ page: 1, page_size: 10, to: expect.any(String) })
    const snapshot = listAudits.mock.lastCall?.[0].to
    await wrapper.findAll('button').find(b => b.text().includes('ticketDetails.expand'))?.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('page-one-detail')
    wrapper.findComponent(Pagination).vm.$emit('update:page', 2)
    await flushPromises()
    expect(listAudits).toHaveBeenLastCalledWith({ page: 2, page_size: 10, to: snapshot })
    expect(wrapper.text()).not.toContain('page-one-detail')
    wrapper.findComponent(Pagination).vm.$emit('update:pageSize', 20)
    await flushPromises()
    expect(listAudits).toHaveBeenLastCalledWith({ page: 1, page_size: 20, to: snapshot })
  })

})
