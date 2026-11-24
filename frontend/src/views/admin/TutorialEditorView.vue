<template>
  <AppLayout>
    <div class="mx-auto max-w-[1500px] space-y-5">
      <header
        class="sticky top-0 z-20 -mx-1 flex flex-wrap items-center justify-between gap-3 bg-gray-50/95 px-1 py-3 dark:bg-dark-950/95"
      >
        <div class="flex flex-wrap items-center gap-3">
          <RouterLink
            to="/admin/tutorials"
            class="tutorial-focus flex items-center gap-1 text-sm text-gray-600 hover:text-primary-700 dark:text-dark-300"
            ><Icon name="chevronLeft" size="sm" />{{
              t('tutorials.manage')
            }}</RouterLink
          >
          <span class="text-gray-300 dark:text-dark-600">/</span>
          <h1 class="text-base font-semibold text-gray-900 dark:text-white">
            {{ t(article ? 'tutorials.edit' : 'tutorials.new') }}
          </h1>
          <span
            class="text-xs text-gray-600 dark:text-dark-300"
            aria-live="polite"
            >{{ saveStatus }}</span
          >
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <button
            v-if="article && !loading"
            type="button"
            class="btn btn-secondary"
            :disabled="packageBusy || publishing || uploads > 0 || conflict"
            @click="packageInput?.click()"
          >
            <Icon name="upload" size="sm" class="mr-2" />
            {{ t(importing ? 'tutorials.importingPackage' : 'tutorials.importPackage') }}
          </button>
          <input ref="packageInput" type="file" accept=".zip,application/zip" hidden @change="pickPackage" />
          <button
            v-if="article && !loading"
            type="button"
            class="btn btn-secondary"
            :disabled="packageBusy || publishing || uploads > 0 || conflict"
            @click="exportPackage"
          >
            <Icon name="download" size="sm" class="mr-2" />
            {{ t(exporting ? 'tutorials.exportingPackage' : 'tutorials.exportPackage') }}
          </button>
          <button
            type="button"
            class="btn btn-secondary"
            :disabled="loading || saving || conflict || packageBusy || !dirty"
            @click="save(false)"
          >
            {{ t('tutorials.save') }}
          </button>
          <button
            type="button"
            class="btn btn-secondary"
            :disabled="loading || importing"
            @click="mode = mode === 'preview' ? 'split' : 'preview'"
          >
            {{
              t(mode === 'preview' ? 'tutorials.markdown' : 'tutorials.preview')
            }}
          </button>
          <button
            type="button"
            class="btn btn-primary"
            :disabled="
              loading ||
              saving ||
              publishing ||
              packageBusy ||
              uploads > 0 ||
              !!failedUpload ||
              conflict
            "
            @click="publish"
          >
            {{
              t(
                article?.published
                  ? 'tutorials.publishUpdate'
                  : 'tutorials.publish',
              )
            }}
          </button>
        </div>
      </header>
      <div
        v-if="loading"
        class="h-96 animate-pulse rounded-xl bg-gray-100 motion-reduce:animate-none dark:bg-dark-800"
      />
      <div
        v-else-if="loadError"
        class="rounded-xl border border-red-200 bg-white p-8 dark:border-red-900 dark:bg-dark-900"
        role="alert"
      >
        <p class="text-red-700 dark:text-red-300">{{ loadError }}</p>
        <button class="btn btn-secondary mt-4" @click="load">
          {{ t('tutorials.retry') }}
        </button>
      </div>
      <div v-else class="space-y-5" :inert="importing">
        <p
          v-if="packageError"
          class="break-words rounded-lg bg-red-50 p-4 text-sm text-red-700 dark:bg-red-950/40 dark:text-red-300"
          role="alert"
        >{{ packageError }}</p>
        <div
          v-if="error"
          class="flex flex-wrap items-center justify-between gap-3 rounded-lg bg-red-50 p-4 text-sm text-red-700 dark:bg-red-950/40 dark:text-red-300"
          role="alert"
        >
          <span>{{ error }}</span
          ><button v-if="conflict" class="underline" @click="reloadArticle">
            {{ t('tutorials.reload') }}</button
          ><button v-else class="underline" @click="save(false)">
            {{ t('tutorials.retry') }}
          </button>
        </div>
        <div
          v-if="article?.has_changes"
          class="flex items-center gap-2 text-sm text-amber-700 dark:text-amber-300"
        >
          <Icon name="clock" size="sm" />{{ t('tutorials.unpublishedChanges') }}
        </div>
        <section
          class="rounded-xl border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-900"
        >
          <div class="space-y-4 p-5 md:px-7 md:py-6">
            <label class="block"
              ><span
                class="mb-2 block text-sm font-medium text-gray-700 dark:text-dark-200"
                >{{ t('tutorials.titleLabel') }}</span
              ><input
                v-model="form.title"
                class="input !text-xl !font-semibold"
                maxlength="160"
                :placeholder="t('tutorials.titlePlaceholder')"
            /></label>
            <div class="flex flex-wrap items-end justify-between gap-4">
              <label class="min-w-48">
                <span
                  class="mb-1.5 block text-sm text-gray-600 dark:text-dark-300"
                  >{{ t('tutorials.visibility') }}</span
                >
                <select v-model="form.visibility" class="input">
                  <option value="login">{{ t('tutorials.loginOnly') }}</option>
                  <option value="public">{{ t('tutorials.public') }}</option>
                </select>
              </label>
              <label class="min-w-48"
                ><span
                  class="mb-1.5 block text-sm text-gray-600 dark:text-dark-300"
                  >{{ t('tutorials.category') }}</span
                ><select v-model="form.category_id" class="input">
                  <option :value="null">
                    {{ t('tutorials.uncategorized') }}
                  </option>
                  <option v-for="c in categories" :key="c.id" :value="c.id">
                    {{ c.name }}
                  </option>
                </select></label
              >
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                :aria-expanded="settings"
                @click="settings = !settings"
              >
                {{ t('tutorials.settings')
                }}<Icon name="chevronDown" size="sm" class="ml-2" />
              </button>
            </div>
            <p class="text-xs leading-5 text-gray-600 dark:text-dark-300">
              {{
                t(
                  form.visibility === 'public'
                    ? 'tutorials.publicHint'
                    : 'tutorials.loginHint',
                )
              }}
              {{ t('tutorials.visibilityPublishHint') }}
            </p>
            <div
              v-if="settings"
              class="space-y-4 border-t border-gray-100 pt-4 dark:border-dark-700"
            >
              <label class="block"
                ><span
                  class="mb-1.5 block text-sm text-gray-600 dark:text-dark-300"
                  >{{ t('tutorials.summary') }}</span
                ><textarea
                  v-model="form.summary"
                  class="input"
                  maxlength="500"
                  rows="2"
                  :placeholder="t('tutorials.summaryPlaceholder')"
                />
              </label>
              <div class="flex flex-wrap items-center gap-6">
                <label
                  class="flex items-center gap-2 text-sm text-gray-700 dark:text-dark-200"
                  ><input
                    v-model="form.pinned"
                    type="checkbox"
                    class="h-4 w-4 rounded text-primary-600"
                  />{{ t('tutorials.pinned') }}</label
                ><label
                  class="flex items-center gap-2 text-sm text-gray-700 dark:text-dark-200"
                  >{{ t('tutorials.order')
                  }}<input
                    v-model.number="form.sort_order"
                    class="input !w-24"
                    type="number"
                    min="-100000"
                    max="100000" /></label
                ><span class="text-xs text-gray-600 dark:text-dark-300">{{
                  t('tutorials.orderHint')
                }}</span>
              </div>
            </div>
          </div>
          <div
            class="flex flex-wrap items-center justify-between gap-3 border-y border-gray-200 bg-gray-50 px-5 py-3 dark:border-dark-700 dark:bg-dark-800"
          >
            <div
              class="flex flex-wrap gap-1"
              role="group"
              :aria-label="t('tutorials.markdown')"
            >
              <button
                v-for="tool in tools"
                :key="tool.key"
                type="button"
                class="tutorial-focus rounded px-2.5 py-2 text-sm text-gray-700 hover:bg-gray-200 dark:text-dark-200 dark:hover:bg-dark-700"
                :title="t(`tutorials.${tool.key}`)"
                :aria-label="t(`tutorials.${tool.key}`)"
                @click="insert(tool.before, tool.after, tool.sample)"
              >
                {{ tool.label }}
              </button>
              <button
                type="button"
                class="tutorial-focus rounded p-2 text-gray-700 hover:bg-gray-200 disabled:opacity-40 dark:text-dark-200 dark:hover:bg-dark-700"
                :aria-label="t('tutorials.undo')"
                :disabled="historyIndex <= 0"
                @click="undo(-1)"
              >
                <Icon name="undo" size="sm" />
              </button>
              <button
                type="button"
                class="tutorial-focus rounded p-2 text-gray-700 hover:bg-gray-200 disabled:opacity-40 dark:text-dark-200 dark:hover:bg-dark-700"
                :aria-label="t('tutorials.redo')"
                :disabled="historyIndex >= history.length - 1"
                @click="undo(1)"
              >
                <Icon name="redo" size="sm" />
              </button>
              <button
                type="button"
                class="tutorial-focus flex items-center gap-1 rounded px-2 py-2 text-sm text-gray-700 hover:bg-gray-200 dark:text-dark-200 dark:hover:bg-dark-700"
                :disabled="uploads > 0 || conflict"
                @click="imageInput?.click()"
              >
                <Icon name="upload" size="sm" />{{ t('tutorials.uploadImage') }}
              </button>
              <input
                ref="imageInput"
                type="file"
                accept="image/png,image/jpeg,image/gif"
                class="hidden"
                @change="fileSelected"
              />
            </div>
            <div class="flex items-center gap-3">
              <label
                v-if="!form.content"
                class="text-sm text-gray-600 dark:text-dark-300"
                ><span class="sr-only">{{ t('tutorials.template') }}</span
                ><select
                  class="input !py-1.5 !text-sm"
                  value=""
                  @change="applyTemplate"
                >
                  <option value="" disabled>
                    {{ t('tutorials.template') }}
                  </option>
                  <option value="quickStart">
                    {{ t('tutorials.quickStart') }}
                  </option>
                  <option value="clientSetup">
                    {{ t('tutorials.clientSetup') }}
                  </option>
                  <option value="faq">{{ t('tutorials.faq') }}</option>
                </select></label
              >
              <div
                class="flex rounded-md border border-gray-200 dark:border-dark-600"
                role="group"
                :aria-label="t('tutorials.preview')"
              >
                <button
                  v-for="m in modes"
                  :key="m.value"
                  class="tutorial-focus px-3 py-1.5 text-xs"
                  :class="[
                    mode === m.value
                      ? 'bg-white font-semibold text-primary-800 dark:bg-dark-700 dark:text-primary-200'
                      : 'text-gray-600 dark:text-dark-300',
                    m.value === 'split' ? 'hidden lg:block' : '',
                  ]"
                  :aria-pressed="mode === m.value"
                  @click="mode = m.value"
                >
                  {{ t(`tutorials.${m.key}`) }}
                </button>
              </div>
            </div>
          </div>
          <div
            v-if="uploads || failedUpload"
            class="flex flex-wrap items-center gap-3 border-b border-gray-200 px-5 py-3 text-sm dark:border-dark-700"
            aria-live="polite"
          >
            <span v-if="uploads" class="text-primary-800 dark:text-primary-200"
              >{{ t('tutorials.uploading') }} {{ uploadProgress }}%</span
            >
            <template v-else
              ><span class="text-red-700 dark:text-red-300">{{
                t('tutorials.uploadFailed')
              }}</span
              ><button class="underline" @click="retryUpload">
                {{ t('tutorials.retryUpload') }}</button
              ><button class="underline" @click="failedUpload = null">
                {{ t('common.cancel') }}
              </button></template
            >
          </div>
          <div class="tutorial-editor-grid" :class="`mode-${mode}`">
            <div
              class="tutorial-editor-source min-w-0 border-gray-200 lg:border-r dark:border-dark-700"
            >
              <label for="tutorial-markdown" class="sr-only">{{
                t('tutorials.contentLabel')
              }}</label>
              <textarea
                id="tutorial-markdown"
                ref="textarea"
                v-model="form.content"
                spellcheck="false"
                class="block min-h-[540px] w-full resize-y bg-transparent p-5 font-mono text-sm leading-7 text-gray-800 outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-primary-600 md:p-7 dark:text-dark-100"
                :placeholder="'## ' + t('tutorials.quickStart')"
                @input="recordHistory"
                @keydown="editorKeydown"
                @paste="paste"
              />
            </div>
            <section
              class="tutorial-editor-preview min-w-0 p-5 md:p-7"
              :aria-label="t('tutorials.preview')"
            >
              <p
                class="mb-6 text-xs font-medium text-gray-600 dark:text-dark-300"
              >
                {{ t('tutorials.previewNotice') }}
              </p>
              <h2
                v-if="form.title"
                class="mb-6 break-words text-2xl font-semibold text-gray-900 dark:text-white"
              >
                {{ form.title }}
              </h2>
              <TutorialMarkdown
                v-if="form.content"
                :content="form.content"
                :article-id="article?.id"
                admin
              />
              <p
                v-else
                class="py-16 text-center text-sm text-gray-600 dark:text-dark-300"
              >
                {{ t('tutorials.emptyPreview') }}
              </p>
            </section>
          </div>
          <footer
            class="border-t border-gray-200 px-5 py-3 text-xs text-gray-600 dark:border-dark-700 dark:text-dark-300"
          >
            {{ t('tutorials.imageHint') }}
          </footer>
        </section>
      </div>
    </div>
    <ConfirmDialog
      :show="!!pendingPackage"
      :title="t('tutorials.replaceDraft')"
      :message="t('tutorials.replaceDraftConfirm')"
      :confirm-text="t('tutorials.importPackage')"
      @confirm="importPackage"
      @cancel="pendingPackage = null"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  reactive,
  ref,
  watch,
} from 'vue'
import {
  onBeforeRouteLeave,
  onBeforeRouteUpdate,
  useRoute,
  useRouter,
} from 'vue-router'
import { useI18n } from 'vue-i18n'
import { saveAs } from 'file-saver'
import {
  emptyTutorial,
  maxTutorialPackageBytes,
  tutorialAPI,
  type Tutorial,
  type TutorialCategory,
  type TutorialContent,
} from '@/api/tutorials'
import { useAppStore } from '@/stores'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import TutorialMarkdown from '@/components/tutorials/TutorialMarkdown.vue'
import { tutorialPackageError } from '@/utils/tutorial-package'

