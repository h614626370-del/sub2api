<template>
  <div class="relative">
    <template v-if="isAdmin">
      <button
        ref="triggerRef"
        type="button"
        class="flex items-center gap-1.5 rounded-lg px-2 py-1 text-xs transition-colors"
        :class="
          hasAnyUpdate
            ? 'bg-amber-100 text-amber-700 hover:bg-amber-200 dark:bg-amber-900/30 dark:text-amber-400 dark:hover:bg-amber-900/50'
            : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-800 dark:text-dark-400 dark:hover:bg-dark-700'
        "
        :title="hasAnyUpdate ? t('version.updateAvailable') : t('version.upToDate')"
        @click="toggleDropdown"
      >
        <span v-if="currentVersion" class="font-medium">v{{ currentVersion }}</span>
        <span v-else class="h-3 w-12 animate-pulse rounded bg-gray-200 dark:bg-dark-600"></span>
        <span v-if="hasAnyUpdate" class="relative flex h-2 w-2">
          <span
            class="absolute inline-flex h-full w-full animate-ping rounded-full bg-amber-400 opacity-75"
          ></span>
          <span class="relative inline-flex h-2 w-2 rounded-full bg-amber-500"></span>
        </span>
      </button>

      <transition name="dropdown">
        <div
          v-if="dropdownOpen"
          ref="dropdownRef"
          class="absolute left-0 z-50 mt-2 max-h-[calc(100vh-5rem)] w-[calc(100vw-2rem)] max-w-3xl overflow-y-auto whitespace-normal rounded-lg border border-gray-200 bg-white shadow-xl dark:border-dark-700 dark:bg-dark-800"
        >
          <div
            class="flex items-center justify-between border-b border-gray-100 px-4 py-3 dark:border-dark-700"
          >
            <div class="min-w-0">
              <p class="text-xs text-gray-400 dark:text-dark-500">{{ t('version.currentVersion') }}</p>
              <p class="truncate text-sm font-semibold text-gray-800 dark:text-dark-100">
                {{ currentVersion ? `v${currentVersion}` : '--' }}
              </p>
            </div>
            <button
              type="button"
              class="rounded-lg p-1.5 text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-600 disabled:cursor-not-allowed disabled:opacity-50 dark:hover:bg-dark-700 dark:hover:text-dark-200"
              :disabled="loading"
              :title="t('version.refresh')"
              @click="refreshVersions(true)"
            >
              <Icon
                name="refresh"
                size="sm"
                :stroke-width="2"
                :class="{ 'animate-spin': loading }"
              />
            </button>
          </div>

          <div class="grid md:grid-cols-2">
            <section class="p-4 md:border-r md:border-gray-100 md:dark:border-dark-700">
              <div class="mb-4 flex items-start justify-between gap-3">
                <div>
                  <h3 class="text-sm font-semibold text-gray-900 dark:text-white">
                    {{ t('version.officialVersion') }}
                  </h3>
                  <p class="mt-0.5 text-xs text-gray-500 dark:text-dark-400">
                    Wei-Shaw/sub2api
                  </p>
                </div>
                <span
                  class="rounded-md bg-gray-100 px-2 py-1 text-[11px] font-medium text-gray-500 dark:bg-dark-700 dark:text-dark-300"
                >
                  {{ t('version.reminderOnly') }}
                </span>
              </div>

              <div v-if="officialLoading" class="flex min-h-40 items-center justify-center">
                <Icon name="refresh" size="md" class="animate-spin text-primary-500" />
              </div>
              <div v-else class="space-y-3">
                <p
                  v-if="officialWarning"
                  class="rounded-lg border border-amber-200 bg-amber-50 p-2.5 text-xs leading-5 text-amber-700 dark:border-amber-800/50 dark:bg-amber-900/20 dark:text-amber-300"
                >
                  {{ t('version.checkWarning') }}: {{ officialWarning }}
                </p>
                <div
                  class="rounded-lg border p-3"
                  :class="
                    officialWarning
                      ? 'border-amber-200 bg-amber-50 dark:border-amber-800/50 dark:bg-amber-900/20'
                      : officialHasUpdate
                      ? 'border-amber-200 bg-amber-50 dark:border-amber-800/50 dark:bg-amber-900/20'
                      : 'border-green-200 bg-green-50 dark:border-green-800/50 dark:bg-green-900/20'
                  "
                >
                  <div class="flex items-center justify-between gap-3">
                    <span class="text-xs text-gray-500 dark:text-dark-400">
                      {{ t('version.latestVersion') }}
                    </span>
                    <span
                      class="text-base font-semibold"
                      :class="
                        officialWarning
                          ? 'text-amber-700 dark:text-amber-300'
                          : officialHasUpdate
                          ? 'text-amber-700 dark:text-amber-300'
                          : 'text-green-700 dark:text-green-300'
                      "
                    >
                      {{ officialLatestVersion ? `v${officialLatestVersion}` : '--' }}
                    </span>
                  </div>
                  <p
                    class="mt-2 text-xs leading-5"
                    :class="
                      officialWarning
                        ? 'text-amber-700 dark:text-amber-300'
                        : officialHasUpdate
                        ? 'text-amber-700 dark:text-amber-300'
                        : 'text-green-700 dark:text-green-300'
                    "
                  >
                    {{
                      officialWarning
                        ? t('version.checkUnavailable')
                        : officialHasUpdate
                        ? t('version.officialUpdateDetected')
                        : t('version.officialUpToDate')
                    }}
                  </p>
                </div>

                <p
                  v-if="officialReleaseInfo?.body"
                  class="line-clamp-3 whitespace-pre-line text-xs leading-5 text-gray-500 dark:text-dark-400"
                >
                  {{ officialReleaseInfo.body }}
                </p>

                <a
                  v-if="officialReleaseInfo?.html_url && officialReleaseInfo.html_url !== '#'"
                  :href="officialReleaseInfo.html_url"
                  target="_blank"
                  rel="noopener noreferrer"
                  class="flex items-center justify-center gap-1.5 rounded-lg border border-gray-200 px-3 py-2 text-xs font-medium text-gray-600 transition-colors hover:bg-gray-50 dark:border-dark-600 dark:text-dark-300 dark:hover:bg-dark-700"
                >
                  {{ t('version.viewOfficialRelease') }}
                  <Icon name="externalLink" size="xs" :stroke-width="2" />
                </a>
              </div>
            </section>

            <section class="border-t border-gray-100 p-4 dark:border-dark-700 md:border-t-0">
              <div class="mb-4 flex items-start justify-between gap-3">
                <div>
                  <h3 class="text-sm font-semibold text-gray-900 dark:text-white">
                    {{ t('version.customVersion') }}
                  </h3>
                  <p class="mt-0.5 text-xs text-gray-500 dark:text-dark-400">
                    h614626370-del/sub2api
                  </p>
                </div>
                <span
                  class="rounded-md bg-primary-50 px-2 py-1 text-[11px] font-medium text-primary-600 dark:bg-primary-900/20 dark:text-primary-300"
                >
                  {{ t('version.installSource') }}
                </span>
              </div>

              <div v-if="customLoading" class="flex min-h-40 items-center justify-center">
                <Icon name="refresh" size="md" class="animate-spin text-primary-500" />
              </div>
              <div v-else class="space-y-3">
                <p
                  v-if="customWarning"
                  class="rounded-lg border border-amber-200 bg-amber-50 p-2.5 text-xs leading-5 text-amber-700 dark:border-amber-800/50 dark:bg-amber-900/20 dark:text-amber-300"
                >
                  {{ t('version.checkWarning') }}: {{ customWarning }}
                </p>
                <div v-if="updateError" class="space-y-2">
                  <div
                    class="rounded-lg border border-red-200 bg-red-50 p-3 dark:border-red-800/50 dark:bg-red-900/20"
                  >
                    <p class="text-sm font-medium text-red-700 dark:text-red-300">
                      {{ t('version.updateFailed') }}
                    </p>
                    <p class="mt-1 break-words text-xs text-red-600 dark:text-red-400">
                      {{ updateError }}
                    </p>
                  </div>
                  <button
                    type="button"
                    class="w-full rounded-lg bg-red-500 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-red-600 disabled:opacity-50"
                    :disabled="updating"
                    @click="handleUpdate"
                  >
                    {{ t('version.retry') }}
                  </button>
                </div>

                <div v-else-if="updateSuccess && needRestart" class="space-y-2">
                  <div
                    class="rounded-lg border border-green-200 bg-green-50 p-3 dark:border-green-800/50 dark:bg-green-900/20"
                  >
                    <p class="text-sm font-medium text-green-700 dark:text-green-300">
                      {{
                        successKind === 'rollback'
                          ? t('version.rollbackComplete')
                          : t('version.updateComplete')
                      }}
                    </p>
                    <p class="mt-1 text-xs text-green-600 dark:text-green-400">
                      {{ t('version.restartRequired') }}
                    </p>
                  </div>
                  <button
                    type="button"
                    class="flex w-full items-center justify-center gap-2 rounded-lg bg-green-500 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-green-600 disabled:opacity-50"
                    :disabled="restarting"
                    @click="handleRestart"
                  >
                    <Icon
                      name="refresh"
                      size="sm"
                      :stroke-width="2"
                      :class="{ 'animate-spin': restarting }"
                    />
                    <span>{{ restarting ? t('version.restarting') : t('version.restartNow') }}</span>
                    <span v-if="restarting && restartCountdown > 0" class="tabular-nums">
                      ({{ restartCountdown }}s)
                    </span>
                  </button>
                </div>

                <template v-else>
                  <div
                    class="rounded-lg border p-3"
                    :class="
                      customWarning
                        ? 'border-amber-200 bg-amber-50 dark:border-amber-800/50 dark:bg-amber-900/20'
                        : customHasUpdate
                        ? 'border-primary-200 bg-primary-50 dark:border-primary-800/50 dark:bg-primary-900/20'
                        : 'border-green-200 bg-green-50 dark:border-green-800/50 dark:bg-green-900/20'
                    "
                  >
                    <div class="flex items-center justify-between gap-3">
                      <span class="text-xs text-gray-500 dark:text-dark-400">
                        {{ t('version.latestVersion') }}
                      </span>
                      <span
                        class="text-base font-semibold"
                        :class="
                          customWarning
                            ? 'text-amber-700 dark:text-amber-300'
                            : customHasUpdate
                            ? 'text-primary-700 dark:text-primary-300'
                            : 'text-green-700 dark:text-green-300'
                        "
                      >
                        {{ customLatestVersion ? `v${customLatestVersion}` : '--' }}
                      </span>
                    </div>
                    <p
                      class="mt-2 text-xs leading-5"
                      :class="
                        customWarning
                          ? 'text-amber-700 dark:text-amber-300'
                          : customHasUpdate
                          ? 'text-primary-700 dark:text-primary-300'
                          : 'text-green-700 dark:text-green-300'
                      "
                    >
                      {{
                        customWarning
                          ? t('version.checkUnavailable')
                          : customHasUpdate
                          ? t('version.customUpdateReady')
                          : t('version.customUpToDate')
                      }}
                    </p>
                  </div>

                  <p
                    v-if="customHasUpdate && !isReleaseBuild"
                    class="rounded-lg border border-blue-200 bg-blue-50 p-3 text-xs leading-5 text-blue-600 dark:border-blue-800/50 dark:bg-blue-900/20 dark:text-blue-400"
                  >
                    {{ t('version.customSourceModeHint') }}
                  </p>

                  <button
                    v-else-if="customHasUpdate && isReleaseBuild"
                    type="button"
                    class="flex w-full items-center justify-center gap-2 rounded-lg bg-primary-500 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-primary-600 disabled:cursor-not-allowed disabled:opacity-50"
                    :disabled="updating"
                    @click="handleUpdate"
                  >
                    <Icon
                      :name="updating ? 'refresh' : 'download'"
                      size="sm"
                      :stroke-width="2"
                      :class="{ 'animate-spin': updating }"
                    />
                    {{ updating ? t('version.updating') : t('version.updateFromCustom') }}
                  </button>

                  <a
                    v-if="customReleaseInfo?.html_url && customReleaseInfo.html_url !== '#'"
                    :href="customReleaseInfo.html_url"
                    target="_blank"
                    rel="noopener noreferrer"
                    class="flex items-center justify-center gap-1.5 text-xs text-gray-500 transition-colors hover:text-gray-700 dark:text-dark-400 dark:hover:text-dark-200"
                  >
                    {{ t('version.viewCustomRelease') }}
                    <Icon name="externalLink" size="xs" :stroke-width="2" />
                  </a>

                  <div class="border-t border-gray-100 pt-2 dark:border-dark-700">
                    <button
                      type="button"
                      class="flex w-full items-center justify-between rounded-lg px-2 py-1.5 text-xs text-gray-400 transition-colors hover:bg-gray-50 hover:text-gray-600 dark:text-dark-500 dark:hover:bg-dark-700/50 dark:hover:text-dark-300"
                      @click="toggleRollbackPanel"
                    >
                      <span class="flex items-center gap-1.5">
                        <Icon name="clock" size="xs" :stroke-width="2" />
                        {{ t('version.rollback') }}
                      </span>
                      <Icon
                        name="chevronDown"
                        size="xs"
                        :stroke-width="2"
                        class="transition-transform"
                        :class="{ 'rotate-180': rollbackPanelOpen }"
                      />
                    </button>

                    <transition name="rollback">
                      <div v-if="rollbackPanelOpen" class="mt-2 space-y-2">
                        <p
                          v-if="!isReleaseBuild"
                          class="rounded-lg border border-blue-200 bg-blue-50 p-2 text-xs text-blue-600 dark:border-blue-800/50 dark:bg-blue-900/20 dark:text-blue-400"
                        >
                          {{ t('version.rollbackSourceHint') }}
                        </p>
                        <div
                          v-else-if="rollbackVersionsLoading"
                          class="flex items-center justify-center py-4"
                        >
                          <Icon name="refresh" size="sm" class="animate-spin text-primary-500" />
                        </div>
                        <div v-else-if="rollbackVersionsError" class="space-y-2">
                          <p
                            class="rounded-lg border border-red-200 bg-red-50 p-2 text-xs text-red-600 dark:border-red-800/50 dark:bg-red-900/20 dark:text-red-400"
                          >
                            {{ rollbackVersionsError }}
                          </p>
                          <button
                            type="button"
                            class="w-full rounded-lg border border-gray-200 py-1.5 text-xs text-gray-500 hover:bg-gray-50 dark:border-dark-700 dark:text-dark-400 dark:hover:bg-dark-700"
                            @click="loadRollbackVersions"
                          >
                            {{ t('version.retry') }}
                          </button>
                        </div>
                        <p
                          v-else-if="rollbackVersions.length === 0"
                          class="py-3 text-center text-xs text-gray-400 dark:text-dark-500"
                        >
                          {{ t('version.noRollbackVersions') }}
                        </p>
                        <template v-else>
                          <p class="text-[11px] text-gray-400 dark:text-dark-500">
                            {{ t('version.rollbackSelectVersion') }}
                          </p>
                          <button
                            v-for="item in rollbackVersions"
                            :key="item.version"
                            type="button"
                            class="flex w-full items-center justify-between rounded-lg border px-3 py-2 text-left transition-colors disabled:opacity-50"
                            :class="
                              selectedRollbackVersion === item.version
                                ? 'border-amber-300 bg-amber-50 dark:border-amber-700 dark:bg-amber-900/20'
                                : 'border-gray-200 hover:bg-gray-50 dark:border-dark-700 dark:hover:bg-dark-700/40'
                            "
                            :disabled="rollingBack"
                            @click="selectRollbackVersion(item.version)"
                          >
                            <span class="text-sm font-semibold text-gray-700 dark:text-dark-200">
                              v{{ item.version }}
                            </span>
                            <span class="text-[11px] text-gray-400 dark:text-dark-500">
                              {{ formatPublishedAt(item.published_at) }}
                            </span>
                          </button>

                          <div v-if="selectedRollbackVersion" class="space-y-2 pt-1">
                            <p class="text-[11px] text-gray-400 dark:text-dark-500">
                              {{ t('version.manualRollbackCommand') }}
                            </p>
                            <div
                              class="overflow-hidden rounded-lg border border-gray-200 dark:border-dark-600"
                            >
                              <div
                                class="flex items-center justify-between border-b border-gray-200 bg-gray-100 px-2 py-1.5 dark:border-dark-600 dark:bg-dark-700"
                              >
                                <span class="text-[11px] text-gray-500 dark:text-dark-300">
                                  {{ t('version.deployScript') }}
                                </span>
                                <button
                                  type="button"
                                  class="flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] text-gray-400 hover:bg-gray-200 dark:hover:bg-dark-600"
                                  @click="copyToClipboard(scriptRollbackCommand)"
                                >
                                  <Icon :name="copied ? 'check' : 'copy'" size="xs" />
                                  {{ copied ? t('version.copied') : t('version.copyCommand') }}
                                </button>
                              </div>
                              <code
                                class="block select-all whitespace-pre-wrap break-all bg-gray-50 p-2.5 font-mono text-[10px] leading-relaxed text-gray-600 dark:bg-dark-900 dark:text-dark-300"
                                >{{ scriptRollbackCommand }}</code
                              >
                            </div>
                            <p class="text-[11px] leading-4 text-amber-600 dark:text-amber-400">
                              {{ t('version.rollbackWarning') }}
                            </p>
                            <p
                              v-if="rollbackError"
                              class="rounded-lg border border-red-200 bg-red-50 p-2 text-xs text-red-600 dark:border-red-800/50 dark:bg-red-900/20 dark:text-red-400"
                            >
                              {{ rollbackError }}
                            </p>
                            <button
                              type="button"
                              class="flex w-full items-center justify-center gap-2 rounded-lg bg-amber-500 px-4 py-2 text-sm font-medium text-white hover:bg-amber-600 disabled:opacity-50"
                              :disabled="rollingBack"
                              @click="handleRollback"
                            >
                              <Icon
                                :name="rollingBack ? 'refresh' : 'clock'"
                                size="sm"
                                :class="{ 'animate-spin': rollingBack }"
                              />
                              {{
                                rollingBack
                                  ? t('version.rollingBack')
                                  : t('version.rollbackConfirm', {
                                      version: `v${selectedRollbackVersion}`
                                    })
                              }}
                            </button>
                          </div>
                        </template>
                      </div>
                    </transition>
                  </div>
                </template>
              </div>
            </section>
          </div>
        </div>
      </transition>
    </template>

    <span v-else-if="version" class="text-xs text-gray-500 dark:text-dark-400">
      v{{ version }}
    </span>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore, useAuthStore } from '@/stores'
