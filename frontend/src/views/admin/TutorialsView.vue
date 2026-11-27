<template>
  <AppLayout>
    <div class="mx-auto max-w-7xl space-y-6">
      <header class="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">
            {{ t('tutorials.manage') }}
          </h1>
          <p class="mt-2 text-sm text-gray-600 dark:text-dark-300">
            {{ t('tutorials.description') }}
          </p>
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <button type="button" class="btn btn-secondary" @click="showApiKeys = true">
            <Icon name="key" size="sm" class="mr-2" />
            {{ t('tutorials.apiKeys.title') }}
          </button>
          <button
            type="button"
            class="btn btn-secondary"
            :disabled="importing"
            @click="choosePackage()"
          >
            <Icon name="upload" size="sm" class="mr-2" />
            {{ t(importing ? 'tutorials.importingPackage' : 'tutorials.importPackage') }}
          </button>
          <input ref="packageInput" type="file" accept=".zip,application/zip" hidden @change="importPackage" />
          <RouterLink to="/admin/tutorials/new" class="btn btn-primary">
            <Icon name="plus" size="sm" class="mr-2" />{{ t('tutorials.new') }}
          </RouterLink>
        </div>
      </header>
      <p
        v-if="packageError"
        class="break-words rounded-lg bg-red-50 p-4 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300"
        role="alert"
      >{{ packageError }}</p>
      <div class="flex flex-wrap items-center gap-3">
        <label class="min-w-52 flex-1"
          ><span class="sr-only">{{ t('tutorials.search') }}</span
          ><input
            v-model="search"
            class="input"
            :placeholder="t('tutorials.search')"
            type="search"
        /></label>
        <label
          ><span class="sr-only">{{ t('tutorials.category') }}</span
          ><select v-model="category" class="input">
            <option value="">{{ t('tutorials.allCategories') }}</option>
            <option v-for="c in categories" :key="c.id" :value="c.id">
              {{ c.name }}
            </option>
          </select></label
        >
        <label
          ><span class="sr-only">{{ t('tutorials.status') }}</span
          ><select v-model="status" class="input">
            <option value="">{{ t('tutorials.all') }}</option>
            <option value="draft">{{ t('tutorials.draft') }}</option>
            <option value="published">{{ t('tutorials.published') }}</option>
          </select></label
        >
        <button
          type="button"
          class="btn btn-secondary"
          :aria-expanded="showCategories"
          @click="showCategories = !showCategories"
        >
          {{ t('tutorials.manageCategories') }}
        </button>
      </div>
      <section
        v-if="showCategories"
        class="rounded-xl border border-gray-200 bg-white p-5 dark:border-dark-700 dark:bg-dark-900"
      >
        <h2 class="mb-4 font-semibold text-gray-900 dark:text-white">
          {{ t('tutorials.categories') }}
        </h2>
        <div
          v-for="c in categoryEdits"
          :key="c.id"
          class="mb-3 flex flex-wrap items-center gap-2"
        >
          <label class="min-w-40 flex-1"
            ><span class="sr-only">{{ t('tutorials.categoryName') }}</span
            ><input v-model="c.name" class="input" maxlength="80"
          /></label>
          <label class="w-24"
            ><span class="sr-only">{{ t('tutorials.order') }}</span
            ><input
              v-model.number="c.sort_order"
              class="input"
              type="number"
              min="-100000"
              max="100000"
          /></label>
          <button
            type="button"
            class="btn btn-secondary"
            :disabled="busy"
            @click="saveCategory(c)"
          >
            {{ t('common.save') }}
          </button>
          <button
            type="button"
            class="btn btn-secondary text-red-600 dark:text-red-400"
            :disabled="busy"
            :aria-label="t('tutorials.deleteCategory') + ': ' + c.name"
            @click="confirmation = { type: 'category', id: c.id }"
          >
            <Icon name="trash" size="sm" />
          </button>
        </div>
        <form
          class="flex flex-wrap gap-2 border-t border-gray-100 pt-4 dark:border-dark-700"
          @submit.prevent="
            saveCategory({ name: categoryName, sort_order: categories.length })
          "
        >
          <label class="min-w-40 flex-1"
            ><span class="sr-only">{{ t('tutorials.categoryName') }}</span
            ><input
              v-model="categoryName"
              class="input"
              :placeholder="t('tutorials.categoryName')"
              maxlength="80"
              required
          /></label>
          <button
            class="btn btn-secondary"
            type="submit"
            :disabled="busy || !categoryName.trim()"
          >
            <Icon name="plus" size="sm" class="mr-2" />{{
              t('tutorials.saveCategory')
            }}
          </button>
        </form>
      </section>
      <div
        v-if="error"
        class="flex items-center justify-between gap-3 rounded-lg bg-red-50 p-4 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300"
        role="alert"
      >
        {{ error
        }}<button class="underline" @click="reload">
          {{ t('tutorials.retry') }}
        </button>
      </div>
      <div
        class="overflow-hidden rounded-xl border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-900"
        :aria-busy="loading"
      >
        <div v-if="loading" class="space-y-5 p-6" aria-live="polite">
          <div
            v-for="i in 4"
            :key="i"
            class="h-12 animate-pulse rounded bg-gray-100 motion-reduce:animate-none dark:bg-dark-800"
          />
        </div>
        <div v-else-if="!items.length" class="px-6 py-16 text-center">
          <Icon
            name="document"
            size="xl"
            class="mx-auto mb-4 text-primary-700 dark:text-primary-300"
          />
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
            {{
              t(
                search || category || status
                  ? 'tutorials.noResults'
                  : 'tutorials.noTutorials',
              )
            }}
          </h2>
          <p
            class="mx-auto mt-2 max-w-md text-sm text-gray-600 dark:text-dark-300"
          >
            {{
              t(
                search || category || status
                  ? 'tutorials.noResultsHint'
                  : 'tutorials.noTutorialsHint',
              )
            }}
          </p>
          <RouterLink
            v-if="!search && !category && !status"
            to="/admin/tutorials/new"
            class="btn btn-primary mt-5"
            >{{ t('tutorials.new') }}</RouterLink
          >
        </div>
        <div v-else class="overflow-x-auto">
          <table class="w-full text-left text-sm">
            <thead
              class="border-b border-gray-200 bg-gray-50 text-gray-600 dark:border-dark-700 dark:bg-dark-800 dark:text-dark-300"
            >
              <tr>
                <th class="px-5 py-3 font-medium">
                  {{ t('tutorials.titleLabel') }}
                </th>
                <th class="px-4 py-3 font-medium">
                  {{ t('tutorials.category') }}
                </th>
                <th class="px-4 py-3 font-medium">
                  {{ t('tutorials.status') }}
                </th>
                <th class="whitespace-nowrap px-4 py-3 font-medium">
                  {{ t('tutorials.updated') }}
                </th>
                <th class="px-5 py-3 text-right font-medium">
                  {{ t('tutorials.actions') }}
                </th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
              <tr
                v-for="a in items"
                :key="a.id"
                class="hover:bg-gray-50 dark:hover:bg-dark-800/60"
              >
                <td class="min-w-52 max-w-sm px-5 py-5">
                  <RouterLink
                    :to="`/admin/tutorials/${a.id}/edit`"
                    class="tutorial-focus font-medium text-gray-900 hover:text-primary-700 dark:text-white dark:hover:text-primary-300"
                    >{{ a.title || t('tutorials.new') }}</RouterLink
                  ><span
                    v-if="a.pinned"
                    class="ml-2 text-xs text-primary-700 dark:text-primary-300"
                    >{{ t('tutorials.pinned') }}</span
                  >
                  <p
                    v-if="a.summary"
                    class="mt-1 truncate text-xs text-gray-600 dark:text-dark-300"
                  >
                    {{ a.summary }}
                  </p>
                </td>
                <td
                  class="whitespace-nowrap px-4 py-5 text-gray-600 dark:text-dark-300"
                >
                  {{ categoryLabel(a.category_id) }}
                </td>
                <td class="whitespace-nowrap px-4 py-5">
                  <span
                    :class="
                      a.published ? 'badge badge-success' : 'badge badge-gray'
                    "
                    >{{
                      t(a.published ? 'tutorials.published' : 'tutorials.draft')
                    }}</span
                  >
                  <p class="mt-1 text-xs text-gray-600 dark:text-dark-300">
                    {{
                      t(
                        a.visibility === 'public'
                          ? 'tutorials.public'
                          : 'tutorials.loginOnly',
                      )
                    }}
                  </p>
                  <p
                    v-if="a.has_changes"
                    class="mt-1 text-xs text-amber-700 dark:text-amber-300"
                  >
                    {{ t('tutorials.unpublishedChanges') }}
                  </p>
                </td>
                <td
                  class="whitespace-nowrap px-4 py-5 text-xs text-gray-600 dark:text-dark-300"
                >
                  {{ formatDate(a.updated_at) }}
                </td>
                <td class="px-5 py-5">
                  <div
                    class="flex items-center justify-end gap-3 whitespace-nowrap"
                  >
                    <RouterLink
                      :to="`/admin/tutorials/${a.id}/edit`"
                      class="tutorial-focus text-primary-700 hover:underline dark:text-primary-300"
                      >{{ t('common.edit') }}</RouterLink
                    >
                    <RouterLink
                      :to="`/admin/tutorials/${a.id}/edit?preview=1`"
                      class="tutorial-focus text-gray-600 hover:underline dark:text-dark-300"
                      >{{ t('tutorials.preview') }}</RouterLink
                    >
                    <button
                      v-if="a.published"
                      type="button"
                      class="tutorial-focus text-gray-600 hover:underline dark:text-dark-300"
                      @click="copyLink(a)"
                    >
                      {{ t('tutorials.copyLink') }}
                    </button>
                    <button
                      type="button"
                      class="tutorial-focus text-gray-600 hover:underline disabled:opacity-50 dark:text-dark-300"
                      :disabled="importing || busy"
                      :aria-label="`${t('tutorials.overwritePackage')}: ${a.title}`"
                      @click="choosePackage(a)"
                    >
                      {{ t('tutorials.overwritePackage') }}
                    </button>
                    <button
                      v-if="a.published"
                      type="button"
                      class="tutorial-focus text-gray-600 hover:underline dark:text-dark-300"
                      :disabled="busy"
                      @click="confirmation = { type: 'unpublish', article: a }"
                    >
                      {{ t('tutorials.unpublish') }}
                    </button>
                    <button
                      type="button"
                      class="tutorial-focus text-red-600 hover:underline dark:text-red-400"
                      :disabled="busy"
                      :aria-label="t('tutorials.delete') + ': ' + a.title"
                      @click="confirmation = { type: 'delete', article: a }"
                    >
                      <Icon name="trash" size="sm" />
                    </button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <footer
          v-if="total"
          class="flex flex-wrap items-center justify-between gap-3 border-t border-gray-200 px-5 py-4 text-sm text-gray-600 dark:border-dark-700 dark:text-dark-300"
        >
          <span>{{ t('tutorials.total', { count: total }) }}</span>
          <div class="flex items-center gap-3">
            <button
              class="btn btn-secondary btn-sm"
              :disabled="page <= 1 || loading"
              @click="page--"
            >
              {{ t('tutorials.previous') }}</button
            ><span>{{ t('tutorials.page', { page, pages }) }}</span
            ><button
              class="btn btn-secondary btn-sm"
              :disabled="page >= pages || loading"
              @click="page++"
            >
              {{ t('tutorials.next') }}
            </button>
          </div>
        </footer>
      </div>
    </div>
    <TutorialApiKeysDialog v-if="showApiKeys" @close="showApiKeys = false" />
    <ConfirmDialog
      :show="!!confirmation"
      :title="
        t(
          confirmation?.type === 'category'
            ? 'tutorials.deleteCategory'
            : confirmation?.type === 'delete'
              ? 'tutorials.delete'
              : 'tutorials.unpublish',
        )
      "
      :message="
        t(
          confirmation?.type === 'category'
            ? 'tutorials.categoryDelete'
            : confirmation?.type === 'delete'
              ? 'tutorials.deleteConfirm'
              : 'tutorials.unpublishConfirm',
        )
      "
      :confirm-text="t('tutorials.confirm')"
      :cancel-text="t('tutorials.cancel')"
      danger
      @confirm="confirmAction"
      @cancel="confirmation = null"
    />
    <ConfirmDialog
      :show="!!pendingOverwrite"
      :title="t('tutorials.overwritePackage')"
      :message="t('tutorials.overwritePackageConfirm', { title: pendingOverwrite?.target.title, file: pendingOverwrite?.file.name })"
      :confirm-text="t('tutorials.overwritePackage')"
      :cancel-text="t('tutorials.cancel')"
      danger
      @confirm="overwritePackage"
      @cancel="pendingOverwrite = null"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import {
  maxTutorialPackageBytes,
  tutorialAPI,
  tutorialPath,
  type Tutorial,
  type TutorialCategory,
} from '@/api/tutorials'
import { useAppStore } from '@/stores'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import TutorialApiKeysDialog from '@/components/tutorials/TutorialApiKeysDialog.vue'
import { tutorialPackageError } from '@/utils/tutorial-package'
import '@/styles/tutorials.css'

