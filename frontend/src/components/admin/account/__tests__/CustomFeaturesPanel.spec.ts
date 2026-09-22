import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import zhAccounts from '@/i18n/locales/zh/admin/accounts'
import enAccounts from '@/i18n/locales/en/admin/accounts'

import CustomFeaturesPanel from '../CustomFeaturesPanel.vue'

const { getSettings, updateSettings, getGroups, showError, showSuccess } = vi.hoisted(() => ({
  getSettings: vi.fn(),
  getGroups: vi.fn(),
  updateSettings: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    settings: {
      getSettings,
      updateSettings,
    },
    groups: {
      getAll: getGroups,
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showSuccess }),
}))

vi.mock('@/utils/apiError', () => ({
  extractApiErrorMessage: () => 'error',
}))

const ToggleStub = defineComponent({
  props: {
    modelValue: { type: Boolean, default: false },
  },
  emits: ['update:modelValue'],
  template: '<input id="custom-toggle-stub" type="checkbox" :checked="modelValue" @change="$emit(\'update:modelValue\', $event.target.checked)" />',
})

function mountPanel(locale = 'zh') {
  return mount(CustomFeaturesPanel, {
    global: {
      plugins: [createI18n({
        legacy: false,
        locale,
        messages: {
          zh: { admin: zhAccounts, common: { loading: '加载中', save: '保存', saving: '保存中', refresh: '刷新' } },
          en: { admin: enAccounts, common: { loading: 'Loading', save: 'Save', saving: 'Saving', refresh: 'Refresh' } },
        },
      })],
      stubs: {
        AccountTimezonePanel: true,
        Toggle: ToggleStub,
        Icon: true,
      },
    },
  })
}

