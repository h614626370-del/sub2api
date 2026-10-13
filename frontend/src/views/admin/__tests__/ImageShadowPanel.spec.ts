import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import ImageShadowPanel from '../ImageShadowPanel.vue'
import { imageMasterAPI, type ShadowRecord, type ShadowStatus } from '@/api/admin/imageMaster'
import { getAll, getGroupApiKeys } from '@/api/admin/groups'
import Select from '@/components/common/Select.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, values?: Record<string, unknown>) => values ? `${key} ${JSON.stringify(values)}` : key }) }))
vi.mock('@/api/admin/groups', () => ({ getAll: vi.fn(), getGroupApiKeys: vi.fn() }))
vi.mock('@/api/admin/imageMaster', () => ({ imageMasterAPI: { shadow: vi.fn(), saveShadow: vi.fn() } }))
const result = (outcome: string) => ({ outcome, status: 200, images: outcome === 'completed' ? 1 : 0, duration_ms: 2000 })
const row = (id: string, original: string, test: string): ShadowRecord => ({
  id, request_id: id, started_at: Date.now(), user_id: 1, source_group_id: 2, group_id: 9, api_key_id: 90,
  requested_model: 'gpt-5.5', model: 'gpt-image-2', route: 'images-generations', original: result(original), test: result(test)
})
const fixture = (): ShadowStatus => ({
  config: { enabled: false, group_id: 9, api_key_id: 90 },
  active: 0, large_skipped: 0, storage_error: false,
  items: [row('pair-a', 'completed', 'completed'), row('pair-b', 'completed', 'failed'), row('unknown', 'unknown', 'completed'), row('unsupported', 'completed', 'unsupported')]
})
let wrapper: VueWrapper
const key = (id = 90, group_id = 9) => ({
  id, group_id, user_id: 7, name: `Test ${id}`, status: 'active',
  quota: 0, quota_used: 0, expires_at: null, key: 'sk-secret-never-render',
  user: { username: 'Test owner' }
})
const keyPage = (items = [key(), key(99)], pages = 1) => ({ items, pages, page: 1, page_size: 100, total: items.length })
beforeEach(() => {
  vi.resetAllMocks(); vi.useFakeTimers()
  vi.mocked(imageMasterAPI.shadow).mockResolvedValue(fixture())
  vi.mocked(imageMasterAPI.saveShadow).mockImplementation(async c => c)
  vi.mocked(getAll).mockResolvedValue([{ id: 9, name: 'Test group' }, { id: 10, name: 'Other group' }] as Awaited<ReturnType<typeof getAll>>)
  vi.mocked(getGroupApiKeys).mockResolvedValue(keyPage())
})
afterEach(() => { wrapper?.unmount(); vi.useRealTimers() })
async function create() { wrapper = mount(ImageShadowPanel); await flushPromises() }
describe('ImageShadowPanel', () => {
  it('compares the same pairs and excludes unknown and unsupported outcomes', async () => {
    await create()
    const comparison = wrapper.get('.comparison').text()
    expect(comparison).toContain('100.0% · 2/2')
    expect(comparison).toContain('50.0% · 1/2')
    expect(wrapper.findAll('.records tbody tr')).toHaveLength(4)
  })
  it('saves a separate disabled-by-default setting using IDs only', async () => {
    await create()
    expect((wrapper.get('[data-testid="shadow-enabled"]').element as HTMLInputElement).checked).toBe(false)
    expect(wrapper.find('input[type="password"]').exists()).toBe(false)
    expect(wrapper.findAll('input[type="number"]')).toHaveLength(0)
    expect(getGroupApiKeys).toHaveBeenCalledWith(9, 1, 100)
    expect(wrapper.getComponent(Select).props('options')).toEqual([
      { value: 90, label: 'Test 90 · Test owner #7 · Key #90', disabled: false },
      { value: 99, label: 'Test 99 · Test owner #7 · Key #99', disabled: false }
    ])
    expect(wrapper.html()).not.toContain('sk-secret-never-render')
    expect(wrapper.text()).not.toContain('imageMaster.shadowSample')
    expect(wrapper.text()).not.toContain('imageMaster.shadowConcurrency')
    await wrapper.get('[data-testid="shadow-enabled"]').setValue(true)
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(imageMasterAPI.saveShadow).toHaveBeenCalledWith({ ...fixture().config, enabled: true })
    expect(wrapper.text()).toContain('imageMaster.saved')
  })
  it('preserves unsaved edits during auto refresh and displays save failure', async () => {
    await create(); wrapper.getComponent(Select).vm.$emit('update:modelValue', 99)
    await vi.advanceTimersByTimeAsync(5000); await flushPromises()
    expect(wrapper.getComponent(Select).props('modelValue')).toBe(99)
    vi.mocked(imageMasterAPI.saveShadow).mockRejectedValue(new Error('Invalid test group'))
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('Invalid test group')
    expect(wrapper.getComponent(Select).props('modelValue')).toBe(99)
  })
  it('loads every page and disables inactive, expired and exhausted keys', async () => {
    vi.mocked(getGroupApiKeys)
      .mockResolvedValueOnce(keyPage([key()], 2))
      .mockResolvedValueOnce(keyPage([
        { ...key(91), status: 'inactive' },
        { ...key(92), expires_at: '2000-01-01T00:00:00Z' },
        { ...key(93), quota: 1, quota_used: 1 }
      ], 2))
    await create()
    expect(getGroupApiKeys).toHaveBeenLastCalledWith(9, 2, 100)
    expect(wrapper.getComponent(Select).props('options').map(option => option.disabled)).toEqual([false, true, true, true])
  })
  it('clears the selected key on group change and blocks saving an enabled empty group', async () => {
    await create()
    vi.mocked(getGroupApiKeys).mockResolvedValue(keyPage([]))
    await wrapper.get('[data-testid="shadow-group"]').setValue('10')
    await flushPromises()
    expect(wrapper.getComponent(Select).props('modelValue')).toBe(0)
    expect(wrapper.getComponent(Select).props('options')).toEqual([])
    await wrapper.get('[data-testid="shadow-enabled"]').setValue(true)
    await wrapper.get('form').trigger('submit')
    expect(imageMasterAPI.saveShadow).not.toHaveBeenCalled()
  })
  it('retains unavailable saved IDs and allows disabling the feature', async () => {
    vi.mocked(imageMasterAPI.shadow).mockResolvedValue({ ...fixture(), config: { ...fixture().config, enabled: true } })
    vi.mocked(getGroupApiKeys).mockResolvedValue(keyPage([]))
    await create()
    expect(wrapper.getComponent(Select).props('options')).toEqual([{ value: 90, label: '#90 · imageMaster.shadowKeyUnavailable', disabled: true }])
    await wrapper.get('form').trigger('submit')
    expect(imageMasterAPI.saveShadow).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="shadow-enabled"]').setValue(false)
    await wrapper.get('form').trigger('submit')
    expect(imageMasterAPI.saveShadow).toHaveBeenCalledWith(fixture().config)
  })
  it('offers retry after key loading fails', async () => {
    vi.mocked(getGroupApiKeys).mockRejectedValueOnce(new Error('offline'))
    await create()
    expect(wrapper.get('.key-error').text()).toContain('imageMaster.shadowKeysError')
    await wrapper.get('[data-testid="shadow-enabled"]').setValue(true)
    await wrapper.get('form').trigger('submit')
    expect(imageMasterAPI.saveShadow).not.toHaveBeenCalled()
    await wrapper.get('.key-error button').trigger('click'); await flushPromises()
    expect(wrapper.find('.key-error').exists()).toBe(false)
    await wrapper.get('form').trigger('submit')
    expect(imageMasterAPI.saveShadow).toHaveBeenCalled()
  })
  it('ignores a late response from the previous group', async () => {
    let resolve!: (value: ReturnType<typeof keyPage>) => void
    vi.mocked(getGroupApiKeys).mockReturnValueOnce(new Promise(done => { resolve = done }))
    await create()
    vi.mocked(getGroupApiKeys).mockResolvedValue(keyPage([key(100, 10)]))
    await wrapper.get('[data-testid="shadow-group"]').setValue('10'); await flushPromises()
    resolve(keyPage()); await flushPromises()
    expect(wrapper.getComponent(Select).props('options').map(option => option.value)).toEqual([100])
  })
  it('filters differences and unsupported copies without changing the paired metric', async () => {
    await create(); await wrapper.get('.filters select').setValue('difference')
    expect(wrapper.findAll('.records tbody tr')).toHaveLength(1)
    expect(wrapper.get('.records').text()).toContain('pair-b')
    expect(wrapper.get('.comparison').text()).toContain('50.0% · 1/2')
    await wrapper.get('.filters select').setValue('unsupported')
    expect(wrapper.get('.records').text()).toContain('unsupported')
  })
  it('shows an informative empty state and stops polling after unmount', async () => {
    vi.mocked(imageMasterAPI.shadow).mockResolvedValue({ ...fixture(), items: [] })
    await create(); expect(wrapper.text()).toContain('imageMaster.shadowEmpty')
    wrapper.unmount(); await vi.advanceTimersByTimeAsync(10000)
    expect(imageMasterAPI.shadow).toHaveBeenCalledTimes(1)
  })
})