const showApiKeys = ref(false)
const { t, locale } = useI18n(),
  app = useAppStore(),
  router = useRouter()
const packageInput = ref<HTMLInputElement>()
const pendingOverwrite = ref<{ target: Tutorial; file: File } | null>(null)
let packageTarget: Tutorial | undefined
const importing = ref(false)
const packageError = ref('')
let disposed = false
const search = ref(''),
  category = ref<number | ''>(''),
  status = ref(''),
  page = ref(1),
  pages = ref(1),
  total = ref(0)
const items = ref<Tutorial[]>([]),
  categories = ref<TutorialCategory[]>([]),
  categoryEdits = ref<TutorialCategory[]>([])
const loading = ref(true),
  busy = ref(false),
  error = ref(''),
  showCategories = ref(false),
  categoryName = ref('')
const confirmation = ref<
  | { type: 'category'; id: number }
  | { type: 'unpublish' | 'delete'; article: Tutorial }
  | null
>(null)
let timer: ReturnType<typeof setTimeout>,
  controller: AbortController | undefined
const categoryLabel = (id: number | null) =>
  categories.value.find((c) => c.id === id)?.name ||
  t('tutorials.uncategorized')
const formatDate = (date: string) =>
  new Date(date).toLocaleString(locale.value, {
    dateStyle: 'short',
    timeStyle: 'short',
  })
