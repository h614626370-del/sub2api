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
      <div class="card p-6">
        <label for="custom-sol-group" class="block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.accounts.customFeatures.solGroup') }}</label>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.accounts.customFeatures.solDescription') }}</p>
        <select id="custom-sol-group" v-model.number="form.openai_sol_group_id" class="input mt-3 w-full">
          <option :value="0">{{ t('admin.accounts.customFeatures.solDisabled') }}</option>
          <option v-if="form.openai_sol_group_id && !specialGroups.some(group => group.id === form.openai_sol_group_id)" :value="form.openai_sol_group_id" disabled>{{ t('admin.accounts.customFeatures.astraUnavailable') }} (#{{ form.openai_sol_group_id }})</option>
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
            <p class="mt-2 text-xs text-gray-600 dark:text-gray-300">
              {{ t('admin.accounts.customFeatures.webshareStickyHint') }}
              <a href="https://help.webshare.io/en/articles/16310718-endpoint-generator-rotating-residential" target="_blank" rel="noopener noreferrer" class="text-primary-600 underline">{{ t('admin.accounts.customFeatures.webshareGuide') }}</a>
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

          <fieldset class="space-y-5 border-t border-gray-200 pt-5 dark:border-dark-600" @input="policyDirty = true" @change="policyDirty = true">
            <legend class="px-1 text-base font-semibold">{{ t('admin.accounts.customFeatures.policyTitle') }}</legend>
            <p class="text-sm text-gray-600 dark:text-gray-300">{{ t('admin.accounts.customFeatures.policyHelp') }}</p>
            <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
              <div v-for="field in numericFields" :key="field.key">
                <label :for="`ticket-${field.key}`" class="block text-sm font-medium">{{ t(`admin.accounts.customFeatures.${field.label}`) }}</label>
                <input :id="`ticket-${field.key}`" v-model.number="policy[field.key]" type="number" required step="1" :min="field.min" :max="field.key === 'refresh_before_seconds' ? policy.ttl_seconds - 1 : field.max" class="input mt-2 w-full" />
              </div>
            </div>
            <div>
              <label for="ticket-models" class="block text-sm font-medium">{{ t('admin.accounts.customFeatures.policyModels') }}</label>
              <textarea id="ticket-models" v-model="modelsText" rows="2" required class="input mt-2 w-full font-mono text-sm" />
            </div>
            <label class="flex items-start gap-3 text-sm">
              <input id="ticket-fail-closed" v-model="policy.fail_closed" type="checkbox" class="mt-1" />
              <span>{{ t('admin.accounts.customFeatures.policyFailClosed') }}</span>
            </label>
            <label class="flex items-start gap-3 text-sm">
              <input id="ticket-reuse-connection" v-model="policy.reuse_connection" type="checkbox" class="mt-1" :disabled="!policy.cookie_enabled" />
              <span>{{ t('admin.accounts.customFeatures.policyReuseConnection') }}</span>
            </label>
            <div v-if="policy.reuse_connection" class="max-w-sm">
              <label for="ticket-connection-max-age" class="block text-sm font-medium">{{ t('admin.accounts.customFeatures.policyConnectionMaxAge') }}</label>
              <input id="ticket-connection-max-age" v-model.number="policy.connection_max_age_seconds" type="number" required min="30" max="3600" step="1" class="input mt-2 w-full" />
            </div>
            <div class="space-y-4 border-t border-gray-200 pt-4 dark:border-dark-600">
              <label class="flex items-start gap-3 text-sm font-medium">
                <input id="ticket-cookie-enabled" v-model="policy.cookie_enabled" type="checkbox" class="mt-1" @change="onCookieToggle" />
                <span>{{ t('admin.accounts.customFeatures.policyCookieEnabled') }}</span>
              </label>
              <p class="text-sm text-gray-600 dark:text-gray-300">{{ t('admin.accounts.customFeatures.policyCookieHelp') }}</p>
              <div v-if="policy.cookie_enabled" class="space-y-4">
                <label class="flex items-start gap-3 text-sm">
                  <input id="ticket-cookie-required" v-model="policy.cookie_required" type="checkbox" class="mt-1" />
                  <span>{{ t('admin.accounts.customFeatures.policyCookieRequired') }}</span>
                </label>
                <div class="max-w-sm">
                  <label for="ticket-cookie-ttl" class="block text-sm font-medium">{{ t('admin.accounts.customFeatures.policyCookieTTL') }}</label>
                  <input id="ticket-cookie-ttl" v-model.number="policy.cookie_ttl_seconds" type="number" required min="10" max="86400" step="1" class="input mt-2 w-full" />
                </div>
              </div>
            </div>
          </fieldset>

          <div class="rounded-lg border border-blue-200 bg-blue-50/70 p-4 text-sm text-blue-800 dark:border-blue-800/60 dark:bg-blue-900/20 dark:text-blue-200">
            {{ t('admin.accounts.customFeatures.codexTicketBehavior') }}
          </div>
        </div>
      </div>

      <AccountTimezonePanel />

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
import type { CodexTicketPolicy } from '@/api/admin/settings'
import Toggle from '@/components/common/Toggle.vue'
import AccountTimezonePanel from './AccountTimezonePanel.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const appStore = useAppStore()
const loading = ref(true)
const saving = ref(false)
const proxyDirty = ref(false)
const policyDirty = ref(false)
const policy = reactive<CodexTicketPolicy>({
  ttl_seconds: 3600, refresh_before_seconds: 600, probe_interval_seconds: 6,
  attempt_timeout_seconds: 25, target_length: 292, models: ['gpt-6-astra', 'gpt-5.6-sol'],
  fail_closed: true, cookie_enabled: false, cookie_required: false, cookie_ttl_seconds: 240,
  reuse_connection: false,
  connection_max_age_seconds: 300,
})
const modelsText = ref(policy.models.join('\n'))
const numericFields = [
  { key: 'ttl_seconds', label: 'policyTTL', min: 10, max: 86400 },
  { key: 'refresh_before_seconds', label: 'policyRefresh', min: 0, max: 86399 },
  { key: 'probe_interval_seconds', label: 'policyInterval', min: 1, max: 3600 },
  { key: 'attempt_timeout_seconds', label: 'policyTimeout', min: 1, max: 120 },
  { key: 'target_length', label: 'policyLength', min: 1, max: 8192 },
] as const
function onCookieToggle() {
  if (!policy.cookie_enabled) {
    policy.cookie_required = false
    policy.reuse_connection = false
  }
}
function loadPolicy(value?: CodexTicketPolicy) {
  if (value) Object.assign(policy, value, { connection_max_age_seconds: value.connection_max_age_seconds || 300, reuse_connection: Boolean(value.reuse_connection), models: [...value.models] })
  modelsText.value = policy.models.join('\n')
  policyDirty.value = false
}
const specialGroups = ref<AdminGroup[]>([])
const loadFailed = ref(false)

const form = reactive({
  openai_astra_group_id: 0,
  openai_sol_group_id: 0,
  openai_codex_ticket_enabled: false,
  openai_codex_ticket_harvest_proxy_url: '',
  openai_codex_ticket_harvest_proxy_configured: false,
})

async function load() {
  loading.value = true
  loadFailed.value = false
  try {
    const [settings, groups] = await Promise.all([adminAPI.settings.getSettings(), adminAPI.groups.getAll('openai')])
    loadPolicy(settings.openai_codex_ticket_policy)
    form.openai_astra_group_id = Number(settings.openai_astra_group_id || 0)
    form.openai_sol_group_id = Number(settings.openai_sol_group_id || 0)
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
      openai_sol_group_id: form.openai_sol_group_id,
      openai_codex_ticket_enabled: form.openai_codex_ticket_enabled,
    }
    if (policyDirty.value) {
      const models = modelsText.value.split(/[\n,]+/).map(value => value.trim()).filter(Boolean)
      if (!Number.isInteger(policy.connection_max_age_seconds) || (policy.connection_max_age_seconds ?? 0) < 30 || (policy.connection_max_age_seconds ?? 0) > 3600 ||
          !models.length || models.length > 20 || new Set(models).size !== models.length || models.some(model => /\s/.test(model) || model.length > 128) ||
          numericFields.some(field => !Number.isInteger(policy[field.key]) || policy[field.key] < field.min || policy[field.key] > field.max) ||
          policy.refresh_before_seconds >= policy.ttl_seconds || !Number.isInteger(policy.cookie_ttl_seconds) || policy.cookie_ttl_seconds < 10 || policy.cookie_ttl_seconds > 86400) {
        appStore.showError(t('admin.accounts.customFeatures.policyInvalid'))
        return
      }
      payload.openai_codex_ticket_policy = { ...policy, models }
    }
    // The API masks stored proxy passwords as `***`. Do not submit that
    // placeholder back as a replacement when the field was untouched.
    if (proxyDirty.value) {
      payload.openai_codex_ticket_harvest_proxy_url = form.openai_codex_ticket_harvest_proxy_url.trim()
    }
    const settings = await adminAPI.settings.updateSettings(payload)
    loadPolicy(settings.openai_codex_ticket_policy)
    form.openai_astra_group_id = Number(settings.openai_astra_group_id || 0)
    form.openai_sol_group_id = Number(settings.openai_sol_group_id || 0)
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
