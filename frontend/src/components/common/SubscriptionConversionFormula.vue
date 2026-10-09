<template>
  <section class="rounded-xl border border-gray-200 bg-white p-5 dark:border-dark-700 dark:bg-dark-800 sm:p-6">
    <h2 class="text-base font-semibold text-gray-900 dark:text-gray-100">{{ t('subscriptionConversion.formulaTitle') }}</h2>
    <p class="my-4 rounded-lg bg-primary-50 p-4 text-sm font-medium leading-7 text-primary-900 dark:bg-primary-900/20 dark:text-primary-200">{{ t('subscriptionConversion.formula') }}</p>
    <ul class="space-y-2 text-sm leading-6 text-gray-700 dark:text-gray-300">
      <li>{{ t('subscriptionConversion.timeRule') }}</li><li>{{ t('subscriptionConversion.quotaRule') }}</li><li>{{ t('subscriptionConversion.priceRule') }}</li>
    </ul>
    <p class="mt-4 text-sm leading-6 text-gray-600 dark:text-gray-300">{{ t('subscriptionConversion.precision') }}</p>
    <details v-if="example" class="mt-5 border-t border-gray-200 pt-4 text-sm dark:border-dark-700">
      <summary class="cursor-pointer font-medium text-gray-800 dark:text-gray-200">{{ t('subscriptionConversion.exampleTitle') }}</summary>
      <p class="mt-3 leading-6 text-gray-700 dark:text-gray-300">{{ t('subscriptionConversion.example') }}</p>
    </details>
  </section>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
defineProps<{ example?: boolean }>()
const { t } = useI18n()
</script>
