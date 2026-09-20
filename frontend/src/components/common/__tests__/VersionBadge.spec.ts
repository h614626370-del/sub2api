import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import VersionBadge from '@/components/common/VersionBadge.vue'

const { appStore, authStore } = vi.hoisted(() => ({
  authStore: { isAdmin: true },
  appStore: {
    currentVersion: '0.2.7',
    versionLoading: false,
    latestVersion: '0.2.8',
    hasUpdate: true,
    releaseInfo: null,
    versionWarning: '',
    customVersionLoading: false,
    customLatestVersion: '0.2.7',
    customHasUpdate: false,
    customReleaseInfo: null,
    customVersionWarning: '',
    buildType: 'release',
    fetchVersion: vi.fn().mockResolvedValue(null),
    fetchCustomVersion: vi.fn().mockResolvedValue(null),
    clearCustomVersionCache: vi.fn()
  }
}))

vi.mock('@/stores', () => ({
  useAuthStore: () => authStore,
  useAppStore: () => appStore
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) =>
      ({
        'version.officialVersion': '官方版本',
        'version.officialUpdateDetected': '官方已发布新版本',
        'version.customVersion': '我的版本',
        'version.updateFromCustom': '从我的仓库更新'
      })[key] || key
  })
}))

vi.mock('@/api/admin/system', () => ({
  getRollbackVersions: vi.fn(),
  performUpdate: vi.fn(),
  restartService: vi.fn(),
  rollback: vi.fn()
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copied: false, copyToClipboard: vi.fn() })
}))

vi.mock('@/components/icons/Icon.vue', () => ({
  default: { template: '<span />' }
}))

async function openBadge() {
  const wrapper = mount(VersionBadge)
  await wrapper.get('button').trigger('click')
  await flushPromises()
  return wrapper
}

describe('VersionBadge update sources', () => {
  beforeEach(() => {
    appStore.hasUpdate = true
    appStore.latestVersion = '0.2.8'
    appStore.customHasUpdate = false
    appStore.customLatestVersion = '0.2.7'
  })

  it('shows official releases as reminders without an update action', async () => {
    const wrapper = await openBadge()

    expect(wrapper.text()).toContain('官方版本')
    expect(wrapper.text()).toContain('官方已发布新版本')
    expect(wrapper.text()).not.toContain('从我的仓库更新')
  })

  it('shows the update action only for a newer custom release', async () => {
    appStore.hasUpdate = false
    appStore.customHasUpdate = true
    appStore.customLatestVersion = '0.2.8'

    const wrapper = await openBadge()

    expect(wrapper.text()).toContain('我的版本')
    expect(wrapper.text()).toContain('从我的仓库更新')
  })
})
