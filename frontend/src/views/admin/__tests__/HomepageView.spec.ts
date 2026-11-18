import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { homepageAPI, type HomepageState } from '@/api/admin/homepage'
import HomepageView from '../HomepageView.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, te: () => true, locale: { value: 'zh-CN' } }) }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<div><slot /></div>' } }))
vi.mock('@/components/common/BaseDialog.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/api/admin/homepage', () => ({ homepageAPI: { get: vi.fn(), save: vi.fn(), restore: vi.fn() } }))
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    siteName: 'Sub2API & Co.',
    siteLogo: '',
    cachedPublicSettings: { site_subtitle: 'API "gateway"' },
  }),
}))

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

describe('Homepage editor', () => {
  it('prefills editable HTML without enabling or saving it', async () => {
    vi.mocked(homepageAPI.get).mockResolvedValue(empty)
    const wrapper = mount(HomepageView)
    await flushPromises()

    const textarea = wrapper.get('textarea')
    expect(textarea.element.value).toMatch(/^<!doctype html>/)
    expect(textarea.element.value).toContain('<h1>Sub2API &amp; Co.</h1>')
    expect(textarea.element.value).toContain('content="API &quot;gateway&quot;"')
    expect(textarea.element.value).toContain('src="/logo.svg"')
    expect(textarea.element.value).not.toMatch(/\{\{|<script/i)
    expect(wrapper.get<HTMLInputElement>('input[type=checkbox]').element.checked).toBe(false)
    expect(wrapper.find('a[href="/sitemap.xml"]').exists()).toBe(false)
    expect(wrapper.get('button[type=submit]').attributes()).toHaveProperty('disabled')
    expect(homepageAPI.save).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('publishes the starter with a canonical URL when explicitly enabled', async () => {
    vi.mocked(homepageAPI.get).mockResolvedValue(empty)
    vi.mocked(homepageAPI.save).mockResolvedValue(custom)
    const wrapper = mount(HomepageView)
    await flushPromises()

    await wrapper.get('#homepage-origin').setValue('https://PUBLIC.example/')
    await wrapper.get('input[type=checkbox]').setValue(true)
    expect(wrapper.get('#homepage-origin').attributes()).toHaveProperty('required')
    const html = wrapper.get('textarea').element.value
    expect(html).toContain('<link rel="canonical" href="https://public.example/">')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(homepageAPI.save).toHaveBeenCalledWith({ enabled: true, public_origin: 'https://PUBLIC.example/', html }, 0)
    expect(wrapper.get('a[href="/sitemap.xml"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('keeps saved HTML when disabling the homepage', async () => {
    const config = { enabled: false, public_origin: custom.public_origin, html: custom.html }
    vi.mocked(homepageAPI.get).mockResolvedValue(custom)
    vi.mocked(homepageAPI.save).mockResolvedValue({ ...custom, ...config, revision: 4 })
    const wrapper = mount(HomepageView)
    await flushPromises()

    expect(wrapper.get('textarea').element.value).toBe(custom.html)
    await wrapper.get('input[type=checkbox]').setValue(false)
    expect(wrapper.get('a[href="/sitemap.xml"]').exists()).toBe(true)
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(homepageAPI.save).toHaveBeenCalledWith(config, 3)
    expect(wrapper.text()).toContain('homepage.inactive')
    expect(wrapper.find('a[href="/sitemap.xml"]').exists()).toBe(false)
    expect(wrapper.get('textarea').element.value).toBe(custom.html)
    wrapper.unmount()
  })

  it('preserves edited HTML when the public origin changes or a conflicting revision reloads', async () => {
    vi.mocked(homepageAPI.get).mockResolvedValueOnce(empty).mockResolvedValueOnce(custom)
    vi.mocked(homepageAPI.save).mockRejectedValue({ reason: 'HOMEPAGE_CONFLICT' })
    const wrapper = mount(HomepageView)
    await flushPromises()

    const draft = '<!doctype html><title>My edited homepage</title>'
    await wrapper.get('textarea').setValue(draft)
    await wrapper.get('#homepage-origin').setValue('https://other.example')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(homepageAPI.save).toHaveBeenCalledWith({
      enabled: false, public_origin: 'https://other.example', html: draft,
    }, 0)
    expect(wrapper.text()).toContain('homepage.errors.HOMEPAGE_CONFLICT')
    await wrapper.get('button.btn-secondary:not(:disabled)').trigger('click')
    await flushPromises()
    expect(wrapper.get('textarea').element.value).toBe(draft)
    wrapper.unmount()
  })
})