import {
  getRollbackVersions,
  performUpdate,
  restartService,
  rollback as rollbackAPI,
  type RollbackVersionInfo
} from '@/api/admin/system'
import { useClipboard } from '@/composables/useClipboard'
import Icon from '@/components/icons/Icon.vue'

const CUSTOM_GITHUB_REPO = 'h614626370-del/sub2api'

const props = defineProps<{
  version?: string
}>()

const { t } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()
const { copied, copyToClipboard } = useClipboard()

const isAdmin = computed(() => authStore.isAdmin)
const currentVersion = computed(() => appStore.currentVersion || props.version || '')
const officialLoading = computed(() => appStore.versionLoading)
const officialLatestVersion = computed(() => appStore.latestVersion)
const officialHasUpdate = computed(() => appStore.hasUpdate)
const officialReleaseInfo = computed(() => appStore.releaseInfo)
const officialWarning = computed(() => appStore.versionWarning)
const customLoading = computed(() => appStore.customVersionLoading)
const customLatestVersion = computed(() => appStore.customLatestVersion)
const customHasUpdate = computed(() => appStore.customHasUpdate)
const customReleaseInfo = computed(() => appStore.customReleaseInfo)
const customWarning = computed(() => appStore.customVersionWarning)
const loading = computed(() => officialLoading.value || customLoading.value)
const hasAnyUpdate = computed(() => officialHasUpdate.value || customHasUpdate.value)
const isReleaseBuild = computed(() => appStore.buildType === 'release')

