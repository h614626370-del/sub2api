<template>
  <div>
    <button
      v-if="isAdmin"
      ref="triggerRef"
      type="button"
      data-testid="version-trigger"
      aria-haspopup="dialog"
      :aria-expanded="dropdownOpen"
      aria-controls="version-updates"
      :title="t('version.repositories')"
      class="flex items-center gap-1.5 rounded-lg px-2 py-1 text-xs transition-colors"
      :class="hasAnyUpdate
        ? 'bg-amber-100 text-amber-700 hover:bg-amber-200 dark:bg-amber-900/30 dark:text-amber-400'
        : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-800 dark:text-dark-400'"
      @click="toggleDropdown"
    >
      <span v-if="currentVersion" class="font-medium">v{{ currentVersion }}</span>
      <span v-else class="h-3 w-12 animate-pulse rounded bg-gray-200 dark:bg-dark-600"></span>
      <Icon v-if="hasAnyUpdate" name="download" size="xs" />
    </button>
    <span v-else-if="version" class="text-xs text-gray-500 dark:text-dark-400">v{{ version }}</span>

    <Teleport to="body">
      <div
        v-if="isAdmin && dropdownOpen"
        id="version-updates"
        ref="dropdownRef"
        role="dialog"
        :aria-label="t('version.repositories')"
        :style="panelStyle"
        class="fixed z-[100] overflow-y-auto rounded-lg border border-gray-200 bg-white shadow-xl dark:border-dark-700 dark:bg-dark-800"
      >
        <header class="flex items-center justify-between gap-3 border-b border-gray-100 px-4 py-3 dark:border-dark-700">
          <div class="min-w-0">
            <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('version.repositories') }}</h2>
            <p class="mt-1 break-all text-xs text-gray-500 dark:text-dark-400">
              {{ t('version.currentVersion') }}: {{ currentVersion ? `v${currentVersion}` : '--' }}
            </p>
          </div>
          <div class="flex shrink-0 gap-1">
            <button
              type="button"
              data-testid="refresh-versions"
              class="rounded p-1.5 text-gray-500 hover:bg-gray-100 disabled:opacity-50 dark:hover:bg-dark-700"
              :title="t('version.refresh')"
              :aria-label="t('version.refresh')"
              :disabled="loading || busy"
              @click="refreshVersions"
            >
              <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
            </button>
            <button type="button" class="rounded p-1.5 text-gray-500 hover:bg-gray-100 dark:hover:bg-dark-700"
              :title="t('common.close')" :aria-label="t('common.close')" @click="closeDropdown">
              <Icon name="x" size="sm" />
            </button>
          </div>
        </header>

        <div class="grid min-[640px]:grid-cols-2">
          <section
            v-for="source in sources"
            :key="source.key"
            :data-testid="`version-${source.key}`"
            class="min-w-0 space-y-3 p-4"
            :class="source.key === 'custom' ? 'border-t border-gray-100 min-[640px]:border-l min-[640px]:border-t-0 dark:border-dark-700' : ''"
          >
            <div>
              <div class="flex flex-wrap items-center justify-between gap-2">
                <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ source.label }}</h3>
                <span class="text-xs text-gray-500 dark:text-dark-400">
                  {{ t(source.key === 'custom' ? 'version.installSource' : 'version.reminderOnly') }}
                </span>
              </div>
              <a :href="`https://github.com/${source.repo}`" target="_blank" rel="noopener noreferrer"
                class="mt-1 block break-all text-xs text-primary-600 hover:underline dark:text-primary-400">
                {{ source.repo }}
              </a>
            </div>

            <div v-if="source.loading" class="flex min-h-24 items-center justify-center" :aria-label="t('common.loading')">
              <Icon name="refresh" size="md" class="animate-spin text-primary-500" />
            </div>
            <template v-else>
              <p class="break-all text-base font-semibold text-gray-900 dark:text-white">
                {{ source.latest ? `v${source.latest}` : '--' }}
              </p>
              <p class="text-xs" :class="source.warning || !source.loaded || source.hasUpdate
                ? 'text-amber-700 dark:text-amber-400' : 'text-green-700 dark:text-green-400'">
                {{ source.warning || !source.loaded ? t('version.checkUnavailable')
                  : source.hasUpdate ? t('version.updateAvailable') : t('version.upToDate') }}
              </p>
              <p v-if="source.warning" role="status" class="break-words text-xs text-amber-700 dark:text-amber-400">
                {{ source.warning }}
              </p>
              <p v-if="source.release?.body" class="line-clamp-3 whitespace-pre-line break-words text-xs leading-5 text-gray-500 dark:text-dark-400">
                {{ source.release.body }}
              </p>
              <a v-if="releaseURL(source.release?.html_url, source.repo)" :href="releaseURL(source.release?.html_url, source.repo)"
                target="_blank" rel="noopener noreferrer" class="inline-flex items-center gap-1 text-xs text-primary-600 hover:underline dark:text-primary-400">
                {{ t('version.viewChangelog') }} <Icon name="externalLink" size="xs" />
              </a>
            </template>

            <template v-if="source.key === 'custom'">
              <p v-if="operationError" role="alert" class="break-words text-xs text-red-600 dark:text-red-400">{{ operationError }}</p>
              <div v-if="updateSuccess" class="space-y-2 border-t border-gray-100 pt-3 dark:border-dark-700">
                <p class="text-sm text-green-700 dark:text-green-400">
                  {{ t(alreadyUpToDate ? 'version.upToDate' : successKind === 'rollback' ? 'version.rollbackComplete' : 'version.updateComplete') }}
                </p>
                <template v-if="needRestart">
                  <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('version.restartRequired') }}</p>
                  <button type="button" data-testid="restart-service" class="btn btn-primary w-full"
                    :disabled="busy" @click="handleRestart">
                    <Icon name="refresh" size="sm" :class="{ 'animate-spin': restarting }" />
                    {{ t(restarting ? 'version.restarting' : 'version.restartNow') }}
                    <span v-if="restarting" class="tabular-nums">({{ restartCountdown }}s)</span>
                  </button>
                </template>
              </div>
              <template v-else>
                <button v-if="source.hasUpdate && isReleaseBuild" type="button" data-testid="install-custom-update"
                  class="btn btn-primary w-full" :disabled="busy || source.loading || !!source.warning" @click="handleUpdate">
                  <Icon :name="updating ? 'refresh' : 'download'" size="sm" :class="{ 'animate-spin': updating }" />
                  {{ t(updating ? 'version.updating' : 'version.updateNow') }}
                </button>
                <p v-if="!isReleaseBuild" class="text-xs text-gray-500 dark:text-dark-400">{{ t('version.sourceModeHint') }}</p>
                <button type="button" data-testid="rollback-toggle" class="flex items-center gap-1.5 text-xs text-gray-600 dark:text-dark-300"
                  :disabled="busy" :aria-expanded="rollbackPanelOpen" @click="toggleRollbackPanel">
                  <Icon name="clock" size="sm" />{{ t('version.rollback') }}
                </button>
                <div v-if="rollbackPanelOpen" class="space-y-3 border-t border-gray-100 pt-3 dark:border-dark-700">
                  <p v-if="!isReleaseBuild" class="text-xs text-gray-500">{{ t('version.sourceModeHint') }}</p>
                  <Icon v-else-if="rollbackVersionsLoading" name="refresh" size="sm" class="animate-spin" />
                  <template v-else>
                    <p v-if="rollbackVersionsError" role="alert" class="break-words text-xs text-red-600">{{ rollbackVersionsError }}</p>
                    <p v-else-if="!rollbackVersions.length" class="text-xs text-gray-500">{{ t('version.noRollbackVersions') }}</p>
                    <label v-for="item in rollbackVersions" :key="item.version" class="flex items-center justify-between gap-2 text-xs">
                      <span class="flex min-w-0 items-center gap-2">
                        <input v-model="selectedRollbackVersion" type="radio" name="rollback-version" :value="item.version" :disabled="busy" />
                        <span class="break-all font-medium">v{{ item.version }}</span>
                      </span>
                      <span class="shrink-0 text-gray-400">{{ formatPublishedAt(item.published_at) }}</span>
                    </label>
                    <template v-if="selectedRollbackVersion">
                      <div class="flex items-center justify-between gap-1">
                        <div class="flex gap-1" role="tablist" :aria-label="t('version.manualRollbackCommand')">
                          <button v-for="tab in manualTabs" :key="tab.key" type="button" role="tab"
                            :aria-selected="manualTab === tab.key" class="rounded px-2 py-1 text-xs"
                            :class="manualTab === tab.key ? 'bg-gray-100 font-medium dark:bg-dark-700' : 'text-gray-500'"
                            @click="manualTab = tab.key">{{ tab.label }}</button>
                        </div>
                        <button type="button" class="rounded p-1.5" :title="t(copied ? 'version.copied' : 'version.copyCommand')"
                          :aria-label="t('version.copyCommand')" @click="copyToClipboard(activeManualCommand)">
                          <Icon :name="copied ? 'check' : 'copy'" size="xs" />
                        </button>
                      </div>
                      <code data-testid="rollback-command" class="block whitespace-pre-wrap break-all rounded bg-gray-50 p-2 text-[11px] dark:bg-dark-900">{{ activeManualCommand }}</code>
                      <p class="text-xs text-amber-700 dark:text-amber-400">{{ t('version.rollbackWarning') }}</p>
                      <button type="button" data-testid="confirm-rollback" class="btn btn-secondary w-full" :disabled="busy" @click="handleRollback">
                        <Icon :name="rollingBack ? 'refresh' : 'clock'" size="sm" :class="{ 'animate-spin': rollingBack }" />
                        {{ rollingBack ? t('version.rollingBack') : t('version.rollbackConfirm', { version: `v${selectedRollbackVersion}` }) }}
                      </button>
                    </template>
                  </template>
                </div>
              </template>
            </template>
          </section>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore, useAuthStore } from '@/stores'
