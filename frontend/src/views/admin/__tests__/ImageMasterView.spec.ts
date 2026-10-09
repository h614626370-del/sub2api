import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import ImageMasterView from '../ImageMasterView.vue'
import { imageMasterAPI, type ImageMasterStatus } from '@/api/admin/imageMaster'

vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<main><slot /></main>' } }))
vi.mock('../ImageShadowPanel.vue', () => ({ default: { template: '<section />' } }))
vi.mock('@/components/common/BaseDialog.vue', () => ({ default: { props: ['show', 'title'], template: '<section v-if="show" role="dialog"><h2>{{ title }}</h2><slot /><slot name="footer" /></section>' } }))
vi.mock('@/components/auth/TotpStepUpDialog.vue', () => ({ default: { template: '<span />' } }))
vi.mock('@/composables/useStepUp', () => ({
  useStepUp: () => ({ run: (action: () => Promise<unknown>) => action() }),
  isStepUpCancelled: () => false
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/admin/imageMaster', () => ({
  imageMasterAPI: { status: vi.fn(), save: vi.fn(), cancel: vi.fn(), remove: vi.fn(), raw: vi.fn(), export: vi.fn() }
}))

const config = {
  enabled: false,
  timeout_seconds: 900, heartbeat_seconds: 5, max_body_mib: 128, max_response_mib: 64,
  done_sentinel: false, raw_request_logging: false
}
const fixture = (): ImageMasterStatus => ({
  config: { ...config }, active: 0, storage_error: false,
  items: [{
    id: '1-request', started_at: 1000, finished_at: 6000, outcome: 'completed', route: 'images-generations',
    requested_model: 'gpt-5.5', model: 'gpt-image-2', user_id: 1, api_key_id: 2,
    source_images: 0, image_count: 1, stream: true, heartbeats: 1,
    duration_ms: 5000, raw_saved: false, stages: [{ name: 'completed', at: 6000 }]
  }]
})
let wrapper: VueWrapper
beforeEach(() => {
  vi.resetAllMocks()
  vi.useFakeTimers()
  vi.mocked(imageMasterAPI.status).mockResolvedValue(fixture())
  vi.mocked(imageMasterAPI.save).mockImplementation(async value => value)
})
afterEach(() => { wrapper?.unmount(); vi.useRealTimers() })
async function create() {
  wrapper = mount(ImageMasterView, { global: { stubs: { Icon: true } } })
  await flushPromises()
}
async function settings() { await wrapper.findAll('[role="tab"]')[1].trigger('click') }

describe('ImageMasterView', () => {
  it('shows requests and the three integrated tabs', async () => {
    await create()
    expect(wrapper.text()).toContain('imageMaster.operations')
    expect(wrapper.text()).toContain('gpt-image-2')
    expect(wrapper.findAll('[role="tab"]')).toHaveLength(3)
    await settings()
    expect(wrapper.find('input[type="url"]').exists()).toBe(false)
    expect(wrapper.find('input[type="password"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('账号诊断')
  })

  it('saves the enable switch using only integrated settings', async () => {
    await create(); await settings()
    await wrapper.get('[data-testid="image-master-enabled"]').setValue(true)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(imageMasterAPI.save).toHaveBeenCalledWith({ ...config, enabled: true })
    expect(wrapper.text()).toContain('imageMaster.saved')
  })

  it('has no local concurrency, queue or pause controls', async () => {
    await create()
    expect(wrapper.text()).not.toContain('imageMaster.pause')
    expect(wrapper.text()).not.toContain('imageMaster.resume')
    expect(wrapper.find('option[value="queued"]').exists()).toBe(false)
    await settings()
    expect(wrapper.text()).not.toContain('imageMaster.concurrency')
    expect(wrapper.text()).not.toContain('imageMaster.queue')
    expect(wrapper.findAll('input[type="number"]')).toHaveLength(4)
    expect(wrapper.find('[data-testid="image-master-enabled"]').exists()).toBe(true)
  })

  it('uses a single enable switch without a controller model or edit routing setting', async () => {
    await create(); await settings()
    expect(wrapper.find('input[pattern]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('imageMaster.controlModel')
    expect(wrapper.text()).not.toContain('imageMaster.directEdits')
    expect(wrapper.findAll('.switches input')).toHaveLength(2)
  })

  it('preserves edited settings during polling', async () => {
    await create(); await settings()
    await wrapper.get('[data-testid="image-master-enabled"]').setValue(true)
    await vi.advanceTimersByTimeAsync(3000); await flushPromises()
    expect((wrapper.get('[data-testid="image-master-enabled"]').element as HTMLInputElement).checked).toBe(true)
    expect(imageMasterAPI.save).not.toHaveBeenCalled()
  })

  it('filters requests and shows the empty state', async () => {
    await create()
    await wrapper.get('input[aria-label="imageMaster.search"]').setValue('not-present')
    expect(wrapper.text()).toContain('imageMaster.empty')
    expect(wrapper.find('.request-link').exists()).toBe(false)
  })

  it('requires confirmation before clearing finished requests', async () => {
    await create()
    await wrapper.get('button[aria-label="imageMaster.clear"]').trigger('click')
    expect(imageMasterAPI.remove).not.toHaveBeenCalled()
    const confirm = wrapper.findAll('button').find(button => button.text() === 'imageMaster.confirmAction')!
    await confirm.trigger('click'); await flushPromises()
    expect(imageMasterAPI.remove).toHaveBeenCalledWith(undefined)
  })

  it('requires confirmation before saving sensitive original requests', async () => {
    await create(); await settings()
    const switches = wrapper.findAll('.switches input')
    await switches[1].setValue(true)
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(wrapper.text()).toContain('imageMaster.rawConfirm')
    expect(imageMasterAPI.save).not.toHaveBeenCalled()
    await wrapper.findAll('button').find(button => button.text() === 'imageMaster.confirmAction')!.trigger('click')
    await flushPromises()
    expect(imageMasterAPI.save).toHaveBeenCalledWith({ ...config, raw_request_logging: true })
  })

  it('surfaces API errors and stops polling after leaving the page', async () => {
    vi.mocked(imageMasterAPI.status).mockRejectedValue(new Error('storage unavailable'))
    await create()
    expect(wrapper.get('[role="alert"]').text()).toContain('storage unavailable')
    const before = vi.mocked(imageMasterAPI.status).mock.calls.length
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(9000)
    expect(imageMasterAPI.status).toHaveBeenCalledTimes(before)
  })

  it('opens record details and does not fetch original requests automatically', async () => {
    await create()
    await wrapper.get('.request-link').trigger('click')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('imageMaster.timeline')
    expect(wrapper.get('[role="dialog"]').text()).toContain('gpt-5.5')
    expect(wrapper.get('[role="dialog"]').text()).toContain('gpt-image-2')
    expect(imageMasterAPI.raw).not.toHaveBeenCalled()
  })
})