const dropdownOpen = ref(false)
const dropdownRef = ref<HTMLElement | null>(null)
const triggerRef = ref<HTMLElement | null>(null)

const updating = ref(false)
const restarting = ref(false)
const needRestart = ref(false)
const updateError = ref('')
const updateSuccess = ref(false)
const restartCountdown = ref(0)
const successKind = ref<'update' | 'rollback'>('update')

const rollbackPanelOpen = ref(false)
const rollbackVersions = ref<RollbackVersionInfo[]>([])
const rollbackVersionsLoading = ref(false)
const rollbackVersionsError = ref('')
const selectedRollbackVersion = ref('')
const rollingBack = ref(false)
const rollbackError = ref('')

const scriptRollbackCommand = computed(() => {
  if (!selectedRollbackVersion.value) return ''
  const tag = `v${selectedRollbackVersion.value}`
  return `curl -sSL https://raw.githubusercontent.com/${CUSTOM_GITHUB_REPO}/${tag}/deploy/install.sh | sudo env GITHUB_REPO=${CUSTOM_GITHUB_REPO} bash -s -- rollback ${tag}`
})

function toggleDropdown() {
  dropdownOpen.value = !dropdownOpen.value
}

function resetOperationState() {
  updateError.value = ''
  updateSuccess.value = false
  needRestart.value = false
  rollbackPanelOpen.value = false
  rollbackVersions.value = []
  rollbackVersionsError.value = ''
  selectedRollbackVersion.value = ''
  rollbackError.value = ''
}

