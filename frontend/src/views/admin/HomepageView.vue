<template>
  <AppLayout>
    <main class="mx-auto max-w-5xl space-y-6">
      <header class="flex flex-wrap items-center justify-between gap-4 border-b border-gray-200 pb-5 dark:border-dark-700">
        <h1 class="text-2xl font-semibold text-gray-900 dark:text-gray-100">{{ t('homepage.title') }}</h1>
        <a v-if="saved && !saved.backend_only" :href="live ? '/?landing=1' : '/'" target="_blank" rel="noopener noreferrer" class="btn btn-secondary gap-2">
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
        <section aria-labelledby="homepage-assets-title" class="space-y-3 border-t border-gray-200 pt-5 dark:border-dark-700">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <h2 id="homepage-assets-title" class="text-sm font-semibold text-gray-900 dark:text-gray-100">{{ t('homepage.assets.title') }}</h2>
            <input
              ref="fileInput"
              type="file"
              multiple
              :accept="['.html', ...assetExtensions].join(',')"
              :aria-label="t('homepage.assets.add')"
              aria-describedby="homepage-entry-hint"
              :disabled="busy"
              class="hidden"
              @change="selectAssets"
            />
            <button type="button" class="btn btn-secondary gap-2" :disabled="busy" @click="fileInput?.click()">
              <Icon name="upload" size="sm" />{{ t('homepage.assets.add') }}
            </button>
          </div>
          <p id="homepage-entry-hint" class="text-sm text-gray-700 dark:text-gray-200">{{ t('homepage.assets.entryHint') }}</p>
          <p class="text-sm text-gray-600 dark:text-gray-300">{{ t('homepage.assets.limits', { count: maxAssets, size: maxAssetMiB, total: maxTotalAssetMiB }) }}</p>
          <ul v-if="entryFile || saved.html || assetRows.length" class="divide-y divide-gray-200 border-y border-gray-200 dark:divide-dark-700 dark:border-dark-700">
            <li v-if="entryFile || saved.html" class="flex items-center justify-between gap-3 py-2">
              <div class="min-w-0">
                <p class="break-all text-sm font-medium text-gray-900 dark:text-gray-100">{{ entryFile?.name || saved.entry_file || t('homepage.assets.existingEntry') }}</p>
                <p class="text-xs text-gray-600 dark:text-gray-300">
                  {{ t('homepage.assets.entry') }}
                  <span v-if="entryFile" class="ml-2">{{ t('homepage.assets.pending') }}</span>
                </p>
              </div>
              <button v-if="entryFile" type="button" class="btn btn-secondary !p-2" :title="t('common.cancel')" :aria-label="t('homepage.assets.cancelEntry')" :disabled="busy" @click="entryFile = undefined">
                <Icon name="x" size="sm" />
              </button>
            </li>
            <li v-for="file in assetRows" :key="file.name" class="flex items-center justify-between gap-3 py-2">
              <div class="min-w-0">
                <p class="break-all text-sm font-medium text-gray-900 dark:text-gray-100">{{ file.name }}</p>
                <p class="text-xs text-gray-600 dark:text-gray-300">
                  {{ n(Math.max(1, Math.ceil(file.size / 1024))) }} KiB
                  <span v-if="assetChanges.has(file.name)" class="ml-2">{{ t('homepage.assets.pending') }}</span>
                </p>
              </div>
              <div class="flex shrink-0 gap-2">
                <button type="button" class="btn btn-secondary !p-2" :title="t('homepage.assets.copy')" :aria-label="t('homepage.assets.copyFile', { name: file.name })" @click="copyAssetPath(file.name)">
                  <Icon name="copy" size="sm" />
                </button>
                <button type="button" class="btn btn-secondary !p-2" :title="t('homepage.assets.remove')" :aria-label="t('homepage.assets.removeFile', { name: file.name })" :disabled="busy" @click="removeAsset(file.name)">
                  <Icon name="trash" size="sm" />
                </button>
              </div>
            </li>
          </ul>
          <p v-else class="text-sm text-gray-600 dark:text-gray-300">{{ t('homepage.assets.empty') }}</p>
        </section>
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
import { useClipboard } from '@/composables/useClipboard'

const { t, te, n, locale } = useI18n()
const { copyToClipboard } = useClipboard()
const documentPaths = ['/sitemap.xml', '/robots.txt']
const assetExtensions = ['.jpg', '.jpeg', '.png', '.webp', '.gif', '.css', '.woff2']
const assetNamePattern = /^[\p{L}\p{N}][\p{L}\p{N}._ -]*\.[a-zA-Z0-9]+$/u
const maxAssets = 32
const maxAssetMiB = 2
const maxTotalAssetMiB = 8
const bytesPerMiB = 1024 ** 2
const form = reactive({ enabled: false, public_origin: '' })
const fileInput = ref<HTMLInputElement>()
const entryFile = ref<File>()
const assetChanges = reactive(new Map<string, File | null>())
const saved = ref<HomepageState>()
const loading = ref(false)
const action = ref<'save' | 'restore'>()
const confirmRestore = ref(false)
const conflict = ref(false)
const error = ref('')
const notice = ref('')