describe('CustomFeaturesPanel', () => {
  beforeEach(() => {
    getGroups.mockReset().mockResolvedValue([])
    getSettings.mockReset()
    updateSettings.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
  })

  it.each(['zh', 'en'])('renders the proxy placeholder with real %s translations', async (locale) => {
    const compilationErrors = vi.spyOn(console, 'error').mockImplementation(() => {})
    getSettings.mockResolvedValue({})
    const wrapper = mountPanel(locale)
    await flushPromises()
    expect(wrapper.get('#custom-codex-ticket-proxy').attributes('placeholder'))
      .toBe('http://user:pass@proxy.example.com:1080')
    expect(wrapper.find('#custom-astra-group').exists()).toBe(true)
    wrapper.unmount()
    const errors = compilationErrors.mock.calls.slice()
    compilationErrors.mockRestore()
    expect(errors).toEqual([])
  })

  it('saves timing and cookie policy and disables strict mode with cookie capture', async () => {
    getSettings.mockResolvedValue({})
    updateSettings.mockImplementation(async payload => payload)
    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.get('#ticket-ttl_seconds').setValue(240)
    await wrapper.get('#ticket-refresh_before_seconds').setValue(30)
    await wrapper.get('#ticket-cookie-enabled').setValue(true)
    await wrapper.get('#ticket-cookie-required').setValue(true)
    await wrapper.get('#ticket-cookie-ttl').setValue(180)
    await wrapper.get('#ticket-models').setValue('gpt-6-astra')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()
    expect(updateSettings).toHaveBeenLastCalledWith(expect.objectContaining({
      openai_codex_ticket_policy: {
        ttl_seconds: 240, refresh_before_seconds: 30, probe_interval_seconds: 6,
        attempt_timeout_seconds: 25, target_length: 292, models: ['gpt-6-astra'],
        fail_closed: true, cookie_enabled: true, cookie_required: true, cookie_ttl_seconds: 180,
        reuse_connection: false,
        connection_max_age_seconds: 300,
      },
    }))
    await wrapper.get('#ticket-cookie-enabled').setValue(false)
    expect(wrapper.find('#ticket-cookie-required').exists()).toBe(false)
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()
    expect(updateSettings).toHaveBeenLastCalledWith(expect.objectContaining({
      openai_codex_ticket_policy: expect.objectContaining({ cookie_enabled: false, cookie_required: false }),
    }))
  })

  it('requires cookie capture for connection reuse and saves the experimental switch', async () => {
    getSettings.mockResolvedValue({})
    updateSettings.mockImplementation(async payload => payload)
    const wrapper = mountPanel()
    await flushPromises()
    expect(wrapper.get('#ticket-reuse-connection').attributes('disabled')).toBeDefined()
    await wrapper.get('#ticket-cookie-enabled').setValue(true)
    await wrapper.get('#ticket-reuse-connection').setValue(true)
    await wrapper.get('#ticket-connection-max-age').setValue(600)
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()
    expect(updateSettings).toHaveBeenLastCalledWith(expect.objectContaining({
      openai_codex_ticket_policy: expect.objectContaining({ reuse_connection: true, cookie_enabled: true, connection_max_age_seconds: 600 }),
    }))
    await wrapper.get('#ticket-cookie-enabled').setValue(false)
    expect((wrapper.get('#ticket-reuse-connection').element as HTMLInputElement).checked).toBe(false)
    wrapper.unmount()
  })

  it('rejects invalid refresh times without sending settings', async () => {
    getSettings.mockResolvedValue({})
    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.get('#ticket-ttl_seconds').setValue(240)
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()
    expect(updateSettings).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledTimes(1)
  })

  it('saves a special OpenAI pool and can disable routing', async () => {
    getSettings.mockResolvedValue({ openai_astra_group_id: 20 })
    getGroups.mockResolvedValue([
      { id: 20, name: 'Astra pool', platform: 'openai', subscription_type: 'special' },
      { id: 10, name: 'Ordinary', platform: 'openai', subscription_type: 'standard' },
      { id: 30, name: 'Other platform', platform: 'anthropic', subscription_type: 'special' },
    ])
    updateSettings.mockImplementation(async payload => payload)
    const wrapper = mountPanel()
    await flushPromises()
    const select = wrapper.get('#custom-astra-group')
    expect(select.element.value).toBe('20')
    expect(select.text()).toContain('Astra pool')
    expect(select.text()).not.toContain('Ordinary')
    expect(select.text()).not.toContain('Other platform')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()
    expect(updateSettings).toHaveBeenLastCalledWith({ openai_astra_group_id: 20, openai_codex_ticket_enabled: false })
    await select.setValue('0')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()
    expect(updateSettings).toHaveBeenLastCalledWith({ openai_astra_group_id: 0, openai_codex_ticket_enabled: false })
  })

  it('keeps an unavailable configured pool visible instead of silently resetting it', async () => {
    getSettings.mockResolvedValue({ openai_astra_group_id: 20 })
    const wrapper = mountPanel()
    await flushPromises()
    expect(wrapper.get('#custom-astra-group').element.value).toBe('20')
    expect(wrapper.get('#custom-astra-group option[value="20"]').attributes()).toHaveProperty('disabled')
  })

  it('prevents a partial load failure from saving default settings', async () => {
    getSettings.mockResolvedValue({ openai_astra_group_id: 20 })
    getGroups.mockRejectedValue(new Error('groups unavailable'))
    const wrapper = mountPanel()
    await flushPromises()
    expect(wrapper.get('button[type="submit"]').attributes()).toHaveProperty('disabled')
    await wrapper.get('form').trigger('submit.prevent')
    expect(updateSettings).not.toHaveBeenCalled()
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
  })

  it('loads the Codex ticket settings in the account-management custom tab', async () => {
    getSettings.mockResolvedValue({
      openai_codex_ticket_enabled: true,
      openai_codex_ticket_harvest_proxy_url: 'http://user:***@proxy.example.com:1080',
      openai_codex_ticket_harvest_proxy_configured: true,
    })

    const wrapper = mountPanel()
    await flushPromises()

    expect(getSettings).toHaveBeenCalledTimes(1)
    expect(wrapper.get('#custom-codex-ticket-enabled').element.checked).toBe(true)
    expect(wrapper.get('#custom-codex-ticket-proxy').element.value)
      .toBe('http://user:***@proxy.example.com:1080')
    expect(wrapper.find('#custom-codex-ticket-proxy').exists()).toBe(true)
  })

  it('saves only the custom Codex ticket fields', async () => {
    getSettings.mockResolvedValue({
      openai_codex_ticket_enabled: false,
      openai_codex_ticket_harvest_proxy_url: '',
    })
    updateSettings.mockResolvedValue({
      openai_codex_ticket_enabled: true,
      openai_codex_ticket_harvest_proxy_url: 'socks5h://user:new-secret@proxy.example.com:1080',
      openai_codex_ticket_harvest_proxy_configured: true,
    })

    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.get('#custom-codex-ticket-enabled').setValue(true)
    await wrapper.get('#custom-codex-ticket-proxy').setValue('  socks5h://user:new-secret@proxy.example.com:1080  ')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()

    expect(updateSettings).toHaveBeenCalledWith({
      openai_astra_group_id: 0,
      openai_codex_ticket_enabled: true,
      openai_codex_ticket_harvest_proxy_url: 'socks5h://user:new-secret@proxy.example.com:1080',
    })
    expect(showSuccess).toHaveBeenCalledTimes(1)
  })

  it('does not resubmit the masked proxy password when the field is untouched', async () => {
    getSettings.mockResolvedValue({
      openai_codex_ticket_enabled: true,
      openai_codex_ticket_harvest_proxy_url: 'http://user:***@proxy.example.com:1080',
      openai_codex_ticket_harvest_proxy_configured: true,
    })
    updateSettings.mockResolvedValue({
      openai_codex_ticket_enabled: true,
      openai_codex_ticket_harvest_proxy_url: 'http://user:***@proxy.example.com:1080',
      openai_codex_ticket_harvest_proxy_configured: true,
    })

    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()

    expect(updateSettings).toHaveBeenCalledWith({ openai_astra_group_id: 0, openai_codex_ticket_enabled: true })
  })
})
