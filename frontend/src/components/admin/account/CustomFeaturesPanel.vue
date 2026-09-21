<template>
  <section class="space-y-6" aria-labelledby="custom-features-title">
    <div class="flex flex-col gap-1 sm:flex-row sm:items-end sm:justify-between">
      <div>
        <h1 id="custom-features-title" class="text-xl font-semibold text-gray-900 dark:text-white">
          {{ t('admin.accounts.customFeatures.title') }}
        </h1>
        <p class="mt-1 max-w-3xl text-sm text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.customFeatures.description') }}
        </p>
      </div>
      <span
        class="inline-flex w-fit items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-medium"
        :class="form.openai_codex_ticket_enabled
          ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
          : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'"
      >
        <span class="h-1.5 w-1.5 rounded-full" :class="form.openai_codex_ticket_enabled ? 'bg-emerald-500' : 'bg-gray-400'" />
        {{ form.openai_codex_ticket_enabled ? t('admin.accounts.customFeatures.enabled') : t('admin.accounts.customFeatures.disabled') }}
      </span>
    </div>

    <form v-if="!loading" class="space-y-6" @submit.prevent="save">
      <p v-if="loadFailed" role="alert" class="text-sm text-red-600">{{ t('admin.accounts.customFeatures.loadFailed') }} <button type="button" class="underline" @click="load">{{ t('common.refresh') }}</button></p>
      <div class="card p-6">
        <label for="custom-astra-group" class="block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.accounts.customFeatures.astraGroup') }}</label>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.accounts.customFeatures.astraDescription') }}</p>
        <select id="custom-astra-group" v-model.number="form.openai_astra_group_id" class="input mt-3 w-full">
          <option :value="0">{{ t('admin.accounts.customFeatures.astraDisabled') }}</option>
          <option v-if="form.openai_astra_group_id && !specialGroups.some(group => group.id === form.openai_astra_group_id)" :value="form.openai_astra_group_id" disabled>{{ t('admin.accounts.customFeatures.astraUnavailable') }} (#{{ form.openai_astra_group_id }})</option>
          <option v-for="group in specialGroups" :key="group.id" :value="group.id">{{ group.name }} (#{{ group.id }})</option>
        </select>
      </div>
      <div class="card overflow-hidden">
        <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
          <div class="flex items-start justify-between gap-4">
            <div>
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t('admin.accounts.customFeatures.codexTicketTitle') }}
              </h2>
              <p class="mt-1 max-w-3xl text-sm text-gray-500 dark:text-gray-400">
                {{ t('admin.accounts.customFeatures.codexTicketDescription') }}
              </p>
            </div>
            <Toggle id="custom-codex-ticket-enabled" v-model="form.openai_codex_ticket_enabled" />
          </div>
        </div>

        <div class="space-y-4 p-6">
          <div>
            <label for="custom-codex-ticket-proxy" class="block text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t('admin.accounts.customFeatures.codexTicketProxy') }}
            </label>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.customFeatures.codexTicketProxyDescription') }}
            </p>
            <input
              id="custom-codex-ticket-proxy"
              v-model="form.openai_codex_ticket_harvest_proxy_url"
              type="text"
              class="input mt-3 w-full font-mono text-sm"
              :placeholder="t('admin.accounts.customFeatures.codexTicketProxyPlaceholder')"
              autocomplete="off"
              @input="proxyDirty = true"
            />
            <p v-if="form.openai_codex_ticket_harvest_proxy_configured" class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.customFeatures.codexTicketProxyConfigured') }}
            </p>
          </div>

          <div class="rounded-lg border border-blue-200 bg-blue-50/70 p-4 text-sm text-blue-800 dark:border-blue-800/60 dark:bg-blue-900/20 dark:text-blue-200">
            {{ t('admin.accounts.customFeatures.codexTicketBehavior') }}
          </div>
        </div>
      </div>

      <div class="card overflow-hidden">
        <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
            {{ t('admin.accounts.customFeatures.futureTitle') }}
          </h2>
        </div>
        <div class="p-6">
          <div class="flex items-start gap-3 rounded-lg border border-dashed border-gray-300 p-4 dark:border-dark-600">
            <Icon name="clock" size="md" class="mt-0.5 shrink-0 text-gray-400" />
            <div>
              <p class="text-sm font-medium text-gray-700 dark:text-gray-200">
                {{ t('admin.accounts.customFeatures.futureTimezoneTitle') }}
              </p>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t('admin.accounts.customFeatures.futureTimezoneDescription') }}
              </p>
            </div>
          </div>
        </div>
      </div>

      <div class="flex justify-end">
        <button type="submit" class="btn btn-primary" :disabled="saving || loadFailed">
          <Icon v-if="saving" name="refresh" size="sm" class="mr-1.5 animate-spin" />
          {{ saving ? t('common.saving') : t('common.save') }}
        </button>
      </div>
    </form>

    <div v-else class="card flex items-center justify-center py-16 text-sm text-gray-500 dark:text-gray-400">
      <Icon name="refresh" size="sm" class="mr-2 animate-spin" />
      {{ t('common.loading') }}
    </div>
  </section>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { AdminGroup } from '@/types'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const appStore = useAppStore()
