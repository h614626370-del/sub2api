<template>
  <div class="space-y-3" data-testid="bps-mode-fields">
    <label class="flex items-center gap-2">
      <input v-model="enabled" type="checkbox" class="checkbox" data-testid="bps-mode-enabled" />
      <span class="text-sm font-medium">{{ t('admin.accounts.bps.modeEnabled') }}</span>
    </label>
    <div v-if="enabled">
      <label class="input-label" for="bps-mode-models">{{ t('admin.accounts.bps.modeModels') }}</label>
      <textarea id="bps-mode-models" v-model="models" class="input font-mono" rows="3" required data-testid="bps-mode-models" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

const enabled = defineModel<boolean>('enabled', { required: true })
const models = defineModel<string>('models', { required: true })
const { t } = useI18n()
</script>
