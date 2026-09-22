<template>
  <section class="card p-6" aria-labelledby="account-timezone-title">
    <h2 id="account-timezone-title" class="text-lg font-semibold text-gray-900 dark:text-white">{{ t(`${key}.title`) }}</h2>
    <p class="mt-1 max-w-3xl text-sm text-gray-600 dark:text-gray-300">{{ t(`${key}.description`) }}</p>
    <div class="mt-5 flex flex-wrap items-end gap-3">
      <div class="min-w-0 flex-1 sm:max-w-sm">
        <label for="timezone-search" class="block text-sm font-medium">{{ t(`${key}.search`) }}</label>
        <input id="timezone-search" v-model="search" type="search" class="input mt-2 w-full" @keydown.enter.prevent="searchAccounts" />
      </div>
      <button type="button" class="btn btn-secondary" :disabled="listLoading || busy" @click="searchAccounts">{{ t(`${key}.searchAction`) }}</button>
    </div>
    <p v-if="listError" role="alert" class="mt-3 text-sm text-red-700 dark:text-red-300">{{ t(`${key}.listFailed`) }}</p>
    <div class="mt-4 max-w-xl">
      <label for="timezone-account" class="block text-sm font-medium">{{ t(`${key}.account`) }}</label>
      <select id="timezone-account" v-model.number="accountID" class="input mt-2 w-full" :disabled="busy || listLoading" @change="selectAccount">
        <option :value="0">{{ listLoading ? t('common.loading') : t(`${key}.choose`) }}</option>
        <option v-for="account in accounts" :key="account.id" :value="account.id">{{ account.name }} (#{{ account.id }})</option>
      </select>
      <div class="mt-2 flex items-center gap-3 text-sm">
        <button type="button" class="btn btn-secondary" :disabled="page <= 1 || listLoading || busy" @click="loadAccounts(page - 1)">{{ t(`${key}.previous`) }}</button>
        <span>{{ t(`${key}.page`, { page, total }) }}</span>
        <button type="button" class="btn btn-secondary" :disabled="page * 50 >= total || listLoading || busy" @click="loadAccounts(page + 1)">{{ t(`${key}.next`) }}</button>
      </div>
      <p v-if="!listLoading && !listError && !accounts.length" class="mt-2 text-sm text-gray-600 dark:text-gray-300">{{ t(`${key}.empty`) }}</p>
    </div>
    <div v-if="accountID" class="mt-5 space-y-4 border-t border-gray-200 pt-5 dark:border-dark-600" :aria-busy="busy">
      <p v-if="busy" role="status" class="text-sm text-gray-600 dark:text-gray-300">{{ t(`${key}.working`) }}</p>
      <p v-if="error" role="alert" class="text-sm text-red-700 dark:text-red-300">{{ error }}</p>
      <button v-if="!state && !busy" type="button" class="btn btn-secondary" @click="selectAccount">{{ t('common.refresh') }}</button>
      <template v-if="state">
        <dl class="grid gap-x-6 gap-y-3 text-sm sm:grid-cols-2">
          <div><dt class="text-gray-600 dark:text-gray-300">{{ t(`${key}.effective`) }}</dt><dd class="mt-1 font-medium">{{ state.timezone || t(`${key}.unchanged`) }} <span class="font-normal text-gray-600 dark:text-gray-300">· {{ t(`${key}.source${state.source}`) }}</span></dd></div>
          <div><dt class="text-gray-600 dark:text-gray-300">{{ t(`${key}.detected`) }}</dt><dd class="mt-1 font-mono">{{ state.detected_timezone || '—' }}<span v-if="state.ip"> · {{ state.ip }}</span></dd></div>
        </dl>
        <p v-if="state.stale" class="text-sm text-amber-800 dark:text-amber-200">{{ t(`${key}.stale`) }}</p>
        <p v-if="!state.has_proxy" class="text-sm text-gray-600 dark:text-gray-300">{{ t(`${key}.noProxy`) }}</p>
        <div class="flex flex-wrap items-end gap-3">
          <div class="min-w-0 flex-1 sm:max-w-sm">
            <label for="timezone-mode" class="block text-sm font-medium">{{ t(`${key}.mode`) }}</label>
            <select id="timezone-mode" v-model="mode" class="input mt-2 w-full" :disabled="busy">
              <option value="auto">{{ t(`${key}.auto`) }}</option>
              <option value="manual">{{ t(`${key}.manual`) }}</option>
            </select>
          </div>
          <button type="button" class="btn btn-secondary" :disabled="busy || !state.has_proxy" @click="detect">{{ t(`${key}.detectAction`) }}</button>
        </div>
        <div v-if="mode === 'manual'" class="max-w-sm">
          <label for="timezone-override" class="block text-sm font-medium">{{ t(`${key}.override`) }}</label>
          <input id="timezone-override" v-model="override" type="text" list="account-timezone-options" class="input mt-2 w-full" placeholder="America/Los_Angeles" :disabled="busy" :aria-describedby="'timezone-override-help'" @keydown.enter.prevent="save" />
          <datalist id="account-timezone-options"><option v-for="zone in zones" :key="zone" :value="zone" /></datalist>
          <p id="timezone-override-help" class="mt-2 text-sm text-gray-600 dark:text-gray-300">{{ t(`${key}.overrideHelp`) }}</p>
        </div>
        <div class="flex flex-wrap items-center gap-3">
          <button type="button" class="btn btn-primary" :disabled="busy || (mode === 'manual' && !override.trim())" @click="save">{{ t(`${key}.save`) }}</button>
          <p v-if="saved" role="status" class="text-sm text-emerald-700 dark:text-emerald-300">{{ t(`${key}.saved`) }}</p>
        </div>
      </template>
    </div>
  </section>
</template>

<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { list } from '@/api/admin/accounts'
import { getAccountTimezone, detectAccountTimezone, setAccountTimezone, type AccountTimezoneState } from '@/api/admin/accountTimezone'
import type { AccountListItem } from '@/types'

const { t } = useI18n()
const key = 'admin.accounts.accountTimezone'
const accounts = ref<AccountListItem[]>([])
const search = ref('')
const page = ref(1)
const total = ref(0)
const accountID = ref(0)
const listLoading = ref(false)
const listError = ref(false)
const busy = ref(false)
const saved = ref(false)
const error = ref('')
const state = ref<AccountTimezoneState | null>(null)
const mode = ref('auto')
const override = ref('')
const zones = ['America/Los_Angeles', 'America/New_York', 'America/Chicago', 'Europe/London', 'Europe/Berlin', 'Asia/Tokyo', 'Asia/Singapore', 'Asia/Shanghai', 'Australia/Sydney', 'UTC']
let revision = 0
let listRevision = 0
onBeforeUnmount(() => { revision++; listRevision++ })

async function loadAccounts(nextPage = 1) {
  if (busy.value) return
  const request = ++listRevision
  revision++
  accountID.value = 0
  state.value = null
  listLoading.value = true
  listError.value = false
  try {
    const result = await list(nextPage, 50, { platform: 'openai', search: search.value.trim(), lite: 'true' })
    if (request !== listRevision) return
    accounts.value = result.items
    total.value = result.total
    page.value = nextPage
  } catch {
    if (request === listRevision) { listError.value = true; accounts.value = []; total.value = 0 }
  } finally { if (request === listRevision) listLoading.value = false }
}

function searchAccounts() { void loadAccounts(1) }

function accept(value: AccountTimezoneState) {
  state.value = value
  mode.value = value.override ? 'manual' : 'auto'
  override.value = value.override
}

async function selectAccount() {
  const request = ++revision
  state.value = null
  error.value = ''
  saved.value = false
  if (!accountID.value) return
  const id = accountID.value
  busy.value = true
  try {
    const value = await getAccountTimezone(id)
    if (request !== revision) return
    accept(value)
    if (!value.timezone && value.has_proxy) {
      const detected = await detectAccountTimezone(id)
      if (request === revision) accept(detected)
    }
  } catch { if (request === revision) error.value = t(`${key}.${state.value ? 'detectFailed' : 'loadFailed'}`) }
  finally { if (request === revision) busy.value = false }
}

async function detect() {
  if (busy.value) return
  const request = revision
  busy.value = true
  error.value = ''
  saved.value = false
  try {
    const value = await detectAccountTimezone(accountID.value, true)
    // Refreshing detection must not erase an unsaved manual draft.
    if (request === revision) state.value = value
  } catch { if (request === revision) error.value = t(`${key}.detectFailed`) }
  finally { if (request === revision) busy.value = false }
}

async function save() {
  if (busy.value || !state.value) return
  const value = mode.value === 'manual' ? override.value.trim() : ''
  if (mode.value === 'manual') {
    try {
      if (!value || value === 'Local') throw new Error('invalid')
      new Intl.DateTimeFormat('en', { timeZone: value }).format()
    } catch { error.value = t(`${key}.invalid`); return }
  }
  const request = revision
  busy.value = true
  error.value = ''
  saved.value = false
  try {
    const result = await setAccountTimezone(accountID.value, value)
    if (request !== revision) return
    accept(result)
    saved.value = true
    if (!result.timezone && result.has_proxy) {
      try {
        const detected = await detectAccountTimezone(accountID.value)
        if (request === revision) accept(detected)
      } catch { if (request === revision) error.value = t(`${key}.detectFailed`) }
    }
  } catch { if (request === revision) error.value = t(`${key}.saveFailed`) }
  finally { if (request === revision) busy.value = false }
}

onMounted(() => { void loadAccounts() })
</script>
