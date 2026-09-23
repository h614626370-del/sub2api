<template>
  <section class="space-y-6" aria-labelledby="custom-features-title">
    <div>
      <h1 id="custom-features-title" class="text-xl font-semibold text-gray-900 dark:text-white">
        {{ t('admin.accounts.customFeatures.title') }}
      </h1>
      <p class="mt-1 max-w-3xl text-sm text-gray-500 dark:text-gray-400">
        {{ t('admin.accounts.customFeatures.description') }}
      </p>
    </div>

    <form v-if="!loading" class="space-y-6" @submit.prevent="save">
      <p v-if="loadFailed" role="alert" class="text-sm text-red-600">
        {{ t('admin.accounts.customFeatures.loadFailed') }}
        <button type="button" class="underline" @click="load">{{ t('common.refresh') }}</button>
      </p>
      <fieldset v-for="route in routes" :key="route.key" class="min-w-0 space-y-4 border-t border-gray-200 pt-5 dark:border-gray-700" :disabled="saving || loadFailed">
        <legend class="px-0 pt-5 text-base font-semibold text-gray-900 dark:text-white">
          {{ t(`admin.accounts.customFeatures.${route.name}Group`) }}
        </legend>
        <div>
          <div class="flex flex-wrap items-center justify-between gap-2">
            <label :for="`custom-${route.name}-sources`" class="text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t('admin.accounts.customFeatures.sourceGroups') }}
              <span class="ml-2 font-normal text-gray-500 dark:text-gray-400">{{ t('admin.accounts.customFeatures.selectedCount', { count: form[route.sources].length }) }}</span>
            </label>
            <div class="flex gap-3 text-sm">
              <button :id="`custom-${route.name}-select-all`" type="button" class="text-primary-600 hover:underline disabled:opacity-50 dark:text-primary-400" @click="selectAll(route.sources)">{{ t('admin.accounts.customFeatures.selectAllSources') }}</button>
              <button :id="`custom-${route.name}-clear`" type="button" class="text-gray-600 hover:underline disabled:opacity-50 dark:text-gray-300" @click="form[route.sources] = []">{{ t('admin.accounts.customFeatures.clearSources') }}</button>
            </div>
          </div>
          <input :id="`custom-${route.name}-sources`" v-model="search[route.name]" type="search" class="input mt-2 w-full" :placeholder="t('admin.accounts.customFeatures.searchSources')" />
          <div class="mt-2 max-h-56 overflow-y-auto rounded-md border border-gray-200 p-2 dark:border-gray-700">
            <div class="grid min-w-0 gap-1 sm:grid-cols-2">
              <label v-for="group in visibleSources(route.name, route.sources)" :key="group.id" class="flex min-w-0 cursor-pointer items-start gap-2 rounded p-2 text-sm hover:bg-gray-50 dark:hover:bg-dark-700" :class="group.unavailable ? 'text-red-600 dark:text-red-400' : 'text-gray-700 dark:text-gray-300'">
                <input :id="`custom-${route.name}-source-${group.id}`" v-model="form[route.sources]" type="checkbox" :value="group.id" class="mt-0.5 shrink-0" />
                <span class="min-w-0 break-words">
                  {{ group.name }} <span class="text-gray-500 dark:text-gray-400">#{{ group.id }}</span>
                  <span v-if="group.unavailable" class="block text-xs">{{ t('admin.accounts.customFeatures.sourceUnavailable') }}</span>
                </span>
              </label>
            </div>
            <p v-if="visibleSources(route.name, route.sources).length === 0" class="p-2 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.accounts.customFeatures.noSources') }}</p>
          </div>
        </div>
        <label :for="`custom-${route.name}-group`" class="block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.accounts.customFeatures.targetGroup') }}</label>
        <select :id="`custom-${route.name}-group`" v-model.number="form[route.key]" class="input w-full" :disabled="saving || loadFailed">
          <option :value="0">{{ t(`admin.accounts.customFeatures.${route.name}Disabled`) }}</option>
          <option v-if="form[route.key] && !specialGroups.some(group => group.id === form[route.key])" :value="form[route.key]" disabled>
            {{ t('admin.accounts.customFeatures.astraUnavailable') }} (#{{ form[route.key] }})
          </option>
          <option v-for="group in specialGroups" :key="group.id" :value="group.id">{{ group.name }} (#{{ group.id }})</option>
        </select>
      </fieldset>
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
    <AccountTimezonePanel />
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { AdminGroup } from '@/types'
import AccountTimezonePanel from './AccountTimezonePanel.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const appStore = useAppStore()
const loading = ref(true)
const saving = ref(false)
const loadFailed = ref(false)
const groups = ref<AdminGroup[]>([])
const specialGroups = computed(() => groups.value.filter(group => group.status === 'active' && group.subscription_type === 'special' && group.platform === 'openai'))
const sourceGroups = computed(() => groups.value.filter(group => group.status === 'active' && group.subscription_type !== 'special' && ['openai', 'composite'].includes(group.platform)))
const routes = [
  { name: 'astra', key: 'openai_astra_group_id', sources: 'openai_astra_source_group_ids' },
  { name: 'sol', key: 'openai_sol_group_id', sources: 'openai_sol_source_group_ids' },
] as const
type SourceKey = typeof routes[number]['sources']
const form = reactive({
  openai_astra_group_id: 0,
  openai_sol_group_id: 0,
  openai_astra_source_group_ids: [] as number[],
  openai_sol_source_group_ids: [] as number[],
})
const search = reactive({ astra: '', sol: '' })

function selectAll(key: SourceKey) {
  form[key] = [...new Set([...form[key], ...sourceGroups.value.map(group => group.id)])]
}

function visibleSources(name: typeof routes[number]['name'], key: SourceKey) {
  const availableIDs = new Set(sourceGroups.value.map(group => group.id))
  const invalid = form[key].filter(id => !availableIDs.has(id)).map(id => ({
    id, name: groups.value.find(group => group.id === id)?.name || t('admin.accounts.customFeatures.deletedSource'), unavailable: true,
  }))
  const query = search[name].trim().toLowerCase()
  return [
    ...invalid,
    ...sourceGroups.value.filter(group => `${group.name} #${group.id}`.toLowerCase().includes(query))
      .map(group => ({ id: group.id, name: group.name, unavailable: false })),
  ]
}

async function load() {
  loading.value = true
  loadFailed.value = false
  try {
    const [settings, allGroups] = await Promise.all([adminAPI.settings.getSettings(), adminAPI.groups.getAllIncludingInactive()])
    form.openai_astra_group_id = Number(settings.openai_astra_group_id || 0)
    form.openai_sol_group_id = Number(settings.openai_sol_group_id || 0)
    form.openai_astra_source_group_ids = [...(settings.openai_astra_source_group_ids || [])]
    form.openai_sol_source_group_ids = [...(settings.openai_sol_source_group_ids || [])]
    groups.value = allGroups
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
    const settings = await adminAPI.settings.updateSettings({ ...form })
    form.openai_astra_group_id = Number(settings.openai_astra_group_id || 0)
    form.openai_sol_group_id = Number(settings.openai_sol_group_id || 0)
    form.openai_astra_source_group_ids = [...(settings.openai_astra_source_group_ids || [])]
    form.openai_sol_source_group_ids = [...(settings.openai_sol_source_group_ids || [])]
    appStore.showSuccess(t('admin.accounts.customFeatures.saved'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.accounts.customFeatures.saveFailed')))
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>
