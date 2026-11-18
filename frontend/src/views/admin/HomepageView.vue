<template>
  <AppLayout>
    <main class="mx-auto max-w-5xl space-y-6">
      <header class="flex flex-wrap items-center justify-between gap-4 border-b border-gray-200 pb-5 dark:border-dark-700">
        <h1 class="text-2xl font-semibold text-gray-900 dark:text-gray-100">{{ t('homepage.title') }}</h1>
        <a v-if="saved && !saved.backend_only" href="/" target="_blank" rel="noopener noreferrer" class="btn btn-secondary gap-2">
          <Icon name="externalLink" size="sm" />{{ t('homepage.open') }}
        </a>
      </header>

      <p v-if="error" role="alert" class="rounded-lg bg-red-50 p-4 text-sm text-red-800 dark:bg-red-900/30 dark:text-red-200">{{ error }}</p>
      <p v-if="notice" role="status" class="rounded-lg bg-green-50 p-4 text-sm text-green-800 dark:bg-green-900/30 dark:text-green-200">{{ notice }}</p>
      <p v-if="loading" role="status" class="text-sm text-gray-600 dark:text-gray-300">{{ t('common.loading') }}</p>
      <button v-else-if="!saved" type="button" class="btn btn-secondary gap-2" @click="load()">
        <Icon name="refresh" size="sm" />{{ t('homepage.reload') }}
      </button>
      <form v-else class="space-y-5" @submit.prevent="save">
        <div class="flex flex-wrap items-end justify-between gap-4">
          <label class="flex min-h-10 items-center gap-3 text-sm font-medium text-gray-900 dark:text-gray-100">
            <input v-model="form.enabled" type="checkbox" :disabled="busy" class="h-4 w-4 accent-primary-600" />
            {{ t('homepage.enabled') }}
          </label>
          <div class="flex flex-wrap items-center gap-2">
            <button type="button" class="btn btn-secondary gap-2" :disabled="busy || !saved.has_previous || conflict" @click="confirmRestore = true">
              <Icon name="refresh" size="sm" />{{ t('homepage.restore') }}
            </button>
            <button type="submit" class="btn btn-primary gap-2" :disabled="busy || !dirty || conflict">
              <Icon name="check" size="sm" />{{ t(action === 'save' ? 'homepage.saving' : 'homepage.save') }}
            </button>
          </div>
        </div>
        <p aria-live="polite" class="text-sm text-gray-600 dark:text-gray-300">{{ t(`homepage.${saved.backend_only ? 'backendOnly' : live ? 'active' : 'inactive'}`) }}</p>
        <button v-if="conflict" type="button" class="btn btn-secondary" :disabled="busy" @click="load(true)">{{ t('homepage.reloadRevision') }}</button>
        <div>
          <label for="homepage-origin" class="mb-2 block text-sm font-medium text-gray-900 dark:text-gray-100">{{ t('homepage.origin') }}</label>
          <input id="homepage-origin" v-model.trim="form.public_origin" type="url" placeholder="https://example.com" maxlength="320" :required="form.enabled" :disabled="busy" class="input w-full max-w-xl" />
        </div>
        <div>
          <label for="homepage-html" class="mb-2 block text-sm font-medium text-gray-900 dark:text-gray-100">{{ t('homepage.html') }}</label>
          <textarea id="homepage-html" v-model="html" rows="18" spellcheck="false" autocapitalize="off" autocomplete="off" :required="form.enabled" :disabled="busy" class="input block min-h-64 w-full resize-y font-mono text-sm leading-6" />
        </div>
        <ul v-if="!dirty && saved.warnings.length" aria-live="polite" class="list-disc space-y-1 pl-5 text-sm text-amber-800 dark:text-amber-200">
          <li v-for="warning in saved.warnings" :key="warning">{{ t(`homepage.warnings.${warning}`) }}</li>
        </ul>
        <div v-if="live" class="flex flex-wrap gap-x-4 gap-y-2 text-sm">
          <a v-for="documentPath in documentPaths" :key="documentPath" :href="documentPath" target="_blank" rel="noopener noreferrer" class="inline-flex items-center gap-1 font-mono text-primary-700 underline dark:text-primary-300">
            {{ documentPath }}<Icon name="externalLink" size="sm" />
          </a>
        </div>
      </form>
      <BaseDialog :show="confirmRestore" :title="t('homepage.restore')" width="narrow" @close="confirmRestore = false">
        <p class="text-sm text-gray-700 dark:text-gray-200">{{ t('homepage.restoreConfirm') }}</p>
        <template #footer>
          <div class="flex flex-wrap justify-end gap-3">
            <button type="button" class="btn btn-secondary" @click="confirmRestore = false">{{ t('common.cancel') }}</button>
            <button type="button" class="btn btn-primary" @click="restore">{{ t('homepage.restore') }}</button>
          </div>
        </template>
      </BaseDialog>
    </main>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { homepageAPI, type HomepageState } from '@/api/admin/homepage'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import { useAppStore } from '@/stores/app'