async function refreshVersions(force = true) {
  if (!isAdmin.value) return
  resetOperationState()
  await Promise.all([appStore.fetchVersion(force), appStore.fetchCustomVersion(force)])
}

async function handleUpdate() {
  if (updating.value || !customHasUpdate.value || !isReleaseBuild.value) return

  updating.value = true
  updateError.value = ''
  updateSuccess.value = false
  try {
    const result = await performUpdate()
    successKind.value = 'update'
    updateSuccess.value = true
    needRestart.value = result.need_restart
    appStore.clearCustomVersionCache()
  } catch (error: unknown) {
    const err = error as { response?: { data?: { message?: string } }; message?: string }
    updateError.value = err.response?.data?.message || err.message || t('version.updateFailed')
  } finally {
    updating.value = false
  }
}

async function toggleRollbackPanel() {
  rollbackPanelOpen.value = !rollbackPanelOpen.value
  if (
    rollbackPanelOpen.value &&
    isReleaseBuild.value &&
    rollbackVersions.value.length === 0 &&
    !rollbackVersionsLoading.value
  ) {
    await loadRollbackVersions()
  }
}

async function loadRollbackVersions() {
  rollbackVersionsLoading.value = true
  rollbackVersionsError.value = ''
  try {
    const data = await getRollbackVersions()
    rollbackVersions.value = data.versions || []
  } catch (error: unknown) {
    const err = error as { response?: { data?: { message?: string } }; message?: string }
    rollbackVersionsError.value =
      err.response?.data?.message || err.message || t('version.loadVersionsFailed')
  } finally {
    rollbackVersionsLoading.value = false
  }
}

