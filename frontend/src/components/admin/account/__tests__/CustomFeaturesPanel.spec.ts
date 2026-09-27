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
      getAllIncludingInactive: getGroups,
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

function mountPanel(locale = 'zh', component = CustomFeaturesPanel) {
  return mount(component, {
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

describe('Account feature settings ownership', () => {
  beforeEach(() => {
    getGroups.mockReset().mockResolvedValue([])
    getSettings.mockReset()
    updateSettings.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
  })

  it('saves a special OpenAI pool and can disable routing', async () => {
    getSettings.mockResolvedValue({ openai_astra_group_id: 20, openai_sol_group_id: 20 })
    getGroups.mockResolvedValue([
      { id: 20, name: 'Astra pool', platform: 'openai', subscription_type: 'special', status: 'active' },
      { id: 10, name: 'Ordinary', platform: 'openai', subscription_type: 'standard' },
      { id: 30, name: 'Other platform', platform: 'anthropic', subscription_type: 'special' },
    ])
    updateSettings.mockImplementation(async payload => payload)
    const wrapper = mountPanel('zh', CustomFeaturesPanel)
    await flushPromises()
    const select = wrapper.get('#custom-astra-group')
    const solSelect = wrapper.get('#custom-sol-group')
    expect(select.element.value).toBe('20')
    expect(select.text()).toContain('Astra pool')
    expect(select.text()).not.toContain('Ordinary')
    expect(select.text()).not.toContain('Other platform')
    expect(solSelect.element.value).toBe('20')
    expect(wrapper.get('label[for="custom-astra-group"]').text()).toBe('目标分组')
    expect(wrapper.get('label[for="custom-sol-group"]').text()).toBe('目标分组')
    expect(wrapper.findAll('legend').map(legend => legend.text())).toEqual(['Astra 路由分组', 'Sol 路由分组'])
    expect(wrapper.get('#custom-astra-group option[value="0"]').text()).toBe('使用原分组')
    expect(wrapper.get('#custom-sol-group option[value="0"]').text()).toBe('使用原分组')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()
    expect(updateSettings).toHaveBeenLastCalledWith({ openai_astra_group_id: 20, openai_sol_group_id: 20, openai_astra_source_group_ids: [], openai_sol_source_group_ids: [] })
    await select.setValue('0')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()
    expect(updateSettings).toHaveBeenLastCalledWith({ openai_astra_group_id: 0, openai_sol_group_id: 20, openai_astra_source_group_ids: [], openai_sol_source_group_ids: [] })
    expect(wrapper.find('#custom-codex-ticket-enabled').exists()).toBe(false)
    expect(wrapper.find('#ticket-models').exists()).toBe(false)
    expect(wrapper.find('account-timezone-panel-stub').exists()).toBe(true)
  })

  it('keeps an unavailable configured pool visible instead of silently resetting it', async () => {
    getSettings.mockResolvedValue({ openai_astra_group_id: 20, openai_sol_group_id: 20 })
    const wrapper = mountPanel('zh', CustomFeaturesPanel)
    await flushPromises()
    expect(wrapper.get('#custom-astra-group').element.value).toBe('20')
    expect(wrapper.get('#custom-astra-group option[value="20"]').attributes()).toHaveProperty('disabled')
  })

  it.each(['zh', 'en'])('searches and selects eligible sources independently (%s)', async locale => {
    getSettings.mockResolvedValue({ openai_astra_group_id: 20, openai_sol_group_id: 20 })
    getGroups.mockResolvedValue([
      { id: 10, name: 'Ordinary', platform: 'openai', status: 'active', subscription_type: 'standard' },
      { id: 11, name: 'Composite', platform: 'composite', status: 'active', subscription_type: 'standard' },
      { id: 20, name: 'Special', platform: 'openai', status: 'active', subscription_type: 'special' },
      { id: 30, name: 'Other', platform: 'anthropic', status: 'active', subscription_type: 'standard' },
      { id: 40, name: 'Disabled', platform: 'openai', status: 'inactive', subscription_type: 'standard' },
    ])
    updateSettings.mockImplementation(async payload => payload)
    const wrapper = mountPanel(locale, CustomFeaturesPanel)
    await flushPromises()
    expect(wrapper.findAll('#custom-astra-source-20, #custom-astra-source-30, #custom-astra-source-40')).toHaveLength(0)
    await wrapper.get('#custom-astra-sources').setValue('composite')
    expect(wrapper.find('#custom-astra-source-10').exists()).toBe(false)
    expect(wrapper.find('#custom-astra-source-11').exists()).toBe(true)
    await wrapper.get('#custom-astra-select-all').trigger('click')
    await wrapper.get('#custom-sol-source-11').setValue(true)
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()
    expect(updateSettings).toHaveBeenLastCalledWith({
      openai_astra_group_id: 20, openai_sol_group_id: 20,
      openai_astra_source_group_ids: [10, 11], openai_sol_source_group_ids: [11],
    })
    await wrapper.get('#custom-astra-clear').trigger('click')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()
    expect(updateSettings).toHaveBeenLastCalledWith(expect.objectContaining({ openai_astra_source_group_ids: [], openai_sol_source_group_ids: [11] }))
    wrapper.unmount()
  })

  it('preserves invalid selections and unsaved edits after a failed save', async () => {
    getSettings.mockResolvedValue({ openai_astra_source_group_ids: [40, 99] })
    getGroups.mockResolvedValue([
      { id: 10, name: 'Ordinary', platform: 'openai', status: 'active', subscription_type: 'standard' },
      { id: 40, name: 'Disabled name', platform: 'openai', status: 'inactive', subscription_type: 'standard' },
    ])
    updateSettings.mockRejectedValue(new Error('invalid sources'))
    const wrapper = mountPanel('zh', CustomFeaturesPanel)
    await flushPromises()
    expect(wrapper.text()).toContain('Disabled name')
    expect(wrapper.text()).toContain('已删除的分组')
    await wrapper.get('#custom-astra-sources').setValue('no match')
    expect(wrapper.find('#custom-astra-source-99').exists()).toBe(true)
    await wrapper.get('#custom-astra-select-all').trigger('click')
    await wrapper.get('#custom-astra-source-40').setValue(false)
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()
    expect(updateSettings).toHaveBeenLastCalledWith(expect.objectContaining({ openai_astra_source_group_ids: [99, 10] }))
    expect(wrapper.get('#custom-astra-source-99').element.checked).toBe(true)
    await wrapper.get('#custom-astra-sources').setValue('')
    expect(wrapper.get('#custom-astra-source-10').element.checked).toBe(true)
    expect(showError).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('prevents a partial load failure from saving default settings', async () => {
    getSettings.mockResolvedValue({ openai_astra_group_id: 20, openai_sol_group_id: 20 })
    getGroups.mockRejectedValue(new Error('groups unavailable'))
    const wrapper = mountPanel('zh', CustomFeaturesPanel)
    await flushPromises()
    expect(wrapper.get('button[type="submit"]').attributes()).toHaveProperty('disabled')
    await wrapper.get('form').trigger('submit.prevent')
    expect(updateSettings).not.toHaveBeenCalled()
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
  })
})
