import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import zh from '@/i18n/locales/zh/admin/accounts'
import en from '@/i18n/locales/en/admin/accounts'
import Select from '@/components/common/Select.vue'
import AccountTimezonePanel from '../AccountTimezonePanel.vue'

const api = vi.hoisted(() => ({
  list: vi.fn(), get: vi.fn(), detect: vi.fn(), getSettings: vi.fn(), updateSettings: vi.fn(),
}))
vi.mock('@/api/admin/accounts', () => ({ list: api.list }))
vi.mock('@/api/admin/accountTimezone', () => ({ getAccountTimezone: api.get, detectAccountTimezone: api.detect }))
vi.mock('@/api/admin/settings', () => ({ getSettings: api.getSettings, updateSettings: api.updateSettings }))
const proxy = {
  timezone: 'America/Los_Angeles', detected_timezone: 'America/Los_Angeles',
  ip: '203.0.113.10', source: 'proxy', stale: false, has_proxy: true, detected_at: '2026-09-23T06:00:00Z',
}
const fallback = { ...proxy, timezone: 'Asia/Shanghai', detected_timezone: '', ip: '', source: 'global', detected_at: undefined }

function panel(locale = 'zh') {
  return mount(AccountTimezonePanel, {
    global: {
      plugins: [createI18n({
        legacy: false, locale,
        messages: {
          zh: { admin: zh, common: { loading: '加载中', saving: '保存中', refresh: '刷新' } },
          en: { admin: en, common: { loading: 'Loading', saving: 'Saving', refresh: 'Refresh' } },
        },
      })],
      stubs: { Icon: true },
    },
  })
}

async function openDetails(wrapper: VueWrapper) {
  const details = wrapper.get('details')
  ;(details.element as HTMLDetailsElement).open = true
  await details.trigger('toggle')
  await flushPromises()
}

beforeEach(() => {
  vi.resetAllMocks()
  api.list.mockResolvedValue({ items: [{ id: 1, name: 'Account A', platform: 'openai', type: 'oauth' }], total: 1 })
  api.getSettings.mockResolvedValue({ openai_oauth_default_timezone: 'Asia/Shanghai' })
  api.updateSettings.mockImplementation(async value => value)
  api.get.mockResolvedValue(proxy)
  api.detect.mockResolvedValue(proxy)
})

