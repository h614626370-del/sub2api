<template>
  <AppLayout>
    <div class="mx-auto max-w-6xl space-y-6">
      <header class="flex flex-wrap items-start justify-between gap-4"><div><h1 class="text-2xl font-semibold text-gray-900 dark:text-gray-100">{{ t('subscriptionConversion.title') }}</h1><p class="mt-2 text-sm text-gray-600 dark:text-gray-300">{{ t('subscriptionConversion.userIntro') }}</p></div><button class="btn btn-secondary" :disabled="loading || busy" @click="load">{{ t('subscriptionConversion.refresh') }}</button></header>
      <p v-if="error" role="alert" class="rounded-lg bg-red-50 p-4 text-sm text-red-800 dark:bg-red-900/20 dark:text-red-200">{{ error }}</p>
      <p v-if="notice" role="status" class="rounded-lg bg-green-50 p-4 text-sm text-green-800 dark:bg-green-900/20 dark:text-green-200">{{ notice }}</p>
      <div v-if="loading && !overview" role="status" class="h-48 animate-pulse rounded-xl bg-gray-100 p-6 dark:bg-dark-800">{{ t('subscriptionConversion.loading') }}</div>
      <section v-else-if="overview && !overview.enabled" class="rounded-xl border border-gray-200 p-8 dark:border-dark-700"><h2 class="font-semibold text-gray-900 dark:text-gray-100">{{ t('subscriptionConversion.closed') }}</h2><p class="mt-3 text-sm text-gray-600 dark:text-gray-300">{{ t('subscriptionConversion.closedHint') }}</p></section>
      <template v-else-if="overview">
        <SubscriptionConversionFormula />
        <section>
          <h2 class="mb-4 text-lg font-semibold text-gray-900 dark:text-gray-100">{{ t('subscriptionConversion.activeTitle') }}</h2>
          <div v-if="!overview.subscriptions.length" class="rounded-xl border border-dashed border-gray-300 p-8 dark:border-dark-600"><h3 class="font-medium text-gray-900 dark:text-gray-100">{{ t('subscriptionConversion.empty') }}</h3><p class="mt-2 text-sm text-gray-600 dark:text-gray-300">{{ t('subscriptionConversion.emptyHint') }}</p></div>
          <div v-else class="divide-y divide-gray-200 rounded-xl border border-gray-200 bg-white dark:divide-dark-700 dark:border-dark-700 dark:bg-dark-800">
            <article v-for="row in overview.subscriptions" :key="row.subscription_id" class="p-5 sm:p-6" data-testid="subscription">
              <div class="flex flex-wrap items-start justify-between gap-4"><div><h3 class="font-semibold text-gray-900 dark:text-gray-100">{{ row.group_name }}</h3><p class="mt-1 text-sm text-gray-600 dark:text-gray-300">#{{ row.subscription_id }} · {{ t('subscriptionConversion.expiry') }} {{ date(row.expires_at) }}</p></div><button v-if="row.eligible" class="btn btn-primary" :disabled="busy || loading" data-testid="convert" @click="select(row)">{{ t('subscriptionConversion.convert') }}</button><span v-else class="rounded-md bg-gray-100 px-3 py-1 text-sm text-gray-700 dark:bg-dark-700 dark:text-gray-200">{{ t('subscriptionConversion.unavailable') }}</span></div>
              <p v-if="!row.eligible" class="mt-4 text-sm text-amber-800 dark:text-amber-200">{{ t(`subscriptionConversion.reasons.${row.reason}`) }}</p>
              <template v-else>
                <dl class="mt-5 grid grid-cols-2 gap-x-6 gap-y-4 sm:grid-cols-4"><div><dt class="text-sm text-gray-600 dark:text-gray-300">{{ t('subscriptionConversion.price') }}</dt><dd class="mt-1 font-medium tabular-nums text-gray-900 dark:text-gray-100">${{ money(row.price_usd) }}</dd></div><div><dt class="text-sm text-gray-600 dark:text-gray-300">{{ t('subscriptionConversion.remainingTime') }}</dt><dd class="mt-1 font-medium tabular-nums text-gray-900 dark:text-gray-100">{{ days(row.remaining_seconds) }}</dd></div><div><dt class="text-sm text-gray-600 dark:text-gray-300">{{ t('subscriptionConversion.remainingQuota') }}</dt><dd class="mt-1 font-medium tabular-nums text-gray-900 dark:text-gray-100">${{ money(row.remaining_quota_usd) }}</dd></div><div><dt class="text-sm text-gray-600 dark:text-gray-300">{{ t('subscriptionConversion.estimate') }}</dt><dd class="mt-1 font-semibold tabular-nums text-primary-700 dark:text-primary-300">${{ money(row.amount_usd) }}</dd></div></dl>
                <details class="mt-5 border-t border-gray-100 pt-4 text-sm dark:border-dark-700"><summary class="cursor-pointer font-medium text-gray-700 dark:text-gray-200">{{ t('subscriptionConversion.details') }}</summary><dl class="mt-4 grid gap-3 text-gray-700 dark:text-gray-300 sm:grid-cols-2"><div>{{ t('subscriptionConversion.orders') }}: {{ row.order_ids.map(id => `#${id}`).join('、') }}</div><div>{{ t('subscriptionConversion.monthlyQuota') }}: ${{ money(row.monthly_quota_usd) }}</div><div>{{ t('subscriptionConversion.totalQuota') }}: ${{ money(row.total_quota_usd) }}</div><div>{{ t('subscriptionConversion.usedQuota') }}: ${{ money(row.used_quota_usd) }}</div></dl><p class="mt-4 break-words font-medium tabular-nums text-gray-900 dark:text-gray-100">${{ money(row.price_usd) }} × ({{ percent(row.time_ratio) }} × 50% + {{ percent(row.quota_ratio) }} × 50%) = ${{ money(row.amount_usd) }}</p><p class="mt-2 text-gray-600 dark:text-gray-300">{{ t('subscriptionConversion.stale') }}</p></details>
              </template>
            </article>
          </div>
        </section>
        <section><h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">{{ t('subscriptionConversion.history') }}</h2><p class="mb-4 mt-1 text-sm text-gray-600 dark:text-gray-300">{{ t('subscriptionConversion.historyLimit') }}</p><div class="overflow-x-auto rounded-xl border border-gray-200 dark:border-dark-700"><table class="w-full text-left text-sm" :class="{ 'min-w-[540px]': overview.history.length > 0 }"><thead class="bg-gray-50 text-gray-600 dark:bg-dark-800 dark:text-gray-300"><tr><th class="px-5 py-3">{{ t('subscriptionConversion.group') }}</th><th class="px-5 py-3">{{ t('subscriptionConversion.convertedAt') }}</th><th class="px-5 py-3 text-right">{{ t('subscriptionConversion.credited') }}</th></tr></thead><tbody class="divide-y divide-gray-200 text-gray-800 dark:divide-dark-700 dark:text-gray-200"><tr v-for="receipt in overview.history" :key="receipt.subscription_id"><td class="px-5 py-4">{{ receipt.group_name }} <span class="text-gray-600 dark:text-gray-300">#{{ receipt.subscription_id }}</span></td><td class="px-5 py-4">{{ date(receipt.converted_at) }}</td><td class="px-5 py-4 text-right font-medium tabular-nums">${{ money(receipt.amount_usd) }}</td></tr><tr v-if="!overview.history.length"><td colspan="3" class="px-5 py-8 text-center text-gray-600 dark:text-gray-300">{{ t('subscriptionConversion.historyEmpty') }}</td></tr></tbody></table></div></section>
      </template>
      <BaseDialog :show="!!selected" :title="t('subscriptionConversion.confirmTitle')" width="narrow" @close="close">
        <div v-if="selected" class="space-y-4 text-sm"><p class="leading-6 text-gray-700 dark:text-gray-200">{{ t('subscriptionConversion.confirmHint', { name: selected.group_name }) }}</p><p class="rounded-lg bg-gray-50 p-4 text-gray-900 dark:bg-dark-700 dark:text-gray-100">{{ t('subscriptionConversion.estimate') }} <strong class="ml-2 tabular-nums">${{ money(selected.amount_usd) }}</strong></p><p class="text-gray-600 dark:text-gray-300">{{ t('subscriptionConversion.stale') }}</p><label class="flex items-start gap-3"><input v-model="acknowledged" type="checkbox" class="mt-1 h-4 w-4 rounded" :disabled="busy" data-testid="acknowledge" /><span class="leading-6 text-gray-800 dark:text-gray-200">{{ t('subscriptionConversion.acknowledge') }}</span></label><p v-if="confirmError" role="alert" class="text-red-700 dark:text-red-300">{{ confirmError }}</p></div>
        <template #footer><div class="flex justify-end gap-3"><button class="btn btn-secondary" :disabled="busy" @click="close">{{ t('subscriptionConversion.cancel') }}</button><button class="btn btn-primary" data-testid="confirm" :disabled="busy || !acknowledged" @click="convert">{{ t(busy ? 'subscriptionConversion.converting' : 'subscriptionConversion.confirm') }}</button></div></template>
      </BaseDialog>
    </div>
  </AppLayout>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import SubscriptionConversionFormula from '@/components/common/SubscriptionConversionFormula.vue'
