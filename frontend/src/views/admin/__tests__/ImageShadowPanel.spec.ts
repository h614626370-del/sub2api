import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import ImageShadowPanel from '../ImageShadowPanel.vue'
import { imageMasterAPI, type ShadowRecord, type ShadowStatus } from '@/api/admin/imageMaster'
import { getAll } from '@/api/admin/groups'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, values?: Record<string, unknown>) => values ? `${key} ${JSON.stringify(values)}` : key }) }))
vi.mock('@/api/admin/groups', () => ({ getAll: vi.fn() }))
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
beforeEach(() => {
  vi.resetAllMocks(); vi.useFakeTimers()
  vi.mocked(imageMasterAPI.shadow).mockResolvedValue(fixture())
  vi.mocked(imageMasterAPI.saveShadow).mockImplementation(async c => c)
  vi.mocked(getAll).mockResolvedValue([{ id: 9, name: 'Test group' }] as Awaited<ReturnType<typeof getAll>>)
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
    expect(wrapper.findAll('input[type="number"]')).toHaveLength(1)
    expect(wrapper.text()).not.toContain('imageMaster.shadowSample')
    expect(wrapper.text()).not.toContain('imageMaster.shadowConcurrency')
    await wrapper.get('[data-testid="shadow-enabled"]').setValue(true)
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(imageMasterAPI.saveShadow).toHaveBeenCalledWith({ ...fixture().config, enabled: true })
    expect(wrapper.text()).toContain('imageMaster.saved')
  })
  it('preserves unsaved edits during auto refresh and displays save failure', async () => {
    await create(); await wrapper.get('[data-testid="shadow-key"]').setValue('99')
    await vi.advanceTimersByTimeAsync(5000); await flushPromises()
    expect((wrapper.get('[data-testid="shadow-key"]').element as HTMLInputElement).value).toBe('99')
    vi.mocked(imageMasterAPI.saveShadow).mockRejectedValue(new Error('Invalid test group'))
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('Invalid test group')
    expect((wrapper.get('[data-testid="shadow-key"]').element as HTMLInputElement).value).toBe('99')
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
