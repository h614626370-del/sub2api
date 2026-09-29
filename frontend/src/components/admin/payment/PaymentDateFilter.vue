<template>
  <section class="card p-4" :aria-label="t('payment.admin.dateFilter.title')">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div class="flex flex-wrap gap-1 rounded-lg bg-gray-100 p-1 dark:bg-dark-800" :aria-label="t('payment.admin.dateFilter.mode')">
        <button v-for="item in modes" :key="item" type="button" :aria-pressed="mode === item"
          class="rounded-md px-3 py-2 text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500"
          :class="mode === item ? 'bg-white text-primary-700 shadow-sm dark:bg-dark-600 dark:text-primary-300' : 'text-gray-600 hover:text-gray-900 dark:text-gray-300 dark:hover:text-white'"
          @click="mode = item">{{ t(`payment.admin.dateFilter.${item}`) }}</button>
      </div>
      <button type="button" class="btn btn-secondary" :disabled="loading" :aria-label="t('common.refresh')" @click="emit('refresh')">
        <Icon name="refresh" size="sm" :class="loading ? 'animate-spin motion-reduce:animate-none' : ''" />
        {{ t('common.refresh') }}
      </button>
    </div>

    <div v-if="mode === 'quick'" class="mt-4 flex flex-wrap gap-2">
      <button v-for="preset in PAYMENT_PRESETS" :key="preset" type="button" :aria-pressed="activePreset === preset"
        class="rounded-lg border px-3 py-2 text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500"
        :class="activePreset === preset ? 'border-primary-600 bg-primary-600 text-white' : 'border-gray-200 text-gray-700 hover:border-primary-400 hover:bg-primary-50 dark:border-dark-600 dark:text-gray-200 dark:hover:bg-dark-700'"
        @click="selectPreset(preset)">{{ t(`payment.admin.dateFilter.${preset}`) }}</button>
    </div>

    <form v-else class="mt-4" @submit.prevent="applyRange">
      <div class="flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-end">
        <div class="min-w-0 flex-1 sm:max-w-60">
          <label for="payment-range-start" class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-200">
            {{ t(`payment.admin.dateFilter.${mode === 'months' ? 'startMonth' : 'startDate'}`) }}
          </label>
          <input id="payment-range-start" v-model="draftStart" :type="mode === 'months' ? 'month' : 'date'"
            :max="mode === 'months' ? today.slice(0, 7) : today" class="input w-full dark:[color-scheme:dark]"
            aria-describedby="payment-range-help payment-range-error" :aria-invalid="!!errorKey" />
        </div>
        <div class="min-w-0 flex-1 sm:max-w-60">
          <label for="payment-range-end" class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-200">
            {{ t(`payment.admin.dateFilter.${mode === 'months' ? 'endMonth' : 'endDate'}`) }}
          </label>
          <input id="payment-range-end" v-model="draftEnd" :type="mode === 'months' ? 'month' : 'date'"
            :min="draftStart" :max="mode === 'months' ? today.slice(0, 7) : today" class="input w-full dark:[color-scheme:dark]"
            aria-describedby="payment-range-help payment-range-error" :aria-invalid="!!errorKey" />
        </div>
        <button type="submit" class="btn btn-primary" :disabled="!!errorKey || loading">{{ t('payment.admin.dateFilter.apply') }}</button>
      </div>
      <p id="payment-range-help" class="mt-3 text-xs text-gray-600 dark:text-gray-300">
        {{ t(`payment.admin.dateFilter.${mode === 'months' ? 'monthHint' : 'dateHint'}`) }}
      </p>
      <p v-if="errorKey" id="payment-range-error" role="alert" class="mt-2 text-sm text-red-600 dark:text-red-400">{{ t(`payment.admin.dateFilter.${errorKey}`) }}</p>
    </form>

    <div class="mt-4 flex flex-wrap items-center gap-x-4 gap-y-1 border-t border-gray-100 pt-3 text-xs text-gray-600 dark:border-dark-700 dark:text-gray-300" aria-live="polite">
      <span v-if="appliedStart && appliedEnd" class="inline-flex items-center gap-1.5">
        <Icon name="calendar" size="sm" />
        {{ t('payment.admin.dateFilter.applied') }}: <span class="font-medium tabular-nums">{{ appliedStart }} — {{ appliedEnd }}</span>
        <span>{{ t('payment.admin.dateFilter.dayCount', { count: appliedDays }) }}</span>
      </span>
      <span v-if="timezone">{{ t('payment.admin.dateFilter.timezone', { timezone }) }}</span>
      <span v-if="loading">{{ t('payment.admin.dateFilter.loading') }}</span>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { PaymentDashboardQuery } from '@/api/admin/payment'
import { PAYMENT_PRESETS, paymentMonthRange, paymentPresetRange, paymentRangeDays, paymentRangeError, type PaymentPreset } from './paymentDateRange'

const props = defineProps<{ today: string; timezone?: string; appliedStart?: string; appliedEnd?: string; loading: boolean }>()
const emit = defineEmits<{ change: [query: PaymentDashboardQuery]; refresh: [] }>()
const { t } = useI18n()
const modes = ['quick', 'months', 'custom'] as const
const mode = ref<typeof modes[number]>('quick')
const activePreset = ref<PaymentPreset | null>('last30')
const draftStart = ref('')
const draftEnd = ref('')
const draftRange = computed(() => mode.value === 'months'
  ? paymentMonthRange(draftStart.value, draftEnd.value, props.today)
  : { start_date: draftStart.value, end_date: draftEnd.value })
const errorKey = computed(() => {
  if (mode.value === 'months') {
    if (draftStart.value && draftEnd.value && draftStart.value > draftEnd.value) return 'reversed'
    if (draftEnd.value > props.today.slice(0, 7)) return 'future'
  }
  return paymentRangeError(draftRange.value, props.today)
})
const appliedDays = computed(() => paymentRangeDays({ start_date: props.appliedStart || '', end_date: props.appliedEnd || '' }))

watch(mode, value => {
  if (value === 'months') {
    draftStart.value = props.appliedStart?.slice(0, 7) || props.today.slice(0, 7)
    draftEnd.value = props.appliedEnd?.slice(0, 7) || props.today.slice(0, 7)
  } else if (value === 'custom') {
    draftStart.value = props.appliedStart || props.today
    draftEnd.value = props.appliedEnd || props.today
  }
})

function selectPreset(preset: PaymentPreset) {
  activePreset.value = preset
  if (preset === 'last7' || preset === 'last30' || preset === 'last90') {
    emit('change', { days: Number(preset.slice(4)) })
  } else {
    emit('change', paymentPresetRange(preset, props.today))
  }
}

function applyRange() {
  if (errorKey.value) return
  activePreset.value = null
  emit('change', draftRange.value)
}
</script>