describe('AccountTimezonePanel', () => {
  it.each(['zh', 'en'])('opens timezone help without probing accounts (%s)', async locale => {
    const wrapper = panel(locale)
    await flushPromises()
    const label = locale === 'zh' ? '时区生效规则' : 'Timezone rules'
    await wrapper.get(`button[aria-label="${label}"]`).trigger('click')
    await flushPromises()
    const tooltip = document.body.querySelector('[role="tooltip"]') as HTMLElement
    expect(tooltip.style.display).not.toBe('none')
    expect(tooltip.textContent).toContain('OpenAI OAuth')
    expect(tooltip.textContent).toContain(locale === 'zh' ? '有效代理时区 → 统一时区 → 客户端原值' : 'valid proxy timezone → default timezone → client value')
    expect(tooltip.textContent).toContain('America/Los_Angeles')
    expect(tooltip.textContent).toContain(locale === 'zh' ? '升级保留已有设置，包括空值' : 'Upgrades preserve existing settings, including an empty value')
    expect(tooltip.textContent).toContain(locale === 'zh' ? '已有有效代理时区仍优先' : 'a valid proxy timezone still takes priority')
    expect(tooltip.textContent).toContain(locale === 'zh' ? '下方选择账号时尝试检测' : 'when selecting the account below')
    expect(tooltip.textContent).toContain(locale === 'zh' ? '不提供单账号手动时区设置' : 'per-account manual timezone overrides are not supported')
    expect(api.list).not.toHaveBeenCalled()
    expect(api.detect).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it.each(['zh', 'en'])('loads a searchable default without loading accounts (%s)', async locale => {
    const wrapper = panel(locale)
    await flushPromises()
    expect(wrapper.get('details').attributes('open')).toBeUndefined()
    expect(api.list).not.toHaveBeenCalled()
    const select = wrapper.getComponent(Select)
    expect(select.props('modelValue')).toBe('Asia/Shanghai')
    expect(select.props('searchable')).toBe(true)
    expect(select.props('options')).toContainEqual({ value: '', label: locale === 'zh' ? '保留客户端时区' : 'Keep client timezone' })
    expect(select.props('options')).toContainEqual({
      value: 'America/Los_Angeles', label: `${locale === 'zh' ? '洛杉矶' : 'Los Angeles'} · America/Los_Angeles`,
    })
    expect(wrapper.find('#timezone-mode').exists()).toBe(false)
    expect(wrapper.find('#timezone-override').exists()).toBe(false)
    wrapper.unmount()
  })

  it('saves and clears only the global timezone without probing accounts', async () => {
    const wrapper = panel()
    await flushPromises()
    wrapper.getComponent(Select).vm.$emit('update:modelValue', 'America/New_York')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.updateSettings).toHaveBeenLastCalledWith({ openai_oauth_default_timezone: 'America/New_York' })
    wrapper.getComponent(Select).vm.$emit('update:modelValue', '')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.updateSettings).toHaveBeenLastCalledWith({ openai_oauth_default_timezone: '' })
    expect(api.detect).not.toHaveBeenCalled()
    expect(api.list).not.toHaveBeenCalled()
    expect(wrapper.get('[role="status"]').text()).toContain('已保存')
    wrapper.unmount()
  })

  it('restricts initial loading, search and pagination to OAuth accounts', async () => {
    api.list.mockResolvedValue({ items: [{ id: 1, name: 'Account A', platform: 'openai', type: 'oauth' }], total: 51 })
    const wrapper = panel()
    await openDetails(wrapper)
    expect(api.list).toHaveBeenLastCalledWith(1, 50, { platform: 'openai', type: 'oauth', search: '', lite: 'true' })
    await wrapper.get('#timezone-search').setValue(' Account A ')
    await wrapper.get('#timezone-search').trigger('keydown.enter')
    await flushPromises()
    expect(api.list).toHaveBeenLastCalledWith(1, 50, { platform: 'openai', type: 'oauth', search: 'Account A', lite: 'true' })
    await wrapper.findAll('button').find(button => button.text() === '下一页')!.trigger('click')
    await flushPromises()
    expect(api.list).toHaveBeenLastCalledWith(2, 50, { platform: 'openai', type: 'oauth', search: 'Account A', lite: 'true' })
    wrapper.unmount()
  })

  it('shows cached proxy details without another probe', async () => {
    const wrapper = panel()
    await openDetails(wrapper)
    await wrapper.get('#timezone-account').setValue(1)
    await flushPromises()
    expect(wrapper.text()).toContain('America/Los_Angeles')
    expect(wrapper.text()).toContain('203.0.113.10')
    expect(wrapper.text()).toContain('2026')
    expect(api.detect).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('detects a missing proxy result even when a global timezone is effective', async () => {
    api.get.mockResolvedValue(fallback)
    const wrapper = panel()
    await openDetails(wrapper)
    await wrapper.get('#timezone-account').setValue(1)
    await flushPromises()
    expect(api.detect).toHaveBeenCalledWith(1)
    expect(wrapper.text()).toContain('America/Los_Angeles')
    wrapper.unmount()
  })

  it('keeps the global fallback visible after failed detection', async () => {
    api.get.mockResolvedValue({ ...fallback, stale: true })
    api.detect.mockRejectedValue(new Error('offline'))
    const wrapper = panel()
    await openDetails(wrapper)
    await wrapper.get('#timezone-account').setValue(1)
    await flushPromises()
    expect(wrapper.text()).toContain('Asia/Shanghai')
    expect(wrapper.get('[role="alert"]').text()).toContain('代理时区检测失败')
    expect(wrapper.text()).toContain('旧识别结果已失效')
    wrapper.unmount()
  })

  it('retains a valid cached proxy result if a forced refresh fails', async () => {
    const wrapper = panel()
    await openDetails(wrapper)
    await wrapper.get('#timezone-account').setValue(1)
    await flushPromises()
    api.detect.mockRejectedValue(new Error('offline'))
    await wrapper.findAll('button').find(button => button.text() === '重新检测代理')!.trigger('click')
    await flushPromises()
    expect(api.detect).toHaveBeenCalledWith(1, true)
    expect(wrapper.text()).toContain('America/Los_Angeles')
    expect(wrapper.get('[role="alert"]').text()).toContain('已保留现有有效结果')
    wrapper.unmount()
  })

  it('disables detection without a proxy and refreshes the selected state after saving', async () => {
    api.get.mockResolvedValue({ ...fallback, has_proxy: false })
    const wrapper = panel()
    await openDetails(wrapper)
    await wrapper.get('#timezone-account').setValue(1)
    await flushPromises()
    const detect = wrapper.findAll('button').find(button => button.text() === '重新检测代理')!
    expect(detect.attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('账号未配置代理')
    api.get.mockResolvedValue({ ...fallback, has_proxy: false, timezone: '', source: 'none' })
    wrapper.getComponent(Select).vm.$emit('update:modelValue', '')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.get).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('客户端原值')
    expect(api.detect).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('does not probe after a default save even if the selected proxy has no result', async () => {
    api.get.mockResolvedValue(fallback)
    api.detect.mockRejectedValue(new Error('offline'))
    const wrapper = panel()
    await openDetails(wrapper)
    await wrapper.get('#timezone-account').setValue(1)
    await flushPromises()
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.get).toHaveBeenCalledTimes(3)
    expect(api.detect).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('reloads the fallback if the proxy changes during a failed forced detection', async () => {
    const wrapper = panel()
    await openDetails(wrapper)
    await wrapper.get('#timezone-account').setValue(1)
    await flushPromises()
    api.get.mockResolvedValue({ ...fallback, stale: true })
    api.detect.mockRejectedValue(new Error('proxy changed'))
    await wrapper.findAll('button').find(button => button.text() === '重新检测代理')!.trigger('click')
    await flushPromises()
    expect(wrapper.get('dl').text()).toContain('Asia/Shanghai')
    expect(wrapper.text()).toContain('旧识别结果已失效')
    wrapper.unmount()
  })

  it('prevents saving defaults after load failure and supports retry', async () => {
    api.getSettings.mockRejectedValueOnce(new Error('offline')).mockResolvedValue({ openai_oauth_default_timezone: 'UTC' })
    const wrapper = panel()
    await flushPromises()
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()
    await wrapper.get('form').trigger('submit')
    expect(api.updateSettings).not.toHaveBeenCalled()
    await wrapper.get('[role="alert"] button').trigger('click')
    await flushPromises()
    expect(wrapper.getComponent(Select).props('modelValue')).toBe('UTC')
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('keeps an unsaved default selection after save failure', async () => {
    api.updateSettings.mockRejectedValue(new Error('offline'))
    const wrapper = panel()
    await flushPromises()
    wrapper.getComponent(Select).vm.$emit('update:modelValue', 'Asia/Tokyo')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.getComponent(Select).props('modelValue')).toBe('Asia/Tokyo')
    expect(wrapper.get('[role="alert"]').text()).toContain('未保存的选择已保留')
    wrapper.unmount()
  })
})
