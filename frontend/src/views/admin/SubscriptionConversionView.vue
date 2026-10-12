<template>
  <AppLayout>
    <div class="mx-auto max-w-6xl space-y-6">
      <header><p class="mb-2 text-sm text-gray-600 dark:text-gray-300">{{ t('imageMaster.operations') }}</p><h1 class="text-2xl font-semibold text-gray-900 dark:text-gray-100">{{ t('subscriptionConversion.title') }}</h1></header>
      <p v-if="error" role="alert" class="rounded-lg bg-red-50 p-4 text-sm text-red-800 dark:bg-red-900/20 dark:text-red-200">{{ error }}</p>
      <p v-if="notice" role="status" class="rounded-lg bg-green-50 p-4 text-sm text-green-800 dark:bg-green-900/20 dark:text-green-200">{{ notice }}</p>
      <p v-if="settingsLoading" role="status">{{ t('subscriptionConversion.loading') }}</p>
      <button v-else-if="!settingsLoaded" class="btn btn-secondary" @click="loadSettings">{{ t('subscriptionConversion.retry') }}</button>
      <template v-else>
        <form data-testid="settings-form" class="flex flex-wrap items-center justify-between gap-4 border-b border-gray-200 pb-5 dark:border-dark-700" @submit.prevent="save">
          <label class="flex items-center gap-3 text-sm font-medium text-gray-900 dark:text-gray-100">
            <input v-model="enabled" data-testid="conversion-enabled" type="checkbox" class="h-4 w-4 rounded" :disabled="controlsLocked" />
            {{ t('subscriptionConversion.enabled') }}
          </label>
          <button class="btn btn-primary gap-2" type="submit" :disabled="controlsLocked || enabled === savedEnabled"><Icon name="check" size="sm" />{{ t(saving ? 'subscriptionConversion.saving' : 'subscriptionConversion.save') }}</button>
        </form>
        <section class="space-y-4">
          <form data-testid="search-form" class="flex flex-wrap items-end gap-3" @submit.prevent="searchUsers">
            <div class="min-w-0 flex-1">
              <label for="conversion-search" class="mb-2 block text-sm font-medium text-gray-900 dark:text-gray-100">{{ t('subscriptionConversion.targetUser') }}</label>
              <input id="conversion-search" v-model="keyword" data-testid="user-search" type="search" class="input w-full" :placeholder="t('subscriptionConversion.searchPlaceholder')" :disabled="controlsLocked" />
            </div>
            <button type="submit" class="btn btn-secondary gap-2" :disabled="controlsLocked || !keyword.trim()"><Icon name="search" size="sm" />{{ t('subscriptionConversion.search') }}</button>
          </form>
          <p v-if="searched && !users.length && !searching" class="text-sm text-gray-600 dark:text-gray-300">{{ t('subscriptionConversion.noUsers') }}</p>
          <template v-if="users.length">
            <label for="conversion-user" class="sr-only">{{ t('subscriptionConversion.targetUser') }}</label>
            <select id="conversion-user" v-model="userID" data-testid="user-select" class="input w-full" :disabled="controlsLocked" @change="changeUser">
              <option value="">{{ t('subscriptionConversion.selectUser') }}</option>
              <option v-for="user in users" :key="user.id" :value="String(user.id)">{{ userLabel(user) }}</option>
            </select>
            <p v-if="moreUsers" class="text-sm text-gray-600 dark:text-gray-300">{{ t('subscriptionConversion.moreUsers') }}</p>
          </template>
          <div v-if="targetUser" class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 pb-4 dark:border-dark-700">
            <div class="min-w-0"><p class="break-all font-medium text-gray-900 dark:text-gray-100">{{ userLabel(targetUser) }}</p><p class="mt-1 text-sm text-gray-600 dark:text-gray-300">{{ t('subscriptionConversion.balance') }}: ${{ money(String(targetUser.balance)) }}</p></div>
            <button class="btn btn-secondary h-10 w-10 shrink-0 p-2" :title="t('subscriptionConversion.refresh')" :aria-label="t('subscriptionConversion.refresh')" :disabled="controlsLocked" @click="load"><Icon name="refresh" size="sm" /></button>
          </div>
          <p v-if="!savedEnabled" class="text-sm text-amber-800 dark:text-amber-200">{{ t('subscriptionConversion.closed') }}</p>
        </section>
      </template>
      <p v-if="loading || searching" role="status">{{ t('subscriptionConversion.loading') }}</p>
      <template v-if="overview && targetUser">
        <section>
          <h2 class="mb-4 text-lg font-semibold text-gray-900 dark:text-gray-100">{{ t('subscriptionConversion.activeTitle') }}</h2>
          <p v-if="!overview.subscriptions.length" class="py-5 text-sm text-gray-600 dark:text-gray-300">{{ t('subscriptionConversion.empty') }}</p>
          <div v-else class="divide-y divide-gray-200 dark:divide-dark-700">
            <article v-for="row in overview.subscriptions" :key="row.subscription_id" class="p-5 sm:p-6" data-testid="subscription">
              <div class="flex flex-wrap items-start justify-between gap-4"><div class="min-w-0 break-words"><h3 class="font-semibold text-gray-900 dark:text-gray-100">{{ row.group_name }}</h3><p class="mt-1 text-sm text-gray-600 dark:text-gray-300">#{{ row.subscription_id }} · {{ t('subscriptionConversion.expiry') }} {{ date(row.expires_at) }}</p></div><button v-if="row.eligible" class="btn btn-primary gap-2" :disabled="!canConvert" data-testid="convert" @click="select(row)"><Icon name="arrowsUpDown" size="sm" />{{ t('subscriptionConversion.convert') }}</button><span v-else class="text-sm text-gray-600 dark:text-gray-300">{{ t('subscriptionConversion.unavailable') }}</span></div>
              <p v-if="!row.eligible" class="mt-4 text-sm text-amber-800 dark:text-amber-200">{{ t(`subscriptionConversion.reasons.${row.reason}`) }}</p>
              <template v-else>
                <dl class="mt-5 grid grid-cols-2 gap-x-6 gap-y-4 sm:grid-cols-4"><div><dt class="text-sm text-gray-600 dark:text-gray-300">{{ t('subscriptionConversion.price') }}</dt><dd class="mt-1 font-medium tabular-nums text-gray-900 dark:text-gray-100">${{ money(row.price_usd) }}</dd></div><div><dt class="text-sm text-gray-600 dark:text-gray-300">{{ t('subscriptionConversion.remainingTime') }}</dt><dd class="mt-1 font-medium tabular-nums text-gray-900 dark:text-gray-100">{{ days(row.remaining_seconds) }}</dd></div><div><dt class="text-sm text-gray-600 dark:text-gray-300">{{ t('subscriptionConversion.remainingQuota') }}</dt><dd class="mt-1 font-medium tabular-nums text-gray-900 dark:text-gray-100">${{ money(row.remaining_quota_usd) }}</dd></div><div><dt class="text-sm text-gray-600 dark:text-gray-300">{{ t('subscriptionConversion.estimate') }}</dt><dd class="mt-1 font-semibold tabular-nums text-primary-700 dark:text-primary-300">${{ money(row.amount_usd) }}</dd></div></dl>
                <details class="mt-5 border-t border-gray-100 pt-4 text-sm dark:border-dark-700"><summary class="cursor-pointer font-medium text-gray-700 dark:text-gray-200">{{ t('subscriptionConversion.details') }}</summary><dl class="mt-4 grid gap-3 text-gray-700 dark:text-gray-300 sm:grid-cols-2"><div>{{ t('subscriptionConversion.orders') }}: {{ row.order_ids.map(id => `#${id}`).join('、') }}</div><div>{{ t('subscriptionConversion.monthlyQuota') }}: ${{ money(row.monthly_quota_usd) }}</div><div>{{ t('subscriptionConversion.totalQuota') }}: ${{ money(row.total_quota_usd) }}</div><div>{{ t('subscriptionConversion.usedQuota') }}: ${{ money(row.used_quota_usd) }}</div></dl><p class="mt-4 break-words font-medium tabular-nums text-gray-900 dark:text-gray-100">${{ money(row.price_usd) }} × ({{ percent(row.time_ratio) }} × 50% + {{ percent(row.quota_ratio) }} × 50%) = ${{ money(row.amount_usd) }}</p><p class="mt-2 text-gray-600 dark:text-gray-300">{{ t('subscriptionConversion.stale') }}</p></details>
              </template>
            </article>
          </div>
        </section>
        <section><h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">{{ t('subscriptionConversion.history') }}</h2><p class="mb-4 mt-1 text-sm text-gray-600 dark:text-gray-300">{{ t('subscriptionConversion.historyLimit') }}</p><div class="overflow-x-auto"><table class="w-full text-left text-sm" :class="{ 'min-w-[540px]': overview.history.length > 0 }"><thead class="bg-gray-50 text-gray-600 dark:bg-dark-800 dark:text-gray-300"><tr><th class="px-5 py-3">{{ t('subscriptionConversion.group') }}</th><th class="px-5 py-3">{{ t('subscriptionConversion.convertedAt') }}</th><th class="px-5 py-3 text-right">{{ t('subscriptionConversion.credited') }}</th></tr></thead><tbody class="divide-y divide-gray-200 text-gray-800 dark:divide-dark-700 dark:text-gray-200"><tr v-for="receipt in overview.history" :key="receipt.subscription_id"><td class="px-5 py-4">{{ receipt.group_name }} <span class="text-gray-600 dark:text-gray-300">#{{ receipt.subscription_id }}</span></td><td class="px-5 py-4">{{ date(receipt.converted_at) }}</td><td class="px-5 py-4 text-right font-medium tabular-nums">${{ money(receipt.amount_usd) }}</td></tr><tr v-if="!overview.history.length"><td colspan="3" class="px-5 py-8 text-center text-gray-600 dark:text-gray-300">{{ t('subscriptionConversion.historyEmpty') }}</td></tr></tbody></table></div></section>
      </template>
      <details class="border-t border-gray-200 pt-5 dark:border-dark-700">
        <summary class="cursor-pointer text-sm font-medium text-gray-900 dark:text-gray-100">{{ t('subscriptionConversion.formulaTitle') }}</summary>
        <SubscriptionConversionFormula example />
        <p class="mt-4 text-sm leading-6 text-gray-600 dark:text-gray-300">{{ t('subscriptionConversion.eligibleHint') }}</p>
      </details>
      <BaseDialog :show="!!selected" :title="t('subscriptionConversion.confirmTitle')" width="narrow" @close="close">
        <div v-if="selected" class="space-y-4 text-sm"><p data-testid="confirmation-user" class="break-all font-semibold text-gray-900 dark:text-gray-100">{{ t('subscriptionConversion.creditAccount') }}: {{ selected.userLabel }}</p><p class="leading-6 text-gray-700 dark:text-gray-200">{{ t('subscriptionConversion.confirmHint', { name: selected.row.group_name }) }}</p><p class="rounded-lg bg-gray-50 p-4 text-gray-900 dark:bg-dark-700 dark:text-gray-100">{{ t('subscriptionConversion.estimate') }} <strong class="ml-2 tabular-nums">${{ money(selected.row.amount_usd) }}</strong></p><p class="text-gray-600 dark:text-gray-300">{{ t('subscriptionConversion.stale') }}</p><label class="flex items-start gap-3"><input v-model="acknowledged" type="checkbox" class="mt-1 h-4 w-4 rounded" :disabled="busy" data-testid="acknowledge" /><span class="leading-6 text-gray-800 dark:text-gray-200">{{ t('subscriptionConversion.acknowledge') }}</span></label><p v-if="confirmError" role="alert" class="text-red-700 dark:text-red-300">{{ confirmError }}</p></div>
        <template #footer><div class="flex justify-end gap-3"><button class="btn btn-secondary" :disabled="busy" @click="close">{{ t('subscriptionConversion.cancel') }}</button><button class="btn btn-primary gap-2" data-testid="confirm" :disabled="busy || !acknowledged || !savedEnabled" @click="convert"><Icon name="check" size="sm" />{{ t(busy ? 'subscriptionConversion.converting' : 'subscriptionConversion.confirm') }}</button></div></template>
      </BaseDialog>
    </div>
  </AppLayout>