import { getRollbackVersions, performUpdate, restartService, rollback as rollbackAPI, type RollbackVersionInfo } from '@/api/admin/system'
import { useClipboard } from '@/composables/useClipboard'
import Icon from '@/components/icons/Icon.vue'

const CUSTOM_REPO = 'h614626370-del/sub2api'
const UPSTREAM_REPO = 'ranxi2001/sub2api'
const props = defineProps<{ version?: string }>()
const { t } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()
const { copied, copyToClipboard } = useClipboard()
const isAdmin = computed(() => authStore.isAdmin)
const currentVersion = computed(() => appStore.currentVersion || props.version || '')
const isReleaseBuild = computed(() => appStore.buildType === 'release')
const loading = computed(() => appStore.versionLoading || appStore.upstreamVersionLoading)
const sources = computed(() => [
  { key: 'upstream', repo: UPSTREAM_REPO, label: t('version.upstreamRepository'),
    latest: appStore.upstreamVersionInfo?.latest_version, hasUpdate: appStore.upstreamVersionInfo?.has_update,
    loaded: !!appStore.upstreamVersionInfo, loading: appStore.upstreamVersionLoading,
    warning: appStore.upstreamVersionWarning, release: appStore.upstreamVersionInfo?.release_info },
  { key: 'custom', repo: CUSTOM_REPO, label: t('version.customRepository'),
    latest: appStore.latestVersion, hasUpdate: appStore.hasUpdate, loaded: appStore.versionLoaded,
    loading: appStore.versionLoading, warning: appStore.versionWarning, release: appStore.releaseInfo }
])
const hasAnyUpdate = computed(() => sources.value.some(source => source.hasUpdate && !source.warning))
const triggerRef = ref<HTMLElement | null>(null)
const dropdownRef = ref<HTMLElement | null>(null)
const dropdownOpen = ref(false)
const panelStyle = ref<Record<string, string>>({})
const updating = ref(false)
const rollingBack = ref(false)
const restarting = ref(false)
const busy = computed(() => updating.value || rollingBack.value || restarting.value)
const operationError = ref('')
const updateSuccess = ref(false)
const needRestart = ref(false)
const alreadyUpToDate = ref(false)
const successKind = ref<'update' | 'rollback'>('update')
const restartCountdown = ref(0)
const rollbackPanelOpen = ref(false)
const rollbackVersions = ref<RollbackVersionInfo[]>([])
const rollbackVersionsLoading = ref(false)
const rollbackVersionsError = ref('')
const selectedRollbackVersion = ref('')
const manualTab = ref<'script' | 'docker'>('script')
const manualTabs = computed(() => [
  { key: 'script' as const, label: t('version.deployScript') },
  { key: 'docker' as const, label: t('version.deployDocker') }
])
const activeManualCommand = computed(() => {
  const version = selectedRollbackVersion.value
  if (!version) return ''
  if (manualTab.value === 'docker') return `image: ghcr.io/${CUSTOM_REPO}:${version}\n\ndocker compose up -d`
  return `curl -fsSL https://raw.githubusercontent.com/${CUSTOM_REPO}/v${version}/deploy/install.sh | sudo env SUB2API_GITHUB_REPO=${CUSTOM_REPO} bash -s -- rollback v${version}`
})
let countdownTimer: ReturnType<typeof setInterval> | undefined
let disposed = false

