<template>
  <AppLayout>
    <div class="mx-auto max-w-5xl space-y-6">
      <header><p class="mb-2 text-sm text-gray-600 dark:text-gray-300">{{ t('imageMaster.operations') }}</p><h1 class="text-2xl font-semibold text-gray-900 dark:text-gray-100">{{ t('subscriptionConversion.title') }}</h1><p class="mt-2 text-sm text-gray-600 dark:text-gray-300">{{ t('subscriptionConversion.adminIntro') }}</p></header>
      <p v-if="error" role="alert" class="rounded-lg bg-red-50 p-4 text-sm text-red-800 dark:bg-red-900/20 dark:text-red-200">{{ error }}</p>
      <p v-if="notice" role="status" class="rounded-lg bg-green-50 p-4 text-sm text-green-800 dark:bg-green-900/20 dark:text-green-200">{{ notice }}</p>
      <div v-if="loading" role="status" class="h-32 animate-pulse rounded-xl bg-gray-100 p-6 dark:bg-dark-800">{{ t('subscriptionConversion.loading') }}</div>
      <button v-else-if="!loaded" class="btn btn-secondary" @click="load">{{ t('subscriptionConversion.retry') }}</button>
      <form v-else class="rounded-xl border border-gray-200 bg-white p-6 dark:border-dark-700 dark:bg-dark-800" @submit.prevent="save">
        <label class="flex cursor-pointer items-start gap-3"><input v-model="enabled" data-testid="conversion-enabled" type="checkbox" class="mt-1 h-4 w-4 rounded text-primary-600" :disabled="saving" /><span><span class="font-medium text-gray-900 dark:text-gray-100">{{ t('subscriptionConversion.enabled') }}</span><span class="mt-2 block max-w-2xl text-sm leading-6 text-gray-600 dark:text-gray-300">{{ t('subscriptionConversion.enabledHint') }}</span></span></label>
        <div class="mt-6 border-t border-gray-200 pt-4 dark:border-dark-700"><button class="btn btn-primary" type="submit" :disabled="saving">{{ t(saving ? 'subscriptionConversion.saving' : 'subscriptionConversion.save') }}</button></div>
      </form>
      <SubscriptionConversionFormula example />
      <section class="space-y-3 px-1"><h2 class="text-base font-semibold text-gray-900 dark:text-gray-100">{{ t('subscriptionConversion.eligibility') }}</h2><p class="max-w-3xl text-sm leading-6 text-gray-700 dark:text-gray-300">{{ t('subscriptionConversion.eligibleHint') }}</p><p class="max-w-3xl text-sm leading-6 text-gray-600 dark:text-gray-300">{{ t('subscriptionConversion.historyHint') }}</p></section>
    </div>
  </AppLayout>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import SubscriptionConversionFormula from '@/components/common/SubscriptionConversionFormula.vue'
import { subscriptionConversionAPI } from '@/api/subscriptionConversion'
import { useAppStore } from '@/stores/app'
const { t } = useI18n()
const app = useAppStore()
const enabled = ref(false), loading = ref(false), loaded = ref(false), saving = ref(false)
const error = ref(''), notice = ref('')
function message(e: unknown) { return (e as { message?: string })?.message || t('subscriptionConversion.failed') }
async function load() {
  loading.value = true; error.value = ''
  try { enabled.value = (await subscriptionConversionAPI.settings()).enabled; loaded.value = true }
  catch (e) { error.value = message(e) } finally { loading.value = false }
}
async function save() {
  if (saving.value) return
  saving.value = true; error.value = ''; notice.value = ''
  try {
    enabled.value = (await subscriptionConversionAPI.save({ enabled: enabled.value })).enabled
    notice.value = t('subscriptionConversion.saved')
    await app.fetchPublicSettings(true).catch(() => null)
  } catch (e) { error.value = message(e) } finally { saving.value = false }
}
onMounted(load)
</script>
