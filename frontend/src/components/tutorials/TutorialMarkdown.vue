<template>
  <div>
    <div
      ref="body"
      class="tutorial-prose"
      v-html="rendered.html"
      @click="interact"
      @keydown.enter="interact"
    />
    <p
      v-if="assetError"
      class="mt-4 text-sm text-red-600 dark:text-red-400"
      role="alert"
    >
      {{ t('tutorials.imageLoadFailed') }}
      <button type="button" class="underline" @click="loadAssets">
        {{ t('tutorials.retry') }}
      </button>
    </p>
    <dialog
      ref="dialog"
      class="tutorial-lightbox"
      @click="closeImage"
      @close="zoom = ''"
    >
      <button type="button" class="btn btn-secondary" @click="closeImage">
        {{ t('common.close') }}
      </button>
      <img v-if="zoom" :src="zoom" :alt="zoomAlt" @click.stop />
    </dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { tutorialAPI } from '@/api/tutorials'
import { renderTutorial } from '@/utils/tutorial-markdown'
import '@/styles/tutorials.css'

const props = withDefaults(
  defineProps<{ content: string; articleId?: number; admin?: boolean }>(),
  { admin: false },
)
const emit = defineEmits<{
  headings: [value: ReturnType<typeof renderTutorial>['headings']]
}>()
const { t } = useI18n()
const body = ref<HTMLElement>()
const dialog = ref<HTMLDialogElement>()
const zoom = ref(''),
  zoomAlt = ref(''),
  assetError = ref(false)
const rendered = computed(() =>
  renderTutorial(props.content, t('tutorials.copyCode')),
)
let generation = 0
let controller = new AbortController()
const urls = new Map<string, string>()
function clear() {
  generation++
  controller.abort()
  controller = new AbortController()
  urls.forEach((url) => URL.revokeObjectURL(url))
  urls.clear()
  closeImage()
}
async function loadAssets() {
  const current = ++generation
  const id = props.articleId
  assetError.value = false
  await nextTick()
  if (current !== generation || !id) return
  const elements = [
    ...(body.value?.querySelectorAll<HTMLImageElement>('img[data-asset]') ||
      []),
  ]
  // Sequential requests keep large screenshot-heavy articles from flooding the panel limiter.
  for (const img of elements) {
    const key = img.dataset.asset!
    try {
      let url = urls.get(key)
      if (!url) {
        const blob = await tutorialAPI.asset(
          id,
          key,
          props.admin,
          controller.signal,
        )
        if (current !== generation) return
        url = URL.createObjectURL(blob)
        urls.set(key, url)
      }
      if (current !== generation) return
      img.src = url
      img.setAttribute('aria-label', t('tutorials.enlargeImage'))
    } catch {
      if (current === generation && !controller.signal.aborted)
        assetError.value = true
    }
  }
}
function closeImage() {
  dialog.value?.close()
  zoom.value = ''
}
async function interact(event: Event) {
  const target = event.target as HTMLElement
  const button = target.closest<HTMLButtonElement>('button[data-copy]')
  if (button) {
    try {
      await navigator.clipboard.writeText(
        button.closest('pre')?.querySelector('code')?.textContent || '',
      )
      button.textContent = t('tutorials.copied')
    } catch {
      button.textContent = t('tutorials.copyFailed')
    }
  }
  if (
    target instanceof HTMLImageElement &&
    target.dataset.asset &&
    target.src
  ) {
    zoom.value = target.src
    zoomAlt.value = target.alt
    await nextTick()
    dialog.value?.showModal()
  }
}
watch(() => [props.articleId, props.admin], clear, { flush: 'sync' })
watch(
  rendered,
  (value) => {
    emit('headings', value.headings)
    void loadAssets()
  },
  { immediate: true, flush: 'post' },
)
watch(
  () => [props.articleId, props.admin],
  () => void loadAssets(),
  { flush: 'post' },
)
onBeforeUnmount(clear)
</script>
