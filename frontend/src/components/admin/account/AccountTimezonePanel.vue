<template>
  <section class="space-y-5 border-t border-gray-200 pt-6 dark:border-dark-700" aria-labelledby="account-timezone-title">
    <h2 id="account-timezone-title" class="text-lg font-semibold text-gray-900 dark:text-white">{{ t(`${key}.title`) }}</h2>
    <form class="space-y-3" :aria-busy="settingsLoading || saving" @submit.prevent="saveDefault">
      <div class="flex flex-wrap items-end gap-3">
        <div class="w-full min-w-0 sm:max-w-lg sm:flex-1">
          <label for="timezone-default" class="mb-2 block text-sm font-medium">{{ t(`${key}.defaultTimezone`) }}</label>
          <Select
            id="timezone-default"
            v-model="defaultTimezone"
            :options="timezoneOptions"
            searchable
            :aria-label="t(`${key}.defaultTimezone`)"
            :search-placeholder="t(`${key}.searchTimezone`)"
            :disabled="settingsLoading || saving || settingsLoadFailed"
            @update:model-value="saved = false"
          />
        </div>
        <button type="submit" class="btn btn-primary" :disabled="settingsLoading || saving || settingsLoadFailed || busy">
          <Icon name="check" size="sm" class="mr-1.5" />
          {{ saving ? t('common.saving') : t(`${key}.saveDefault`) }}
        </button>
      </div>
      <p v-if="settingsLoading" role="status" class="text-sm text-gray-600 dark:text-gray-300">{{ t('common.loading') }}</p>
      <p v-if="settingsError" role="alert" class="text-sm text-red-700 dark:text-red-300">
        {{ settingsError }}
        <button v-if="settingsLoadFailed" type="button" class="ml-2 underline" @click="loadDefault">{{ t('common.refresh') }}</button>
      </p>
      <p v-if="saved" role="status" class="text-sm text-emerald-700 dark:text-emerald-300">{{ t(`${key}.saved`) }}</p>
    </form>

    <details class="border-t border-gray-200 dark:border-dark-700" @toggle="onDetailsToggle">
      <summary class="cursor-pointer py-4 text-sm font-semibold focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500">
        {{ t(`${key}.detailsTitle`) }}
      </summary>
      <div class="space-y-4 pb-2">
        <div class="flex flex-wrap items-end gap-3">
          <div class="min-w-0 flex-1 sm:max-w-sm">
            <label for="timezone-search" class="block text-sm font-medium">{{ t(`${key}.search`) }}</label>
            <input id="timezone-search" v-model="search" type="search" class="input mt-2 w-full" @keydown.enter.prevent="searchAccounts" />
          </div>
          <button type="button" class="btn btn-secondary" :disabled="listLoading || busy || saving" @click="searchAccounts">
            <Icon name="search" size="sm" class="mr-1.5" />{{ t(`${key}.searchAction`) }}
          </button>
        </div>
        <p v-if="listError" role="alert" class="text-sm text-red-700 dark:text-red-300">{{ t(`${key}.listFailed`) }}</p>
        <div class="max-w-xl">
          <label for="timezone-account" class="block text-sm font-medium">{{ t(`${key}.account`) }}</label>
          <select id="timezone-account" v-model.number="accountID" class="input mt-2 w-full" :disabled="busy || listLoading || saving" @change="selectAccount">
            <option :value="0">{{ listLoading ? t('common.loading') : t(`${key}.choose`) }}</option>
            <option v-for="account in accounts" :key="account.id" :value="account.id">{{ account.name }} (#{{ account.id }})</option>
          </select>
          <div class="mt-2 flex flex-wrap items-center gap-3 text-sm">
            <button type="button" class="btn btn-secondary" :disabled="page <= 1 || listLoading || busy || saving" @click="loadAccounts(page - 1)">{{ t(`${key}.previous`) }}</button>
            <span>{{ t(`${key}.page`, { page, total }) }}</span>
            <button type="button" class="btn btn-secondary" :disabled="page * 50 >= total || listLoading || busy || saving" @click="loadAccounts(page + 1)">{{ t(`${key}.next`) }}</button>
          </div>
          <p v-if="!listLoading && !listError && !accounts.length" class="mt-2 text-sm text-gray-600 dark:text-gray-300">{{ t(`${key}.empty`) }}</p>
        </div>
        <div v-if="accountID" class="space-y-4 border-t border-gray-200 pt-5 dark:border-dark-600" :aria-busy="busy">
          <p v-if="busy" role="status" class="text-sm text-gray-600 dark:text-gray-300">{{ t(`${key}.working`) }}</p>
          <p v-if="error" role="alert" class="text-sm text-red-700 dark:text-red-300">{{ error }}</p>
          <button v-if="!state && !busy" type="button" class="btn btn-secondary" @click="selectAccount">{{ t('common.refresh') }}</button>
          <template v-if="state">
            <dl class="grid gap-x-6 gap-y-4 text-sm sm:grid-cols-2">
              <div><dt class="text-gray-600 dark:text-gray-300">{{ t(`${key}.effective`) }}</dt><dd class="mt-1 break-words font-medium">{{ state.timezone || t(`${key}.unchanged`) }}</dd></div>
              <div><dt class="text-gray-600 dark:text-gray-300">{{ t(`${key}.source`) }}</dt><dd class="mt-1">{{ t(`${key}.source${state.source}`) }}</dd></div>
              <div><dt class="text-gray-600 dark:text-gray-300">{{ t(`${key}.detected`) }}</dt><dd class="mt-1 break-words font-mono">{{ state.detected_timezone || '—' }}<span v-if="state.ip"> · {{ state.ip }}</span></dd></div>
              <div><dt class="text-gray-600 dark:text-gray-300">{{ t(`${key}.detectedAt`) }}</dt><dd class="mt-1">{{ state.detected_at ? formatDateTime(state.detected_at) : '—' }}</dd></div>
            </dl>
            <p v-if="state.stale" class="text-sm text-amber-800 dark:text-amber-200">{{ t(`${key}.stale`) }}</p>
            <p v-if="!state.has_proxy" class="text-sm text-gray-600 dark:text-gray-300">{{ t(`${key}.noProxy`) }}</p>
            <button type="button" class="btn btn-secondary" :disabled="busy || saving || !state.has_proxy" @click="detect">
              <Icon name="refresh" size="sm" class="mr-1.5" />{{ t(`${key}.detectAction`) }}
            </button>
          </template>
        </div>
      </div>
    </details>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { list } from '@/api/admin/accounts'
import { getSettings, updateSettings } from '@/api/admin/settings'
import { getAccountTimezone, detectAccountTimezone, type AccountTimezoneState } from '@/api/admin/accountTimezone'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { formatDateTime } from '@/utils/format'
import type { AccountListItem } from '@/types'

const { t } = useI18n()
const key = 'admin.accounts.accountTimezone'
const defaultTimezone = ref('')
const settingsLoading = ref(true)
const settingsLoadFailed = ref(false)
const settingsError = ref('')
const saving = ref(false)
const saved = ref(false)
const accounts = ref<AccountListItem[]>([])
const search = ref('')
const page = ref(1)
const total = ref(0)
const accountID = ref(0)
const listLoading = ref(false)
const listError = ref(false)
const listLoaded = ref(false)
const busy = ref(false)
const error = ref('')
const state = ref<AccountTimezoneState | null>(null)
const commonZones = [
  ['America/Los_Angeles', 'losAngeles'], ['America/New_York', 'newYork'],
  ['America/Chicago', 'chicago'], ['Europe/London', 'london'], ['Europe/Berlin', 'berlin'],
  ['Asia/Tokyo', 'tokyo'], ['Asia/Singapore', 'singapore'], ['Asia/Shanghai', 'shanghai'],
  ['Asia/Hong_Kong', 'hongKong'], ['Australia/Sydney', 'sydney'], ['UTC', 'utc'],
] as const
const supportedZones = (Intl as typeof Intl & { supportedValuesOf?: (key: string) => string[] }).supportedValuesOf?.('timeZone') ?? []
const timezoneOptions = computed(() => {
  const options = [
    { value: '', label: t(`${key}.unchanged`) },
    ...commonZones.map(([value, city]) => ({ value, label: `${t(`${key}.cities.${city}`)} · ${value}` })),
  ]
  const known = new Set(options.map(option => option.value))
  for (const value of [...supportedZones, defaultTimezone.value]) {
    if (value && !known.has(value)) {
      options.push({ value, label: value })
      known.add(value)
    }
  }
  return options
})
let revision = 0
let listRevision = 0
let disposed = false
onBeforeUnmount(() => { disposed = true; revision++; listRevision++ })

async function loadDefault() {
  settingsLoading.value = true
  settingsLoadFailed.value = false
  settingsError.value = ''
  try {
    const settings = await getSettings()
    if (!disposed) defaultTimezone.value = settings.openai_oauth_default_timezone || ''
  } catch {
    if (!disposed) {
      settingsLoadFailed.value = true
      settingsError.value = t(`${key}.settingsLoadFailed`)
    }
  } finally { if (!disposed) settingsLoading.value = false }
}

async function saveDefault() {
  if (settingsLoading.value || settingsLoadFailed.value || saving.value || busy.value) return
  saving.value = true
  saved.value = false
  settingsError.value = ''
  // A detection started under the old default must not overwrite the refreshed state.
  revision++
  busy.value = false
  try {
    const settings = await updateSettings({ openai_oauth_default_timezone: defaultTimezone.value })
    if (disposed) return
    defaultTimezone.value = settings.openai_oauth_default_timezone || ''
    saved.value = true
    if (accountID.value) await selectAccount(false)
  } catch {
    if (!disposed) settingsError.value = t(`${key}.settingsSaveFailed`)
  } finally { if (!disposed) saving.value = false }
}

function onDetailsToggle(event: Event) {
  if ((event.target as HTMLDetailsElement).open && !listLoaded.value && !listLoading.value) void loadAccounts(1)
}

async function loadAccounts(nextPage = 1) {
  if (busy.value || saving.value) return
  const request = ++listRevision
  revision++
  accountID.value = 0
  state.value = null
  listLoading.value = true
  listError.value = false
  try {
    const result = await list(nextPage, 50, { platform: 'openai', type: 'oauth', search: search.value.trim(), lite: 'true' })
    if (request !== listRevision) return
    accounts.value = result.items
    total.value = result.total
    page.value = nextPage
    listLoaded.value = true
  } catch {
    if (request === listRevision) { listError.value = true; accounts.value = []; total.value = 0 }
  } finally { if (request === listRevision) listLoading.value = false }
}

function searchAccounts() { void loadAccounts(1) }

async function selectAccount(autoDetect: boolean | Event = true) {
  const request = ++revision
  state.value = null
  error.value = ''
  if (!accountID.value) return
  const id = accountID.value
  busy.value = true
  try {
    const value = await getAccountTimezone(id)
    if (request !== revision) return
    state.value = value
    if (autoDetect !== false && value.source !== 'proxy' && value.has_proxy) {
      const detected = await detectAccountTimezone(id)
      if (request === revision) state.value = detected
    }
  } catch {
    if (request === revision) {
      if (state.value) await refreshAfterDetectionFailure(id, request)
      if (request === revision) error.value = t(`${key}.${state.value ? 'detectFailed' : 'loadFailed'}`)
    }
  } finally { if (request === revision) busy.value = false }
}

async function detect() {
  if (busy.value || saving.value || !state.value?.has_proxy) return
  const request = ++revision
  const id = accountID.value
  busy.value = true
  error.value = ''
  try {
    const value = await detectAccountTimezone(id, true)
    if (request === revision) state.value = value
  } catch {
    if (request === revision) await refreshAfterDetectionFailure(id, request)
    if (request === revision) error.value = t(`${key}.detectFailed`)
  } finally { if (request === revision) busy.value = false }
}

async function refreshAfterDetectionFailure(id: number, request: number) {
  // A failed probe may mean the proxy changed; reload the effective fallback.
  try {
    const value = await getAccountTimezone(id)
    if (request === revision) state.value = value
  } catch {
    // Keep the last known state if the state endpoint is also unavailable.
  }
}

onMounted(loadDefault)
</script>
