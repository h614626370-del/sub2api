import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { homepageAPI, type HomepageState } from '@/api/admin/homepage'
import HomepageView from '../HomepageView.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, te: () => true, n: (value: number) => String(value), locale: { value: 'zh-CN' } }) }))
const { copyToClipboard } = vi.hoisted(() => ({ copyToClipboard: vi.fn() }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard }) }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<div><slot /></div>' } }))
vi.mock('@/components/common/BaseDialog.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/api/admin/homepage', () => ({ homepageAPI: { get: vi.fn(), save: vi.fn(), restore: vi.fn() } }))

const custom: HomepageState = {
  enabled: true,
  public_origin: 'https://public.example',
  html: '<!doctype html><title>Custom homepage</title>',
  revision: 3,
  backend_only: false,
  has_previous: false,
  warnings: [],
}

beforeEach(() => vi.resetAllMocks())

const empty = { ...custom, enabled: false, public_origin: '', html: '', revision: 0 }

async function chooseFiles(wrapper: ReturnType<typeof mount>, ...files: File[]) {
  const input = wrapper.get<HTMLInputElement>('input[type=file]')
  Object.defineProperty(input.element, 'files', { value: files, configurable: true })
  await input.trigger('change')
}

describe('Homepage file upload', () => {
  it('shows the upload hint and requires an entry on first activation', async () => {
    vi.mocked(homepageAPI.get).mockResolvedValue(empty)
    const wrapper = mount(HomepageView)
    await flushPromises()

    expect(wrapper.find('textarea').exists()).toBe(false)
    expect(wrapper.get('#homepage-entry-hint').text()).toBe('homepage.assets.entryHint')
    expect(wrapper.get<HTMLInputElement>('input[type=checkbox]').element.checked).toBe(false)
    expect(wrapper.find('a[href="/sitemap.xml"]').exists()).toBe(false)
    expect(wrapper.get('button[type=submit]').attributes()).toHaveProperty('disabled')
    await wrapper.get('#homepage-origin').setValue('https://public.example')
    await wrapper.get('input[type=checkbox]').setValue(true)
    await wrapper.get('form').trigger('submit')
    expect(wrapper.get('[role=alert]').text()).toBe('homepage.errors.HOMEPAGE_ENTRY_REQUIRED')
    expect(homepageAPI.save).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('publishes home.html when it is the only entry in the batch', async () => {
    vi.mocked(homepageAPI.get).mockResolvedValue(empty)
    vi.mocked(homepageAPI.save).mockResolvedValue({ ...custom, entry_file: 'home.html' })
    const wrapper = mount(HomepageView)
    await flushPromises()

    await wrapper.get('#homepage-origin').setValue('https://PUBLIC.example/')
    await wrapper.get('input[type=checkbox]').setValue(true)
    expect(wrapper.get('#homepage-origin').attributes()).toHaveProperty('required')
    await chooseFiles(wrapper, new File([custom.html], 'home.html'))
    await wrapper.get('form').trigger('submit')
    await vi.waitFor(() => expect(homepageAPI.save).toHaveBeenCalledWith({
      enabled: true, public_origin: 'https://PUBLIC.example/', html: custom.html, entry_file: 'home.html',
    }, 0))
    expect(wrapper.get('a[href="/sitemap.xml"]').exists()).toBe(true)
    expect(wrapper.get('a[href="/?landing=1"]').text()).toBe('homepage.open')
    wrapper.unmount()
  })

  it('keeps saved HTML when disabling the homepage', async () => {
    const config = { enabled: false, public_origin: custom.public_origin, html: custom.html }
    vi.mocked(homepageAPI.get).mockResolvedValue(custom)
    vi.mocked(homepageAPI.save).mockResolvedValue({ ...custom, ...config, revision: 4 })
    const wrapper = mount(HomepageView)
    await flushPromises()

    expect(wrapper.text()).toContain('homepage.assets.existingEntry')
    await wrapper.get('input[type=checkbox]').setValue(false)
    expect(wrapper.get('a[href="/sitemap.xml"]').exists()).toBe(true)
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(homepageAPI.save).toHaveBeenCalledWith(config, 3)
    expect(wrapper.text()).toContain('homepage.inactive')
    expect(wrapper.find('a[href="/sitemap.xml"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('homepage.assets.existingEntry')
    wrapper.unmount()
  })

  it('stages multiple assets and publishes them with the HTML', async () => {
    vi.mocked(homepageAPI.get).mockResolvedValue(custom)
    const assets = [
      { name: '背景 图.jpg', size: 3, digest: 'a', content_type: 'image/jpeg' },
      { name: 'style.css', size: 4, digest: 'b', content_type: 'text/css' },
    ]
    const html = '<!doctype html><title>Uploaded</title><link rel="stylesheet" href="./style.css">'
    vi.mocked(homepageAPI.save).mockResolvedValue({ ...custom, html, entry_file: 'index.html', assets, revision: 4 })
    copyToClipboard.mockResolvedValue(true)
    const wrapper = mount(HomepageView)
    await flushPromises()

    await chooseFiles(wrapper, new File(['img'], '背景 图.jpg'), new File([html], 'INDEX.HTML'),
      new File(['body'], 'style.css'), new File([custom.html], 'home.html'))
    expect(wrapper.text()).toContain('背景 图.jpg')
    expect(wrapper.text()).toContain('style.css')
    expect(homepageAPI.save).not.toHaveBeenCalled()
    const copy = wrapper.findAll('li button[title="homepage.assets.copy"]').find(button => button.element.closest('li')?.textContent?.includes('背景 图.jpg'))
    await copy!.trigger('click')
    expect(copyToClipboard).toHaveBeenCalledWith('/homepage-assets/%E8%83%8C%E6%99%AF%20%E5%9B%BE.jpg', 'homepage.assets.copied')

    await wrapper.get('form').trigger('submit')
    await vi.waitFor(() => expect(homepageAPI.save).toHaveBeenCalledWith(expect.objectContaining({
      html,
      entry_file: 'index.html',
      assets: expect.arrayContaining([{ name: '背景 图.jpg', data: 'aW1n' }, { name: 'style.css', data: 'Ym9keQ==' }]),
    }), 3))
    await flushPromises()
    expect(wrapper.text()).not.toContain('homepage.assets.pending')
    wrapper.unmount()
  })

  it('preserves a selected entry when a conflicting revision reloads', async () => {
    vi.mocked(homepageAPI.get).mockResolvedValueOnce(empty).mockResolvedValueOnce(custom)
    vi.mocked(homepageAPI.save).mockRejectedValue({ reason: 'HOMEPAGE_CONFLICT' })
    const wrapper = mount(HomepageView)
    await flushPromises()

    const draft = '<!doctype html><title>My edited homepage</title>'
    await chooseFiles(wrapper, new File([draft], 'index.html'))
    await wrapper.get('#homepage-origin').setValue('https://other.example')
    await wrapper.get('form').trigger('submit')
    await vi.waitFor(() => expect(homepageAPI.save).toHaveBeenCalledWith({
      enabled: false, public_origin: 'https://other.example', html: draft, entry_file: 'index.html',
    }, 0))
    await flushPromises()
    expect(wrapper.text()).toContain('homepage.errors.HOMEPAGE_CONFLICT')
    await wrapper.get('button.btn-secondary:not(:disabled)').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('index.html')
    expect(wrapper.text()).toContain('homepage.assets.pending')
    wrapper.unmount()
  })
})