function getAssetRows(changes: ReadonlyMap<string, File | null>) {
  const files = new Map<string, { name: string; size: number }>((saved.value?.assets ?? []).map(asset => [asset.name, asset]))
  for (const [name, file] of changes) {
    if (file) files.set(name, file)
    else files.delete(name)
  }
  return [...files.values()].sort((a, b) => a.name.localeCompare(b.name, locale.value))
}

const assetRows = computed(() => getAssetRows(assetChanges))
const busy = computed(() => loading.value || !!action.value)
const live = computed(() => !!saved.value?.enabled && !saved.value.backend_only)
const dirty = computed(() => {
  const state = saved.value
  return !!state && (form.enabled !== state.enabled || form.public_origin !== state.public_origin ||
    !!entryFile.value || assetChanges.size > 0)
})

function selectAssets(event: Event) {
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files ?? [])
  input.value = ''
  if (!files.length) return
  error.value = ''
  notice.value = ''
  const next = new Map(assetChanges)
  let selectedEntry: File | undefined
  for (const file of files) {
    const extension = file.name.slice(file.name.lastIndexOf('.')).toLowerCase()
    if (extension === '.html') {
      const name = file.name.toLowerCase()
      if (name !== 'index.html' && name !== 'home.html') {
        error.value = t('homepage.errors.HOMEPAGE_ENTRY_NAME')
        return
      }
      if (!file.size || file.size > bytesPerMiB) {
        error.value = t(file.size ? 'homepage.errors.HOMEPAGE_TOO_LARGE' : 'homepage.errors.HOMEPAGE_INVALID_HTML')
        return
      }
      if (!selectedEntry || name === 'index.html') selectedEntry = file
      continue
    }
    if (!assetExtensions.includes(extension) || !assetNamePattern.test(file.name) ||
      new TextEncoder().encode(file.name).length > 180 || file.size === 0) {
      error.value = t('homepage.errors.HOMEPAGE_ASSET_INVALID')
      return
    }
    if (file.size > maxAssetMiB * bytesPerMiB) {
      error.value = t('homepage.errors.HOMEPAGE_ASSETS_TOO_LARGE')
      return
    }
    next.set(file.name, file)
  }
  const rows = getAssetRows(next)
  if (rows.length > maxAssets || rows.reduce((total, file) => total + file.size, 0) > maxTotalAssetMiB * bytesPerMiB) {
    error.value = t('homepage.errors.HOMEPAGE_ASSETS_TOO_LARGE')
    return
  }
  for (const [name, file] of next) assetChanges.set(name, file)
  if (selectedEntry) entryFile.value = selectedEntry
}

function removeAsset(name: string) {
  if (saved.value?.assets?.some(asset => asset.name === name)) assetChanges.set(name, null)
  else assetChanges.delete(name)
  error.value = ''
  notice.value = ''
}

async function copyAssetPath(name: string) {
  if (!await copyToClipboard(`/homepage-assets/${encodeURIComponent(name)}`, t('homepage.assets.copied'))) {
    error.value = t('homepage.assets.copyFailed')
  }
}

function readAsset(file: File) {
  return new Promise<string>((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => {
      const value = String(reader.result)
      resolve(value.slice(value.indexOf(',') + 1))
    }
    reader.onerror = () => reject(reader.error)
    reader.onabort = () => reject(new Error('file read aborted'))
    reader.readAsDataURL(file)
  })
}

function readEntry(file: File) {
  return new Promise<string>((resolve, reject) => {
    const reader = new FileReader()
    const invalid = () => reject({ reason: 'HOMEPAGE_INVALID_HTML' })
    reader.onload = () => {
      try {
        resolve(new TextDecoder('utf-8', { fatal: true }).decode(reader.result as ArrayBuffer))
      } catch {
        invalid()
      }
    }
    reader.onerror = invalid
    reader.onabort = invalid
    reader.readAsArrayBuffer(file)
  })
}

function apply(state: HomepageState, keepEdits = false) {
  if (!keepEdits) {
    form.enabled = state.enabled
    form.public_origin = state.public_origin
    entryFile.value = undefined
    assetChanges.clear()
  } else {
    for (const [name, file] of assetChanges) {
      if (!file && !state.assets?.some(asset => asset.name === name)) assetChanges.delete(name)
    }
  }
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
  const entry = entryFile.value
  if (form.enabled && !entry && !state.html) {
    error.value = t('homepage.errors.HOMEPAGE_ENTRY_REQUIRED')
    return
  }
  action.value = 'save'
  error.value = ''
  notice.value = ''
  try {
    const config = {
      enabled: form.enabled,
      public_origin: form.public_origin,
      html: entry ? await readEntry(entry) : state.html,
      ...(entry ? { entry_file: entry.name.toLowerCase() } : {}),
    }
    const assets = assetChanges.size
      ? await Promise.all(assetRows.value.map(async ({ name }) => {
        const file = assetChanges.get(name)
        return file ? { name, data: await readAsset(file) } : { name }
      }))
      : undefined
    apply(await homepageAPI.save(assets ? { ...config, assets } : config, state.revision))
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