function selectRollbackVersion(version: string) {
  if (rollingBack.value) return
  rollbackError.value = ''
  selectedRollbackVersion.value = selectedRollbackVersion.value === version ? '' : version
}

function formatPublishedAt(publishedAt: string): string {
  if (!publishedAt) return ''
  const date = new Date(publishedAt)
  return Number.isNaN(date.getTime()) ? '' : date.toLocaleDateString()
}

async function handleRollback() {
  if (rollingBack.value || !selectedRollbackVersion.value) return

  rollingBack.value = true
  rollbackError.value = ''
  try {
    const result = await rollbackAPI(selectedRollbackVersion.value)
    successKind.value = 'rollback'
    updateSuccess.value = true
    needRestart.value = result.need_restart
    rollbackPanelOpen.value = false
    appStore.clearCustomVersionCache()
  } catch (error: unknown) {
    const err = error as { response?: { data?: { message?: string } }; message?: string }
    rollbackError.value = err.response?.data?.message || err.message || t('version.rollbackFailed')
  } finally {
    rollingBack.value = false
  }
}

async function handleRestart() {
  if (restarting.value) return

  restarting.value = true
  restartCountdown.value = 8
  try {
    await restartService()
  } catch {
    // The request normally disconnects while the service restarts.
  }

  const countdownInterval = window.setInterval(() => {
    restartCountdown.value--
    if (restartCountdown.value <= 0) {
      window.clearInterval(countdownInterval)
      void checkServiceAndReload()
    }
  }, 1000)
}