function releaseURL(value: string | undefined, repo: string): string | undefined {
  if (!value) return undefined
  try {
    const url = new URL(value)
    return url.origin === 'https://github.com' && url.pathname.startsWith(`/${repo}/releases/`) ? url.href : undefined
  } catch { return undefined }
}

function positionPanel() {
  const rect = triggerRef.value?.getBoundingClientRect()
  if (!rect) return
  const width = Math.min(720, window.innerWidth - 24)
  const top = Math.min(rect.bottom + 8, Math.max(12, window.innerHeight - 160))
  panelStyle.value = {
    width: `${width}px`, left: `${Math.max(12, Math.min(rect.left, window.innerWidth - width - 12))}px`,
    top: `${top}px`, maxHeight: `${window.innerHeight - top - 12}px`
  }
}

async function toggleDropdown() {
  dropdownOpen.value = !dropdownOpen.value
  if (dropdownOpen.value) {
    positionPanel()
    await nextTick()
    dropdownRef.value?.querySelector<HTMLButtonElement>('button')?.focus()
  }
}
function closeDropdown() {
  dropdownOpen.value = false
  triggerRef.value?.focus()
}
async function refreshVersions() {
  if (!isAdmin.value || busy.value) return
  if (!needRestart.value) {
    operationError.value = ''
    updateSuccess.value = false
    rollbackPanelOpen.value = false
    selectedRollbackVersion.value = ''
    rollbackVersions.value = []
  }
  await Promise.all([appStore.fetchVersion(true), appStore.fetchUpstreamVersion(true)])
}
function errorMessage(error: unknown, fallback: string) {
  const err = error as { response?: { data?: { message?: string } }; message?: string }
  return err?.response?.data?.message || err?.message || t(fallback)
}
async function handleUpdate() {
  if (!isAdmin.value || busy.value || !isReleaseBuild.value || !appStore.hasUpdate || appStore.versionWarning) return
  updating.value = true
  operationError.value = ''
  try {
    const result = await performUpdate()
    successKind.value = 'update'
    updateSuccess.value = true
    alreadyUpToDate.value = result.already_up_to_date === true
    needRestart.value = result.need_restart === true
    appStore.clearVersionCache()
  } catch (error) { operationError.value = errorMessage(error, 'version.updateFailed') }
  finally { updating.value = false }
}
async function toggleRollbackPanel() {
  if (!isAdmin.value || busy.value) return
  rollbackPanelOpen.value = !rollbackPanelOpen.value
  if (!rollbackPanelOpen.value || !isReleaseBuild.value || rollbackVersionsLoading.value || rollbackVersions.value.length) return
  rollbackVersionsLoading.value = true
  rollbackVersionsError.value = ''
  try { rollbackVersions.value = (await getRollbackVersions()).versions || [] }
  catch (error) { rollbackVersionsError.value = errorMessage(error, 'version.loadVersionsFailed') }
  finally { rollbackVersionsLoading.value = false }
}
async function handleRollback() {
  if (!isAdmin.value || busy.value || !isReleaseBuild.value || !selectedRollbackVersion.value) return
  rollingBack.value = true
  operationError.value = ''
  try {
    const result = await rollbackAPI(selectedRollbackVersion.value)
    successKind.value = 'rollback'
    updateSuccess.value = true
    alreadyUpToDate.value = false
    needRestart.value = result.need_restart === true
    rollbackPanelOpen.value = false
    appStore.clearVersionCache()
  } catch (error) { operationError.value = errorMessage(error, 'version.rollbackFailed') }
  finally { rollingBack.value = false }
}
function formatPublishedAt(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '' : date.toLocaleDateString()
}
async function handleRestart() {
  if (!isAdmin.value || busy.value || !needRestart.value) return
  restarting.value = true
  restartCountdown.value = 8
  try { await restartService() } catch { /* Restart can disconnect the request. */ }
  if (disposed) return
  countdownTimer = setInterval(() => {
    if (--restartCountdown.value <= 0) {
      clearInterval(countdownTimer)
      void checkServiceAndReload()
    }
  }, 1000)
}
async function checkServiceAndReload() {
  for (let attempt = 0; attempt < 5 && !disposed; attempt++) {
    try {
      if ((await fetch('/health', { cache: 'no-cache' })).ok) break
    } catch { /* Wait for the restarted service. */ }
    await new Promise(resolve => setTimeout(resolve, 1000))
  }
  if (!disposed) window.location.reload()
}
function handleClickOutside(event: MouseEvent) {
  const target = event.target as Node
  if (dropdownOpen.value && !dropdownRef.value?.contains(target) && !triggerRef.value?.contains(target)) dropdownOpen.value = false
}
function handleKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape' && dropdownOpen.value) closeDropdown()
}
onMounted(() => {
  if (isAdmin.value) void Promise.all([appStore.fetchVersion(false), appStore.fetchUpstreamVersion(false)])
  document.addEventListener('click', handleClickOutside)
  document.addEventListener('keydown', handleKeydown)
  window.addEventListener('resize', positionPanel)
})
onBeforeUnmount(() => {
  disposed = true
  clearInterval(countdownTimer)
  document.removeEventListener('click', handleClickOutside)
  document.removeEventListener('keydown', handleKeydown)
  window.removeEventListener('resize', positionPanel)
})
</script>