const { t, locale } = useI18n(),
  route = useRoute(),
  router = useRouter(),
  app = useAppStore()
const form = reactive<TutorialContent>(emptyTutorial()),
  article = ref<Tutorial>(),
  categories = ref<TutorialCategory[]>([])
const loading = ref(true),
  loadError = ref(''),
  error = ref(''),
  saving = ref(false),
  publishing = ref(false),
  conflict = ref(false),
  settings = ref(false)
const textarea = ref<HTMLTextAreaElement>(),
  imageInput = ref<HTMLInputElement>(),
  packageInput = ref<HTMLInputElement>()
const pendingPackage = ref<File | null>(null)
const importing = ref(false),
  exporting = ref(false),
  packageError = ref('')
const packageBusy = computed(() => importing.value || exporting.value)
const mode = ref<'edit' | 'split' | 'preview'>(
  route.query.preview
    ? 'preview'
    : matchMedia('(max-width: 1023px)').matches
      ? 'edit'
      : 'split',
)
const modes = [
  { value: 'edit' as const, key: 'markdown' },
  { value: 'split' as const, key: 'split' },
  { value: 'preview' as const, key: 'preview' },
]
const snapshot = () => JSON.stringify(form)
const saved = ref(snapshot()),
  savedAt = ref('')
const dirty = computed(() => snapshot() !== saved.value)
const uploads = ref(0),
  uploadProgress = ref(0),
  failedUpload = ref<File | null>(null)