const message = (e: unknown) =>
  (e as { message?: string })?.message || t('tutorials.loadFailed')
function choosePackage(target?: Tutorial) {
  packageTarget = target
  packageInput.value?.click()
}
function importPackage(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  const target = packageTarget
  packageTarget = undefined
  input.value = ''
  if (!file || importing.value) return
  if (file.size > maxTutorialPackageBytes) {
    packageError.value = t('tutorials.packageErrors.zipSize')
    return
  }
  packageError.value = ''
  if (target) pendingOverwrite.value = { target, file }
  else void uploadPackage(file)
}
function overwritePackage() {
  const pending = pendingOverwrite.value
  pendingOverwrite.value = null
  if (pending) void uploadPackage(pending.file, pending.target)
}
async function uploadPackage(file: File, target?: Tutorial) {
  if (importing.value) return
  importing.value = true
  try {
    const draft = await tutorialAPI.importPackage(file, target)
    if (disposed) return
    app.showSuccess(t('tutorials.importSuccess'))
    await router.push({ path: `/admin/tutorials/${draft.id}/edit`, query: { preview: '1' } })
  } catch (e) {
    if (!disposed) {
      const conflict = (e as { reason?: string }).reason === 'TUTORIAL_CONFLICT'
      packageError.value = conflict ? t('tutorials.overwriteConflict') : tutorialPackageError(t, e, t('tutorials.importFailed'))
      if (conflict) void load()
    }
  } finally {
    importing.value = false
  }
}
async function load() {
  controller?.abort()
  const request = new AbortController()
  controller = request
  loading.value = true
  try {
    const data = await tutorialAPI.list(
      true,
      {
        page: page.value,
        page_size: 20,
        search: search.value,
        category_id: category.value || undefined,
        status: status.value,
      },
      request.signal,
    )
    if (request.signal.aborted) return
    items.value = data.items
    total.value = data.total
    pages.value = Math.max(1, data.pages)
    error.value = ''
    if (page.value > pages.value) page.value = pages.value
  } catch (e) {
    if (!request.signal.aborted) error.value = message(e)
  } finally {
    if (!request.signal.aborted) loading.value = false
  }
}
async function loadCategories() {
  categories.value = await tutorialAPI.categories(true)
  categoryEdits.value = categories.value.map((c) => ({ ...c }))
}
async function reload() {
  try {
    await loadCategories()
    await load()
  } catch (e) {
    error.value = message(e)
    loading.value = false
  }
}
async function saveCategory(c: Omit<TutorialCategory, 'id'> & { id?: number }) {
  if (busy.value) return
  busy.value = true
  try {
    await tutorialAPI.saveCategory(c)
    categoryName.value = ''
    await loadCategories()
    app.showSuccess(t('tutorials.categorySaved'))
    error.value = ''
  } catch (e) {
    error.value = message(e)
  } finally {
    busy.value = false
  }
}
async function confirmAction() {
  const action = confirmation.value
  confirmation.value = null
  if (!action || busy.value) return
  busy.value = true
  try {
    if (action.type === 'category') {
      await tutorialAPI.deleteCategory(action.id)
      if (category.value === action.id) category.value = ''
      await loadCategories()
      app.showSuccess(t('tutorials.categoryRemoved'))
    } else {
      await tutorialAPI.action(
        action.article.id,
        action.article.version,
        action.type,
      )
      app.showSuccess(
        t(
          action.type === 'delete'
            ? 'tutorials.deleteSuccess'
            : 'tutorials.unpublishSuccess',
        ),
      )
    }
    await load()
  } catch (e) {
    error.value = message(e)
  } finally {
    busy.value = false
  }
}
async function copyLink(article: Tutorial) {
  try {
    await navigator.clipboard.writeText(location.origin + tutorialPath(article.id, article.route_slug))
    app.showSuccess(t('tutorials.linkCopied'))
  } catch {
    app.showError(t('tutorials.copyFailed'))
  }
}
watch([search, category, status], () => {
  clearTimeout(timer)
  timer = setTimeout(() => {
    if (page.value !== 1) page.value = 1
    else void load()
  }, 250)
})
watch(page, () => void load())
onMounted(reload)
onBeforeUnmount(() => {
  disposed = true
  controller?.abort()
  clearTimeout(timer)
})
</script>
