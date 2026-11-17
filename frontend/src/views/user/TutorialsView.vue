<template>
  <TutorialReaderLayout>
    <div class="mx-auto max-w-[1500px]">
      <header class="mb-6 flex flex-wrap items-start justify-between gap-4">
        <div>
          <RouterLink
            v-if="articleId"
            to="/tutorials"
            class="tutorial-focus mb-3 inline-flex items-center gap-1 text-sm text-gray-600 hover:text-primary-700 dark:text-dark-300"
            ><Icon name="chevronLeft" size="sm" />{{
              t('tutorials.back')
            }}</RouterLink
          >
          <h1
            v-else
            class="text-2xl font-semibold text-gray-900 dark:text-white"
          >
            {{ t('tutorials.title') }}
          </h1>
          <p
            v-if="!articleId"
            class="mt-2 text-sm text-gray-600 dark:text-dark-300"
          >
            {{ t('tutorials.userDescription') }}
          </p>
        </div>
        <label class="w-full sm:w-80"
          ><span class="sr-only">{{ t('tutorials.search') }}</span
          ><input
            v-model="search"
            type="search"
            class="input"
            :placeholder="t('tutorials.search')"
            @keydown.enter="openSearch"
        /></label>
      </header>
      <div class="tutorial-reader-layout" :class="{ 'has-article': articleId }">
        <aside class="min-w-0">
          <button
            type="button"
            class="btn btn-secondary mb-3 w-full justify-between lg:hidden"
            :aria-expanded="navigationOpen"
            @click="navigationOpen = !navigationOpen"
          >
            {{ t('tutorials.articles') }}<Icon name="chevronDown" size="sm" />
          </button>
          <nav
            class="space-y-1 lg:sticky lg:top-6"
            :class="navigationOpen ? 'block' : 'hidden lg:block'"
            :aria-label="t('tutorials.categories')"
          >
            <button
              type="button"
              class="tutorial-category"
              :class="{ selected: category === '' }"
              @click="selectCategory('')"
            >
              {{ t('tutorials.all') }}
            </button>
            <button
              v-for="c in categories"
              :key="c.id"
              type="button"
              class="tutorial-category"
              :class="{ selected: category === c.id }"
              @click="selectCategory(c.id)"
            >
              {{ c.name }}
            </button>
            <div
              v-if="articleId"
              class="mt-5 border-t border-gray-200 pt-4 dark:border-dark-700"
            >
              <h2
                class="mb-2 px-3 text-xs font-medium text-gray-600 dark:text-dark-300"
              >
                {{ t('tutorials.articles') }}
              </h2>
              <RouterLink
                v-for="a in items"
                :key="a.id"
                :to="`/tutorials/${a.id}`"
                class="tutorial-category !text-sm"
                :class="{ selected: a.id === articleId }"
                @click="navigationOpen = false"
                >{{ a.title }}</RouterLink
              >
              <div
                v-if="pages > 1"
                class="mt-3 flex items-center justify-between gap-2"
              >
                <button
                  class="btn btn-secondary btn-sm"
                  :disabled="page <= 1 || loading"
                  @click="page--"
                >
                  {{ t('tutorials.previous') }}</button
                ><button
                  class="btn btn-secondary btn-sm"
                  :disabled="page >= pages || loading"
                  @click="page++"
                >
                  {{ t('tutorials.next') }}
                </button>
              </div>
            </div>
          </nav>
        </aside>
        <div class="min-w-0">
          <div
            v-if="error"
            class="mb-4 rounded-lg bg-red-50 p-4 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300"
            role="alert"
          >
            {{ error }}
            <button class="ml-2 underline" @click="reload">
              {{ t('tutorials.retry') }}
            </button>
          </div>
          <template v-if="articleId">
            <div
              v-if="articleLoading"
              class="h-[540px] animate-pulse rounded-xl bg-gray-100 motion-reduce:animate-none dark:bg-dark-800"
            />
            <div
              v-else-if="articleError"
              class="rounded-xl border border-gray-200 bg-white p-8 text-center dark:border-dark-700 dark:bg-dark-900"
            >
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{
                  t(
                    articleError === 'notFound'
                      ? 'tutorials.notFound'
                      : 'tutorials.loadFailed',
                  )
                }}
              </h2>
              <p
                v-if="articleError === 'notFound'"
                class="mt-2 text-sm text-gray-600 dark:text-dark-300"
              >
                {{
                  t(
                    auth.isAuthenticated
                      ? 'tutorials.notFoundHint'
                      : 'tutorials.guestNotFoundHint',
                  )
                }}
              </p>
              <RouterLink
                v-if="!auth.isAuthenticated && articleError === 'notFound'"
                :to="{ path: '/login', query: { redirect: route.fullPath } }"
                class="btn btn-primary mt-4"
                >{{ t('tutorials.signIn') }}</RouterLink
              >
              <button
                v-if="articleError !== 'notFound'"
                class="btn btn-secondary mt-4"
                @click="loadArticle"
              >
                {{ t('tutorials.retry') }}
              </button>
            </div>
            <article
              v-else-if="article"
              class="rounded-xl border border-gray-200 bg-white px-5 py-7 md:px-10 md:py-9 dark:border-dark-700 dark:bg-dark-900"
            >
              <div class="mx-auto max-w-[72ch]">
                <div
                  class="mb-4 flex flex-wrap items-center gap-3 text-xs text-gray-600 dark:text-dark-300"
                >
                  <span v-if="categoryLabel(article.category_id)">{{
                    categoryLabel(article.category_id)
                  }}</span
                  ><span
                    >{{ t('tutorials.updated') }}
                    {{
                      formatDate(article.published_at || article.updated_at)
                    }}</span
                  >
                </div>
                <h1
                  class="mb-4 break-words text-2xl font-semibold leading-snug text-gray-900 md:text-3xl dark:text-white"
                >
                  {{ article.title }}
                </h1>
                <p
                  v-if="article.summary"
                  class="mb-8 text-base leading-7 text-gray-600 dark:text-dark-300"
                >
                  {{ article.summary }}
                </p>
                <div
                  v-if="headings.length"
                  class="mb-7 border-y border-gray-200 py-3 xl:hidden dark:border-dark-700"
                >
                  <button
                    class="flex w-full items-center justify-between text-sm font-medium text-gray-700 dark:text-dark-200"
                    :aria-expanded="tocOpen"
                    @click="tocOpen = !tocOpen"
                  >
                    {{ t('tutorials.toc')
                    }}<Icon name="chevronDown" size="sm" />
                  </button>
                  <nav
                    v-if="tocOpen"
                    class="mt-3 space-y-2"
                    :aria-label="t('tutorials.toc')"
                  >
                    <a
                      v-for="h in headings"
                      :key="h.id"
                      :href="`#${h.id}`"
                      class="block text-sm text-gray-600 hover:text-primary-700 dark:text-dark-300"
                      @click.prevent="goHeading(h.id)"
                      >{{ h.text }}</a
                    >
                  </nav>
                </div>
                <TutorialMarkdown
                  :key="article.id"
                  :content="article.content"
                  :article-id="article.id"
                  @headings="headings = $event"
                />
                <footer
                  class="mt-12 flex items-center justify-between gap-4 border-t border-gray-200 pt-6 text-sm dark:border-dark-700"
                >
                  <RouterLink
                    v-if="article.previous_id"
                    :to="`/tutorials/${article.previous_id}`"
                    class="tutorial-focus text-primary-800 hover:underline dark:text-primary-200"
                    >← {{ t('tutorials.previous') }}</RouterLink
                  ><span v-else />
                  <RouterLink
                    v-if="article.next_id"
                    :to="`/tutorials/${article.next_id}`"
                    class="tutorial-focus text-primary-800 hover:underline dark:text-primary-200"
                    >{{ t('tutorials.next') }} →</RouterLink
                  >
                </footer>
              </div>
            </article>
          </template>
          <template v-else>
            <div v-if="loading" class="space-y-4" :aria-busy="true">
              <div
                v-for="i in 4"
                :key="i"
                class="h-28 animate-pulse rounded-lg bg-gray-100 motion-reduce:animate-none dark:bg-dark-800"
              />
            </div>
            <section
              v-else-if="!items.length"
              class="rounded-xl border border-gray-200 bg-white px-6 py-20 text-center dark:border-dark-700 dark:bg-dark-900"
            >
              <Icon
                name="book"
                size="xl"
                class="mx-auto mb-4 text-primary-700 dark:text-primary-300"
              />
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{
                  t(
                    search || category
                      ? 'tutorials.noResults'
                      : 'tutorials.userEmpty',
                  )
                }}
              </h2>
              <p class="mt-2 text-sm text-gray-600 dark:text-dark-300">
                {{
                  t(
                    search || category
                      ? 'tutorials.noResultsHint'
                      : 'tutorials.userEmptyHint',
                  )
                }}
              </p>
            </section>
            <section
              v-else
              class="overflow-hidden rounded-xl border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-900"
            >
              <RouterLink
                v-for="a in items"
                :key="a.id"
                :to="`/tutorials/${a.id}`"
                class="tutorial-focus group block border-b border-gray-100 px-5 py-6 last:border-b-0 hover:bg-gray-50 md:px-7 dark:border-dark-800 dark:hover:bg-dark-800/60"
              >
                <div
                  class="mb-2 flex items-center gap-3 text-xs text-gray-600 dark:text-dark-300"
                >
                  <span
                    v-if="a.pinned"
                    class="font-medium text-primary-800 dark:text-primary-200"
                    >{{ t('tutorials.pinned') }}</span
                  ><span>{{ categoryLabel(a.category_id) }}</span
                  ><span>{{ formatDate(a.updated_at) }}</span>
                </div>
                <div class="flex items-start justify-between gap-4">
                  <h2
                    class="break-words text-lg font-semibold text-gray-900 group-hover:text-primary-800 dark:text-white dark:group-hover:text-primary-200"
                  >
                    {{ a.title }}
                  </h2>
                  <Icon
                    name="chevronRight"
                    size="sm"
                    class="mt-1 shrink-0 text-gray-500 dark:text-dark-300"
                  />
                </div>
                <p
                  v-if="a.summary"
                  class="mt-2 line-clamp-2 text-sm leading-6 text-gray-600 dark:text-dark-300"
                >
                  {{ a.summary }}
                </p>
              </RouterLink>
            </section>
            <div
              v-if="total"
              class="mt-5 flex flex-wrap items-center justify-between gap-3 text-sm text-gray-600 dark:text-dark-300"
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
            </div>
          </template>
        </div>
        <aside v-if="articleId" class="hidden min-w-0 xl:block">
          <nav
            v-if="headings.length"
            class="sticky top-6 space-y-3"
            :aria-label="t('tutorials.toc')"
          >
            <h2
              class="mb-4 text-xs font-semibold text-gray-700 dark:text-dark-200"
            >
              {{ t('tutorials.toc') }}
            </h2>
            <a
              v-for="h in headings"
              :key="h.id"
              :href="`#${h.id}`"
              class="tutorial-focus block break-words text-sm leading-6 text-gray-600 hover:text-primary-700 dark:text-dark-300 dark:hover:text-primary-300"
              :style="{ paddingLeft: `${Math.max(0, h.level - 2) * 12}px` }"
              @click.prevent="goHeading(h.id)"
              >{{ h.text }}</a
            >
          </nav>
        </aside>
      </div>
    </div>
  </TutorialReaderLayout>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  tutorialAPI,
  type Tutorial,
  type TutorialCategory,
} from '@/api/tutorials'
import type { TutorialHeading } from '@/utils/tutorial-markdown'
import TutorialReaderLayout from '@/components/tutorials/TutorialReaderLayout.vue'
import { useAuthStore } from '@/stores/auth'
import Icon from '@/components/icons/Icon.vue'
import TutorialMarkdown from '@/components/tutorials/TutorialMarkdown.vue'
const { t, locale } = useI18n(),
  auth = useAuthStore(),
  route = useRoute(),
  router = useRouter()
