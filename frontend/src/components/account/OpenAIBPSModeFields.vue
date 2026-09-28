<template>
  <div class="space-y-3" data-testid="bps-mode-fields">
    <label v-if="!hideToggle" class="flex items-center gap-2">
      <input v-model="enabled" type="checkbox" class="checkbox" data-testid="bps-mode-enabled" />
      <span class="text-sm font-medium">{{ t('admin.accounts.bps.modeEnabled') }}</span>
    </label>
    <div v-if="enabled || hideToggle">
      <label class="input-label" for="bps-mode-models">{{ t('admin.accounts.bps.modeModels') }}</label>
      <textarea id="bps-mode-models" v-model="models" class="input font-mono" rows="3" :required="enabled" data-testid="bps-mode-models" />
      <p v-if="hideToggle" class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.bps.listToggleHint') }}</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

defineProps<{ hideToggle?: boolean }>()
const enabled = defineModel<boolean>('enabled', { required: true })
const models = defineModel<string>('models', { required: true })
const { t } = useI18n()
</script>