const loading = ref(true)
const saving = ref(false)
const proxyDirty = ref(false)
const specialGroups = ref<AdminGroup[]>([])
const loadFailed = ref(false)

const form = reactive({
  openai_astra_group_id: 0,
  openai_codex_ticket_enabled: false,
  openai_codex_ticket_harvest_proxy_url: '',
  openai_codex_ticket_harvest_proxy_configured: false,
})

async function load() {
  loading.value = true
  loadFailed.value = false
  try {
    const [settings, groups] = await Promise.all([adminAPI.settings.getSettings(), adminAPI.groups.getAll('openai')])
    form.openai_astra_group_id = Number(settings.openai_astra_group_id || 0)
    form.openai_codex_ticket_enabled = Boolean(settings.openai_codex_ticket_enabled)
    specialGroups.value = groups.filter(group => group.subscription_type === 'special' && group.platform === 'openai')
    form.openai_codex_ticket_harvest_proxy_url = settings.openai_codex_ticket_harvest_proxy_url || ''
    form.openai_codex_ticket_harvest_proxy_configured = Boolean(settings.openai_codex_ticket_harvest_proxy_configured)
    proxyDirty.value = false
  } catch (error) {
    loadFailed.value = true
    appStore.showError(extractApiErrorMessage(error, t('admin.accounts.customFeatures.loadFailed')))
  } finally {
    loading.value = false
  }
}

async function save() {
  if (loadFailed.value || saving.value) return
  saving.value = true
  try {
    const payload: Parameters<typeof adminAPI.settings.updateSettings>[0] = {
      openai_astra_group_id: form.openai_astra_group_id,
      openai_codex_ticket_enabled: form.openai_codex_ticket_enabled,
    }
    // The API masks stored proxy passwords as `***`. Do not submit that
    // placeholder back as a replacement when the field was untouched.
    if (proxyDirty.value) {
      payload.openai_codex_ticket_harvest_proxy_url = form.openai_codex_ticket_harvest_proxy_url.trim()
    }
    const settings = await adminAPI.settings.updateSettings(payload)
    form.openai_astra_group_id = Number(settings.openai_astra_group_id || 0)
    form.openai_codex_ticket_enabled = Boolean(settings.openai_codex_ticket_enabled)
    form.openai_codex_ticket_harvest_proxy_url = settings.openai_codex_ticket_harvest_proxy_url || form.openai_codex_ticket_harvest_proxy_url
    form.openai_codex_ticket_harvest_proxy_configured = Boolean(settings.openai_codex_ticket_harvest_proxy_configured)
    proxyDirty.value = false
    appStore.showSuccess(t('admin.accounts.customFeatures.saved'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.accounts.customFeatures.saveFailed')))
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>
