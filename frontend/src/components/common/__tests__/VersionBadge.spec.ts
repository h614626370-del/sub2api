import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { reactive } from 'vue'
import VersionBadge from '../VersionBadge.vue'

const mocks = vi.hoisted(() => ({
  app: {} as Record<string, any>,
  auth: { isAdmin: true },
  update: vi.fn(),
  versions: vi.fn(),
  rollback: vi.fn(),
  restart: vi.fn()
}))
vi.mock('@/stores', () => ({
  useAppStore: () => mocks.app,
  useAuthStore: () => mocks.auth
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/admin/system', () => ({
  performUpdate: mocks.update,
  getRollbackVersions: mocks.versions,
  rollback: mocks.rollback,
  restartService: mocks.restart
}))
vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copied: false, copyToClipboard: vi.fn() })
}))

describe('VersionBadge dual repositories', () => {
  let wrapper: VueWrapper | undefined
  beforeEach(() => {
    vi.resetAllMocks()
    mocks.auth.isAdmin = true
    mocks.app = reactive({
      currentVersion: '2.10.0', latestVersion: '2.10.0.1', hasUpdate: true,
      buildType: 'release', versionLoaded: true, versionLoading: false, versionWarning: '',
      releaseInfo: { body: 'Custom release', html_url: 'https://github.com/h614626370-del/sub2api/releases/tag/v2.10.0.1' },
      upstreamVersionInfo: {
        latest_version: '2.11.0', has_update: true,
        release_info: { body: 'Upstream release', html_url: 'https://github.com/ranxi2001/sub2api/releases/tag/v2.11.0' }
      },
      upstreamVersionLoading: false, upstreamVersionWarning: '',
      fetchVersion: vi.fn(), fetchUpstreamVersion: vi.fn(), clearVersionCache: vi.fn()
    })
    mocks.versions.mockResolvedValue({
      versions: [{ version: '2.9.11.10', published_at: '2026-10-07T00:00:00Z' }]
    })
  })
  afterEach(() => {
    wrapper?.unmount()
    wrapper = undefined
    document.body.innerHTML = ''
  })
  async function open() {
    wrapper = mount(VersionBadge, { attachTo: document.body, global: { stubs: { Icon: true } } })
    await wrapper.get('[data-testid="version-trigger"]').trigger('click')
    await flushPromises()
  }
  const element = (id: string) => document.querySelector<HTMLElement>(`[data-testid="${id}"]`)!
  async function click(id: string) {
    element(id).click()
    await flushPromises()
  }

  it('shows both repositories, with installation only in the custom section', async () => {
    await open()
    expect(element('version-upstream').textContent).toContain('ranxi2001/sub2api')
    expect(element('version-upstream').textContent).toContain('v2.11.0')
    expect(element('version-custom').textContent).toContain('h614626370-del/sub2api')
    expect(element('version-custom').textContent).toContain('v2.10.0.1')
    expect(element('version-upstream').querySelector('button')).toBeNull()
    expect(element('version-custom').contains(element('install-custom-update'))).toBe(true)
    expect(mocks.app.fetchVersion).toHaveBeenCalledWith(false)
    expect(mocks.app.fetchUpstreamVersion).toHaveBeenCalledWith(false)
    await click('refresh-versions')
    expect(mocks.app.fetchVersion).toHaveBeenCalledWith(true)
    expect(mocks.app.fetchUpstreamVersion).toHaveBeenCalledWith(true)
  })

  it('does not check or expose updates to non-admin users', () => {
    mocks.auth.isAdmin = false
    wrapper = mount(VersionBadge, { props: { version: '2.10.0' } })
    expect(wrapper.text()).toBe('v2.10.0')
    expect(wrapper.find('button').exists()).toBe(false)
    expect(mocks.app.fetchVersion).not.toHaveBeenCalled()
    expect(mocks.app.fetchUpstreamVersion).not.toHaveBeenCalled()
  })

  it('upstream failure leaves custom installation available; custom warning blocks it', async () => {
    mocks.app.upstreamVersionWarning = 'upstream offline'
    await open()
    expect(element('version-upstream').textContent).toContain('upstream offline')
    expect((element('install-custom-update') as HTMLButtonElement).disabled).toBe(false)
    mocks.app.versionWarning = 'custom offline'
    await flushPromises()
    expect((element('install-custom-update') as HTMLButtonElement).disabled).toBe(true)
    await click('install-custom-update')
    expect(mocks.update).not.toHaveBeenCalled()
  })

  it('source builds cannot install or roll back binaries', async () => {
    mocks.app.buildType = 'source'
    await open()
    expect(element('install-custom-update')).toBeNull()
    await click('rollback-toggle')
    expect(mocks.versions).not.toHaveBeenCalled()
    expect(element('confirm-rollback')).toBeNull()
  })

  it('shows current versions without an installation action when both are up to date', async () => {
    mocks.app.hasUpdate = false
    mocks.app.upstreamVersionInfo.has_update = false
    await open()
    expect(element('install-custom-update')).toBeNull()
    expect(element('version-upstream').textContent).toContain('version.upToDate')
    expect(element('version-custom').textContent).toContain('version.upToDate')
  })

  it('installs custom updates and retains the required restart across refreshes', async () => {
    mocks.update.mockResolvedValue({ need_restart: true })
    await open()
    await click('install-custom-update')
    expect(mocks.update).toHaveBeenCalledTimes(1)
    expect(element('restart-service')).not.toBeNull()
    expect(mocks.app.clearVersionCache).toHaveBeenCalled()
    await click('refresh-versions')
    expect(element('restart-service')).not.toBeNull()
    expect(mocks.restart).not.toHaveBeenCalled()
  })

  it('does not request restart when the backend reports already up to date', async () => {
    mocks.update.mockResolvedValue({ already_up_to_date: true, need_restart: false })
    await open()
    await click('install-custom-update')
    expect(element('restart-service')).toBeNull()
    expect(element('version-custom').textContent).toContain('version.upToDate')
  })

  it('surfaces install failure and permits retry', async () => {
    mocks.update.mockRejectedValue(new Error('download unavailable'))
    await open()
    await click('install-custom-update')
    expect(element('version-custom').textContent).toContain('download unavailable')
    expect((element('install-custom-update') as HTMLButtonElement).disabled).toBe(false)
  })

  it('keeps four-part rollback versions and commands bound to the custom repository', async () => {
    mocks.rollback.mockResolvedValue({ need_restart: true })
    await open()
    await click('rollback-toggle')
    document.querySelector<HTMLInputElement>('input[name="rollback-version"]')!.click()
    await flushPromises()
    expect(element('rollback-command').textContent).toContain('SUB2API_GITHUB_REPO=h614626370-del/sub2api')
    expect(element('rollback-command').textContent).toContain('rollback v2.9.11.10')
    document.querySelectorAll<HTMLButtonElement>('[role="tab"]')[1].click()
    await flushPromises()
    expect(element('rollback-command').textContent).toContain('ghcr.io/h614626370-del/sub2api:2.9.11.10')
    await click('confirm-rollback')
    expect(mocks.rollback).toHaveBeenCalledWith('2.9.11.10')
    expect(element('restart-service')).not.toBeNull()
  })

  it('rejects release links outside the corresponding repository and closes on Escape', async () => {
    mocks.app.releaseInfo.html_url = 'https://example.com/releases/tag/v2.10.0.1'
    await open()
    expect(element('version-custom').querySelectorAll('a')).toHaveLength(1)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    expect(element('version-custom')).toBeNull()
    expect(document.activeElement).toBe(element('version-trigger'))
  })
})