import { subscriptionConversionAPI, type ConversionOverview, type ConversionPreview } from '@/api/subscriptionConversion'
import { useAuthStore } from '@/stores/auth'
import { useSubscriptionStore } from '@/stores/subscriptions'
const { t, locale } = useI18n()
const auth = useAuthStore(), subscriptions = useSubscriptionStore()
const overview = ref<ConversionOverview | null>(null), selected = ref<ConversionPreview | null>(null)
const loading = ref(false), busy = ref(false), acknowledged = ref(false)
const error = ref(''), confirmError = ref(''), notice = ref('')
const money = (value: string) => Number(value || 0).toLocaleString(locale.value, { minimumFractionDigits: 2, maximumFractionDigits: 6 })
const percent = (value: string) => `${(Number(value) * 100).toFixed(2)}%`
const days = (seconds: number) => t('subscriptionConversion.days', { days: (seconds / 86400).toFixed(2) })
const date = (value: string) => new Date(value).toLocaleString(locale.value)
const message = (e: unknown) => (e as { message?: string })?.message || t('subscriptionConversion.failed')
async function load() {
  loading.value = true; error.value = ''
  try { overview.value = await subscriptionConversionAPI.overview() } catch (e) { error.value = message(e) } finally { loading.value = false }
}
function select(row: ConversionPreview) { selected.value = row; acknowledged.value = false; confirmError.value = '' }
function close() { if (!busy.value) selected.value = null }
async function convert() {
  if (!selected.value || busy.value || !acknowledged.value) return
  busy.value = true; confirmError.value = ''; notice.value = ''
  try {
    const receipt = await subscriptionConversionAPI.convert(selected.value.subscription_id, selected.value.quote)
    notice.value = t('subscriptionConversion.success', { amount: money(receipt.amount_usd) }); selected.value = null
    await Promise.allSettled([load(), auth.refreshUser(), subscriptions.fetchActiveSubscriptions(true)])
  } catch (e) { confirmError.value = message(e) } finally { busy.value = false }
}
onMounted(load)
</script>