async function checkServiceAndReload() {
  for (let attempt = 0; attempt < 5; attempt++) {
    try {
      const response = await fetch('/health', { method: 'GET', cache: 'no-cache' })
      if (response.ok) {
        window.location.reload()
        return
      }
    } catch {
      // Service is not ready yet.
    }
    await new Promise((resolve) => window.setTimeout(resolve, 1000))
  }
  window.location.reload()
}

function handleClickOutside(event: MouseEvent) {
  const target = event.target as Node
  if (
    dropdownRef.value &&
    !dropdownRef.value.contains(target) &&
    !triggerRef.value?.contains(target)
  ) {
    dropdownOpen.value = false
  }
}

onMounted(() => {
  if (isAdmin.value) {
    void Promise.all([appStore.fetchVersion(false), appStore.fetchCustomVersion(false)])
  }
  document.addEventListener('click', handleClickOutside)
})

onBeforeUnmount(() => {
  document.removeEventListener('click', handleClickOutside)
})
</script>

<style scoped>
.dropdown-enter-active,
.dropdown-leave-active,
.rollback-enter-active,
.rollback-leave-active {
  transition: all 0.2s ease;
}

.dropdown-enter-from,
.dropdown-leave-to {
  opacity: 0;
  transform: scale(0.98) translateY(-4px);
}

.rollback-enter-from,
.rollback-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}

.line-clamp-3 {
  display: -webkit-box;
  overflow: hidden;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 3;
}
</style>