const history = ref<string[]>(['']),
  historyIndex = ref(0)
let timer: ReturnType<typeof setTimeout>,
  inFlight: Promise<boolean> | null = null,
  internalNavigation = false,
  disposed = false
let editorEpoch = 0
const saveStatus = computed(() =>
  saving.value
    ? t('tutorials.saving')
    : error.value
      ? t('tutorials.saveFailed')
      : dirty.value
        ? t('tutorials.unsaved')
        : article.value
          ? `${t('tutorials.saved')} ${savedAt.value}`
          : '',
)
const tools = [
  { key: 'heading', label: 'H₂', before: '\n## ', after: '\n', sample: '' },
  { key: 'bold', label: 'B', before: '**', after: '**', sample: '' },
  { key: 'link', label: '↗', before: '[', after: '](https://)', sample: '' },
  {
    key: 'code',
    label: '</>',
    before: '\n```bash\n',
    after: '\n```\n',
    sample: '',
  },
  {
    key: 'table',
    label: '▦',
    before: '\n| ',
    after: ' |\n| --- |\n|  |\n',
    sample: '',
  },
]
function recordHistory() {
  if (history.value[historyIndex.value] === form.content) return
  history.value = [
    ...history.value.slice(0, historyIndex.value + 1),
    form.content,
  ].slice(-150)
  historyIndex.value = history.value.length - 1
}
function undo(direction: number) {
  const index = historyIndex.value + direction
  if (index < 0 || index >= history.value.length) return
  historyIndex.value = index
  form.content = history.value[index]!
  textarea.value?.focus()
}
function insert(before: string, after = '', sample = '') {
  const start = textarea.value?.selectionStart ?? form.content.length,
    end = textarea.value?.selectionEnd ?? start
  const selected = form.content.slice(start, end) || sample
  form.content =
    form.content.slice(0, start) +
    before +
    selected +
    after +
    form.content.slice(end)
  recordHistory()
  void nextTick(() => {
    textarea.value?.focus()
    textarea.value?.setSelectionRange(
      start + before.length,
      start + before.length + selected.length,
    )
  })
}
function editorKeydown(e: KeyboardEvent) {
  if (e.key === 'Tab') {
    e.preventDefault()
    insert('  ')
  }
  if (
    (e.ctrlKey || e.metaKey) &&
    ['z', 'y', 's'].includes(e.key.toLowerCase())
  ) {
    e.preventDefault()
    if (e.key.toLowerCase() === 's') void save(false)
    else undo(e.key.toLowerCase() === 'y' || e.shiftKey ? 1 : -1)
  }
}
async function load() {
  const current = ++editorEpoch
  loading.value = true
  loadError.value = ''
  clearTimeout(timer)
  try {
    const id = Number(route.params.id)
    const [loadedCategories, loadedArticle] = await Promise.all([
      tutorialAPI.categories(true),
      id ? tutorialAPI.get(id, true) : undefined,
    ])
    if (current !== editorEpoch || disposed) return
    categories.value = loadedCategories
    article.value = loadedArticle
    Object.assign(
      form,
      article.value ? pickContent(article.value) : emptyTutorial(),
    )
    saved.value = snapshot()
    history.value = [form.content]
    historyIndex.value = 0
    error.value = ''
    packageError.value = ''
    pendingPackage.value = null
    conflict.value = false
    savedAt.value = ''
  } catch (e) {
    if (current === editorEpoch)
      loadError.value = (e as Error).message || t('tutorials.loadFailed')
  } finally {
    if (current === editorEpoch) loading.value = false
  }
}
function pickContent(a: Tutorial): TutorialContent {
  return {
    visibility: a.visibility || 'login',
    title: a.title,
    summary: a.summary,
    content: a.content,
    category_id: a.category_id,
    pinned: a.pinned,
    sort_order: a.sort_order,
  }
}
function save(silent = true): Promise<boolean> {
  clearTimeout(timer)
  if (inFlight)
    return inFlight.then((ok) => (ok && dirty.value ? save(silent) : ok))
  if (conflict.value || loading.value || importing.value || disposed) return Promise.resolve(false)
  if (!dirty.value && article.value) return Promise.resolve(true)
  const captured = snapshot()
  const current = editorEpoch
  saving.value = true
  inFlight = (async () => {
    try {
      const content: TutorialContent = JSON.parse(captured)
      const created = !article.value
      const result = article.value
        ? await tutorialAPI.save(
            article.value.id,
            article.value.version,
            content,
          )
        : await tutorialAPI.create(content)
      if (disposed || current !== editorEpoch) return false
      article.value = result
      saved.value = captured
      error.value = ''
      savedAt.value = new Date().toLocaleTimeString(locale.value, {
        hour: '2-digit',
        minute: '2-digit',
      })
      if (created) {
        internalNavigation = true
        await router.replace({ path: `/admin/tutorials/${result.id}/edit` })
        internalNavigation = false
      }
      if (!silent) app.showSuccess(t('tutorials.saved'))
      return true
    } catch (e) {
      if (disposed || current !== editorEpoch) return false
      const code = (e as { reason?: string }).reason
      conflict.value =
        (e as { status?: number }).status === 409 ||
        code === 'TUTORIAL_CONFLICT'
      error.value = conflict.value
        ? t('tutorials.conflict')
        : `${t('tutorials.saveFailed')} ${(e as Error).message || ''}`
      return false
    } finally {
      saving.value = false
      inFlight = null
      if (!disposed && dirty.value && !error.value && !publishing.value)
        timer = setTimeout(() => void save(), 1600)
    }
  })()
  return inFlight
}
function pickPackage(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file || packageBusy.value) return
  if (file.size > maxTutorialPackageBytes) {
    packageError.value = t('tutorials.packageErrors.zipSize')
    return
  }
  packageError.value = ''
  pendingPackage.value = file
}
async function importPackage() {
  const file = pendingPackage.value
  pendingPackage.value = null
  if (!file || !article.value || packageBusy.value || publishing.value || uploads.value || conflict.value) return
  const current = editorEpoch
  importing.value = true
  clearTimeout(timer)
  try {
    if (inFlight && !(await inFlight)) return
    if (disposed || current !== editorEpoch || !article.value) return
    const draft = await tutorialAPI.importPackage(file, article.value)
    if (disposed || current !== editorEpoch) return
    article.value = draft
    Object.assign(form, pickContent(draft))
    saved.value = snapshot()
    savedAt.value = new Date(draft.updated_at).toLocaleTimeString(locale.value, {
      hour: '2-digit',
      minute: '2-digit',
    })
    history.value = [form.content]
    historyIndex.value = 0
    failedUpload.value = null
    error.value = ''
    mode.value = 'preview'
    app.showSuccess(t('tutorials.importSuccess'))
    try {
      const updatedCategories = await tutorialAPI.categories(true)
      if (!disposed && current === editorEpoch) categories.value = updatedCategories
    } catch {
      if (!disposed && current === editorEpoch) packageError.value = t('tutorials.categoryRefreshFailed')
    }
  } catch (e) {
    if (disposed || current !== editorEpoch) return
    conflict.value = (e as { status?: number }).status === 409
    if (conflict.value) error.value = t('tutorials.conflict')
    else packageError.value = tutorialPackageError(t, e, t('tutorials.importFailed'))
  } finally {
    importing.value = false
    if (!disposed && dirty.value && !error.value && !conflict.value)
      timer = setTimeout(() => void save(), 1600)
  }
}
async function exportPackage() {
  if (!article.value || packageBusy.value || publishing.value || uploads.value || conflict.value) return
  const current = editorEpoch
  exporting.value = true
  packageError.value = ''
  try {
    if (!(await save()) || disposed || current !== editorEpoch || !article.value) return
    const id = article.value.id
    const zip = await tutorialAPI.exportPackage(id)
    if (!disposed && current === editorEpoch) saveAs(zip, `tutorial-${id}.zip`)
  } catch (e) {
    if (!disposed && current === editorEpoch)
      packageError.value = tutorialPackageError(t, e, t('tutorials.exportFailed'))
  } finally {
    exporting.value = false
  }
}
async function publish() {
  if (publishing.value || packageBusy.value || uploads.value || failedUpload.value || conflict.value)
    return
  if (!form.title.trim() || !form.content.trim()) {
    error.value = t('tutorials.required')
    return
  }
  publishing.value = true
  const current = editorEpoch
  try {
    if (!(await save()) || !article.value) return
    // Capture a stable saved version; subsequent typing stays in the editor for the next draft.
    const result = await tutorialAPI.action(
      article.value.id,
      article.value.version,
      'publish',
    )
    if (disposed || current !== editorEpoch) return
    article.value = result
    error.value = ''
    app.showSuccess(t('tutorials.publishSuccess'))
  } catch (e) {
    conflict.value = (e as { status?: number }).status === 409
    error.value = conflict.value
      ? t('tutorials.conflict')
      : (e as Error).message
  } finally {
    publishing.value = false
    if (dirty.value && !error.value) timer = setTimeout(() => void save(), 1600)
  }
}
async function upload(file: File) {
  if (uploads.value || importing.value || conflict.value) return
  if (
    !['image/png', 'image/jpeg', 'image/gif'].includes(file.type) ||
    file.size > 5 * 1024 * 1024
  ) {
    error.value = t('tutorials.invalidImage')
    return
  }
  const current = editorEpoch
  uploads.value++
  uploadProgress.value = 0
  failedUpload.value = null
  try {
    if (!(await save()) || !article.value || current !== editorEpoch) {
      if (current === editorEpoch) failedUpload.value = file
      return
    }
    const result = await tutorialAPI.upload(
      article.value.id,
      file,
      (percent) => {
        uploadProgress.value = percent
      },
    )
    if (!disposed && current === editorEpoch)
      insert(`\n![${t('tutorials.imageAlt')}](${result.reference})\n`)
  } catch {
    if (current === editorEpoch) failedUpload.value = file
  } finally {
    uploads.value--
  }
}
function fileSelected(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (file) void upload(file)
}
function paste(e: ClipboardEvent) {
  const files = Array.from(e.clipboardData?.files || [])
  const file = files.find((f) => f.type.startsWith('image/'))
  if (file) {
    e.preventDefault()
    void upload(file)
  }
}
function retryUpload() {
  if (failedUpload.value) void upload(failedUpload.value)
}
async function reloadArticle() {
  if (dirty.value && !window.confirm(t('tutorials.leaveConfirm'))) return
  await load()
}
function applyTemplate(e: Event) {
  const key = (e.target as HTMLSelectElement).value
  const zh = locale.value === 'zh'
  const templates: Record<string, string> = zh
    ? {
        quickStart:
          '## 准备工作\n\n说明开始前需要准备的账号、工具和权限。\n\n## 操作步骤\n\n1. 登录账号。\n2. 按步骤完成配置。\n3. 发起一次测试。\n\n## 验证结果\n\n说明成功后的表现，以及如何检查结果。\n\n## 常见问题\n\n### 操作失败时怎么办？\n\n补充排查方法。\n',
        clientSetup:
          '## 适用客户端\n\n填写客户端名称和适用版本。\n\n## 获取连接信息\n\n说明如何获取 API 地址和密钥。不要在教程中填写真实密钥。\n\n## 配置步骤\n\n1. 打开客户端设置。\n2. 填写 API 地址并选择模型。\n3. 输入自己的密钥并保存。\n\n## 测试连接\n\n说明如何验证，以及常见报错的处理方法。\n',
        faq: '## 账号与登录\n\n### 如何登录？\n\n在这里填写解答。\n\n## 调用与配置\n\n### 调用失败怎么办？\n\n在这里填写排查步骤。\n\n## 获取帮助\n\n说明如何提交工单，以及需要提供哪些非敏感信息。\n',
      }
    : {
        quickStart:
          '## Before you begin\n\nDescribe the required account, tools and permissions.\n\n## Steps\n\n1. Sign in.\n2. Complete the configuration.\n3. Send a test request.\n\n## Verify the result\n\nExplain what success looks like.\n\n## Troubleshooting\n\nAdd common problems and solutions.\n',
        clientSetup:
          '## Supported client\n\nSpecify the client and version.\n\n## Connection details\n\nExplain where to find the API URL and key. Never include a real key.\n\n## Configure the client\n\n1. Open settings.\n2. Enter the API URL and select a model.\n3. Enter your own key and save.\n\n## Test the connection\n\nExplain how to verify the configuration.\n',
        faq: '## Account and sign-in\n\n### How do I sign in?\n\nWrite the answer here.\n\n## Requests and configuration\n\n### What if a request fails?\n\nWrite troubleshooting steps here.\n\n## Get help\n\nExplain how to submit a support ticket.\n',
      }
  if (
    templates[key] &&
    (!form.content || window.confirm(t('tutorials.templateConfirm')))
  ) {
    form.content = templates[key]!
    if (!form.title) form.title = t(`tutorials.${key}`)
    recordHistory()
  }
}
function beforeUnload(e: BeforeUnloadEvent) {
  if (dirty.value || uploads.value || importing.value) {
    e.preventDefault()
    e.returnValue = ''
  }
}
function adaptEditor() {
  if (matchMedia('(max-width: 1023px)').matches && mode.value === 'split')
    mode.value = 'edit'
}
onBeforeRouteLeave(
  () =>
    internalNavigation ||
    (!dirty.value && !uploads.value && !importing.value) ||
    window.confirm(t('tutorials.leaveConfirm')),
)
onBeforeRouteUpdate(
  (to, from) =>
    to.params.id === from.params.id ||
    internalNavigation ||
    (!dirty.value && !uploads.value && !importing.value) ||
    window.confirm(t('tutorials.leaveConfirm')),
)
watch(
  () => route.params.id,
  (id) => {
    if (Number(id) !== article.value?.id && !internalNavigation) void load()
  },
)
watch(
  form,
  () => {
    clearTimeout(timer)
    if (!loading.value && !conflict.value && !publishing.value && !importing.value && dirty.value)
      timer = setTimeout(() => void save(), 1600)
  },
  { deep: true },
)
onMounted(() => {
  void load()
  window.addEventListener('beforeunload', beforeUnload)
  window.addEventListener('resize', adaptEditor)
})
onBeforeUnmount(() => {
  disposed = true
  clearTimeout(timer)
  window.removeEventListener('beforeunload', beforeUnload)
  window.removeEventListener('resize', adaptEditor)
})
</script>

<style scoped>
.tutorial-editor-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
}
.mode-preview {
  grid-template-columns: minmax(0, 1fr);
}
.mode-preview .tutorial-editor-source {
  display: none;
}
.mode-edit {
  grid-template-columns: minmax(0, 1fr);
}
.mode-edit .tutorial-editor-preview {
  display: none;
}
@media (max-width: 1023px) {
  .mode-split {
    grid-template-columns: minmax(0, 1fr);
  }
  .mode-split .tutorial-editor-preview {
    display: none;
  }
}
</style>