import starterHTML from './homepage-starter.html?raw'

const { t, te, locale } = useI18n()
const appStore = useAppStore()
const documentPaths = ['/sitemap.xml', '/robots.txt']
const form = reactive({ enabled: false, public_origin: '', html: null as string | null })
const saved = ref<HomepageState>()
const loading = ref(false)
const action = ref<'save' | 'restore'>()
const confirmRestore = ref(false)
const conflict = ref(false)
const error = ref('')
const notice = ref('')

const busy = computed(() => loading.value || !!action.value)
const live = computed(() => !!saved.value?.enabled && !saved.value.backend_only)
const html = computed({
  get: () => form.html ?? makeStarterHTML(),
  set: (value: string) => { form.html = value },
})
const dirty = computed(() => {
  const state = saved.value
  return !!state && (form.enabled !== state.enabled || form.public_origin !== state.public_origin ||
    (form.html !== null && form.html !== state.html))
})

function makeStarterHTML() {
  const origin = form.public_origin.toLowerCase().replace(/\/$/, '')
  const canonical = /^https?:\/\/[^/?#@\\\s"<>]+$/.test(origin)
    ? `\n  <link rel="canonical" href="${escapeHTML(origin)}/">`
    : ''
  const values: Record<string, string> = {
    lang: locale.value,
    siteName: appStore.siteName,
    logo: appStore.siteLogo || '/logo.svg',
    subtitle: appStore.cachedPublicSettings?.site_subtitle || t('homepage.starterSubtitle'),
  }
  return starterHTML.replace(/\{\{([\w.]+)\}\}/g, (_, key: string) =>
    key === 'canonical' ? canonical : escapeHTML(values[key] ?? t(key)))
}

function escapeHTML(value: string) {
  return value.replace(/[&"<>]/g, char => ({ '&': '&amp;', '"': '&quot;', '<': '&lt;', '>': '&gt;' })[char]!)
}

function apply(state: HomepageState, keepEdits = false) {
  if (!keepEdits) {
    form.enabled = state.enabled
    form.public_origin = state.public_origin
  }
  if (!keepEdits || form.html === null) form.html = state.html || null
  saved.value = state
  conflict.value = false
}

function showError(cause: unknown) {
  const reason = (cause as { reason?: string })?.reason
  conflict.value = conflict.value || reason === 'HOMEPAGE_CONFLICT'
  const key = `homepage.errors.${reason}`
  error.value = t(reason && te(key) ? key : 'homepage.errors.failed')
}

async function load(keepEdits = false) {
  if (busy.value) return
  loading.value = true
  error.value = ''
  notice.value = ''
  try {
    apply(await homepageAPI.get(), keepEdits)
    if (keepEdits) notice.value = t('homepage.revisionLoaded')
  } catch (cause) {
    showError(cause)
  } finally {
    loading.value = false
  }
}

async function save() {
  const state = saved.value
  if (!state || busy.value || !dirty.value || conflict.value) return
  const config = {
    enabled: form.enabled,
    public_origin: form.public_origin,
    html: form.enabled ? html.value : form.html ?? '',
  }
  action.value = 'save'
  error.value = ''
  notice.value = ''
  try {
    apply(await homepageAPI.save(config, state.revision))
    notice.value = t('homepage.saved')
  } catch (cause) {
    showError(cause)
  } finally {
    action.value = undefined
  }
}

async function restore() {
  confirmRestore.value = false
  if (!saved.value?.has_previous || busy.value || conflict.value) return
  action.value = 'restore'
  error.value = ''
  notice.value = ''
  try {
    apply(await homepageAPI.restore(saved.value.revision))
    notice.value = t('homepage.restored')
  } catch (cause) {
    showError(cause)
  } finally {
    action.value = undefined
  }
}

onMounted(() => load())
</script>
