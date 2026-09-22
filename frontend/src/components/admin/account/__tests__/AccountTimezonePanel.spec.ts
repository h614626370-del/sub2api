import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import zh from '@/i18n/locales/zh/admin/accounts'
import en from '@/i18n/locales/en/admin/accounts'
import AccountTimezonePanel from '../AccountTimezonePanel.vue'

const api = vi.hoisted(() => ({ list: vi.fn(), get: vi.fn(), detect: vi.fn(), set: vi.fn() }))
vi.mock('@/api/admin/accounts', () => ({ list: api.list }))
vi.mock('@/api/admin/accountTimezone', () => ({ getAccountTimezone: api.get, detectAccountTimezone: api.detect, setAccountTimezone: api.set }))
const auto = { timezone: 'America/Los_Angeles', override: '', detected_timezone: 'America/Los_Angeles', ip: '203.0.113.10', source: 'proxy', stale: false, has_proxy: true }
function panel(locale = 'zh') {
  return mount(AccountTimezonePanel, { global: { plugins: [createI18n({ legacy: false, locale, messages: { zh: { admin: zh, common: { loading: '加载中', refresh: '刷新' } }, en: { admin: en, common: { loading: 'Loading', refresh: 'Refresh' } } } })] } })
}
beforeEach(() => {
  vi.resetAllMocks()
  api.list.mockResolvedValue({ items: [{ id: 1, name: 'Account A' }], total: 1 })
  api.get.mockResolvedValue(auto)
  api.detect.mockResolvedValue(auto)
  api.set.mockImplementation(async (_id, override) => ({ ...auto, override, timezone: override || auto.timezone, source: override ? 'manual' : 'proxy' }))
})
describe('AccountTimezonePanel', () => {
  it.each(['zh', 'en'])('loads cached timezone without another probe (%s)', async locale => {
    const wrapper = panel(locale)
    await flushPromises()
    await wrapper.get('#timezone-account').setValue(1)
    await flushPromises()
    expect(wrapper.text()).toContain('America/Los_Angeles')
    expect(api.detect).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('detects missing timezone, saves override, and clears it for automatic mode', async () => {
    api.get.mockResolvedValue({ ...auto, timezone: '', detected_timezone: '', source: 'none' })
    const wrapper = panel()
    await flushPromises()
    await wrapper.get('#timezone-account').setValue(1)
    await flushPromises()
    expect(api.detect).toHaveBeenCalledWith(1)
    await wrapper.get('#timezone-mode').setValue('manual')
    await wrapper.get('#timezone-override').setValue('Asia/Tokyo')
    await wrapper.get('button.btn-primary').trigger('click')
    await flushPromises()
    expect(api.set).toHaveBeenLastCalledWith(1, 'Asia/Tokyo')
    await wrapper.get('#timezone-mode').setValue('auto')
    await wrapper.get('button.btn-primary').trigger('click')
    await flushPromises()
    expect(api.set).toHaveBeenLastCalledWith(1, '')
    wrapper.unmount()
  })
  it('retains a manual draft on failed detection and rejects invalid zones', async () => {
    const wrapper = panel()
    await flushPromises()
    await wrapper.get('#timezone-account').setValue(1)
    await flushPromises()
    await wrapper.get('#timezone-mode').setValue('manual')
    await wrapper.get('#timezone-override').setValue('Asia/Tokyo')
    api.detect.mockRejectedValue(new Error('offline'))
    const detect = wrapper.findAll('button').find(button => button.text() === '重新检测代理')!
    await detect.trigger('click')
    await flushPromises()
    expect((wrapper.get('#timezone-override').element as HTMLInputElement).value).toBe('Asia/Tokyo')
    expect(wrapper.get('[role="alert"]').text()).toContain('已保留现有设置')
    await wrapper.get('#timezone-override').setValue('Not/AZone')
    await wrapper.get('button.btn-primary').trigger('click')
    expect(api.set).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