const articleId = computed(() => Number(route.params.id) || 0)
const search = ref(String(route.query.search || '')),
  category = ref<number | ''>(''),
  page = ref(1),
  pages = ref(1),
  total = ref(0)
const categories = ref<TutorialCategory[]>([]),
  items = ref<Tutorial[]>([]),
  article = ref<Tutorial>(),
  headings = ref<TutorialHeading[]>([])
const loading = ref(true),
  articleLoading = ref(false),
  error = ref(''),
  articleError = ref(''),
  navigationOpen = ref(false),
  tocOpen = ref(false)
let timer: ReturnType<typeof setTimeout>,
  controller: AbortController | undefined,
  articleGeneration = 0
const categoryLabel = (id: number | null) =>
  categories.value.find((c) => c.id === id)?.name ||
  t('tutorials.uncategorized')
const formatDate = (date: string) =>
  new Date(date).toLocaleDateString(locale.value, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  })
async function loadList() {
  controller?.abort()
  const request = new AbortController()
  controller = request
  loading.value = true
  try {
    const result = await tutorialAPI.list(
      false,
      {
        search: search.value,
        category_id: category.value || undefined,
        page: page.value,
        page_size: 20,
      },
      request.signal,
    )
    if (request.signal.aborted) return
    items.value = result.items
    pages.value = Math.max(1, result.pages)
    total.value = result.total
    if (page.value > pages.value) page.value = pages.value
    error.value = ''
  } catch {
    if (!request.signal.aborted) error.value = t('tutorials.loadFailed')
  } finally {
    if (!request.signal.aborted) loading.value = false
  }
}
async function loadArticle() {
  const current = ++articleGeneration
  headings.value = []
  article.value = undefined
  articleError.value = ''
  tocOpen.value = false
  if (!articleId.value) return
  articleLoading.value = true
  try {
    const result = await tutorialAPI.get(articleId.value)
    if (current === articleGeneration) {
      article.value = result
      await nextTick()
      if (/^#tutorial-section-\d+$/.test(route.hash))
        document.getElementById(route.hash.slice(1))?.scrollIntoView()
    }
  } catch (e) {
    if (current === articleGeneration)
      articleError.value =
        (e as { status?: number }).status === 404 ? 'notFound' : 'error'
  } finally {
    if (current === articleGeneration) articleLoading.value = false
  }
}
async function reload() {
  let categoryError = false
  try {
    categories.value = await tutorialAPI.categories()
  } catch {
    categoryError = true
  }
  await Promise.all([loadList(), loadArticle()])
  if (categoryError) error.value = t('tutorials.loadFailed')
}
function selectCategory(id: number | '') {
  category.value = id
  navigationOpen.value = false
  if (articleId.value) void router.push('/tutorials')
}
function openSearch() {
  if (articleId.value)
    void router.push({
      path: '/tutorials',
      query: search.value ? { search: search.value } : {},
    })
}
function goHeading(id: string) {
  document.getElementById(id)?.scrollIntoView({
    behavior: matchMedia('(prefers-reduced-motion: reduce)').matches
      ? 'auto'
      : 'smooth',
  })
  history.replaceState(history.state, '', `#${id}`)
  tocOpen.value = false
}
watch([search, category], () => {
  clearTimeout(timer)
  timer = setTimeout(() => {
    if (page.value !== 1) page.value = 1
    else void loadList()
  }, 250)
})
watch(page, () => void loadList())
watch(articleId, () => {
  void loadArticle()
  window.scrollTo(0, 0)
})
watch(
  () => auth.isAuthenticated,
  () => void reload(),
)
onMounted(reload)
onBeforeUnmount(() => {
  controller?.abort()
  clearTimeout(timer)
  articleGeneration++
})
</script>

<style scoped>
.tutorial-reader-layout {
  display: grid;
  grid-template-columns: 200px minmax(0, 1fr);
  gap: 28px;
}
.tutorial-category {
  display: block;
  width: 100%;
  border-radius: 7px;
  padding: 10px 12px;
  text-align: left;
  font-size: 14px;
  color: #475569;
  overflow-wrap: anywhere;
}
.tutorial-category:hover {
  background: #f1f5f9;
}
.tutorial-category:focus-visible {
  outline: 2px solid #0d9488;
  outline-offset: 2px;
}
.tutorial-category.selected {
  background: #ccfbf1;
  color: #115e59;
  font-weight: 600;
}
:global(.dark .tutorial-category) {
  color: #cbd5e1;
}
:global(.dark .tutorial-category:hover) {
  background: #1e293b;
}
:global(.dark .tutorial-category.selected) {
  color: #99f6e4;
  background: #134e4a;
}
@media (min-width: 1280px) {
  .tutorial-reader-layout.has-article {
    grid-template-columns: 180px minmax(0, 1fr) 160px;
    gap: 24px;
  }
}
@media (max-width: 1023px) {
  .tutorial-reader-layout {
    grid-template-columns: minmax(0, 1fr);
    gap: 16px;
  }
}
</style>