</template>
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import SubscriptionConversionFormula from '@/components/common/SubscriptionConversionFormula.vue'
import { subscriptionConversionAPI, type ConversionOverview, type ConversionPreview } from '@/api/subscriptionConversion'
import * as usersAPI from '@/api/admin/users'
import type { AdminUser } from '@/types'
const { t, locale } = useI18n()
const overview = ref<ConversionOverview | null>(null)
const selected = ref<{ userID: number; userLabel: string; row: ConversionPreview } | null>(null)
const loading = ref(false), busy = ref(false), acknowledged = ref(false)
const settingsLoading = ref(false), settingsLoaded = ref(false), saving = ref(false)
const enabled = ref(false), savedEnabled = ref(false)
const keyword = ref(''), userID = ref(''), users = ref<AdminUser[]>([])
const searching = ref(false), searched = ref(false), moreUsers = ref(false)
const targetUser = computed(() => users.value.find(user => String(user.id) === userID.value))
const controlsLocked = computed(() => busy.value || loading.value || saving.value || searching.value || !!selected.value)
const canConvert = computed(() => !controlsLocked.value && savedEnabled.value && enabled.value === savedEnabled.value && !!overview.value?.enabled && !!targetUser.value)
const error = ref(''), confirmError = ref(''), notice = ref('')
const userLabel = (user: AdminUser) => `#${user.id} ${user.username || ''} ${user.email}`.trim()
const money = (value: string) => Number(value || 0).toLocaleString(locale.value, { minimumFractionDigits: 2, maximumFractionDigits: 6 })
const percent = (value: string) => `${(Number(value) * 100).toFixed(2)}%`
const days = (seconds: number) => t('subscriptionConversion.days', { days: (seconds / 86400).toFixed(2) })
const date = (value: string) => new Date(value).toLocaleString(locale.value)
const message = (e: unknown) => (e as { message?: string })?.message || t('subscriptionConversion.failed')
async function loadSettings() {
  settingsLoading.value = true; error.value = ''
  try {
    enabled.value = savedEnabled.value = (await subscriptionConversionAPI.settings()).enabled
    settingsLoaded.value = true
  } catch (e) { error.value = message(e) } finally { settingsLoading.value = false }
}
async function save() {
  if (controlsLocked.value) return
  saving.value = true; error.value = ''; notice.value = ''
  try {
    enabled.value = savedEnabled.value = (await subscriptionConversionAPI.save({ enabled: enabled.value })).enabled
    notice.value = t('subscriptionConversion.saved')
    if (targetUser.value) await load()
  } catch (e) { error.value = message(e) } finally { saving.value = false }
}
async function searchUsers() {
  if (controlsLocked.value || !keyword.value.trim()) return
  searching.value = true; searched.value = false; error.value = ''; notice.value = ''
  userID.value = ''; overview.value = null; users.value = []; moreUsers.value = false
  try {
    const result = await usersAPI.list(1, 30, { search: keyword.value.trim() })
    users.value = result.items; moreUsers.value = result.total > result.items.length; searched.value = true
  } catch (e) { error.value = message(e) } finally { searching.value = false }
}
async function changeUser() {
  selected.value = null; overview.value = null; notice.value = ''
  await load()
}
async function load() {
  const user = targetUser.value
  if (!user) return
  loading.value = true; error.value = ''
  overview.value = null
  try {
    const [result, updatedUser] = await Promise.all([subscriptionConversionAPI.overview(user.id), usersAPI.getById(user.id)])
    if (userID.value !== String(user.id)) return
    overview.value = result
    users.value = users.value.map(item => item.id === user.id ? updatedUser : item)
  } catch (e) { error.value = message(e) } finally { loading.value = false }
}
function select(row: ConversionPreview) {
  if (!canConvert.value || !targetUser.value || !row.eligible) return
  selected.value = { userID: targetUser.value.id, userLabel: userLabel(targetUser.value), row }
  acknowledged.value = false; confirmError.value = ''; notice.value = ''
}
function close() { if (!busy.value) selected.value = null }
async function convert() {
  if (!selected.value || busy.value || !acknowledged.value || !savedEnabled.value) return
  busy.value = true; confirmError.value = ''; notice.value = ''
  const confirmation = selected.value
  try {
    const receipt = await subscriptionConversionAPI.convert(confirmation.userID, confirmation.row.subscription_id, confirmation.row.quote)
    notice.value = t('subscriptionConversion.success', { amount: money(receipt.amount_usd) }); selected.value = null
    await load()
  } catch (e) { confirmError.value = message(e) } finally { busy.value = false }
}
onMounted(loadSettings)
</script>
