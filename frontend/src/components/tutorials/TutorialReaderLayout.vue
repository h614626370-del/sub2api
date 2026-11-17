<template>
  <AppLayout v-if="auth.isAuthenticated"><slot /></AppLayout>
  <div
    v-else
    class="min-h-screen bg-gray-50 text-gray-900 dark:bg-dark-950 dark:text-white"
  >
    <header
      class="border-b border-gray-200 bg-white dark:border-dark-800 dark:bg-dark-900"
    >
      <div
        class="mx-auto flex max-w-[1600px] items-center justify-between gap-4 px-4 py-4 sm:px-6 lg:px-8"
      >
        <RouterLink to="/home" class="flex min-w-0 items-center gap-3">
          <img
            :src="app.siteLogo || '/logo.svg'"
            alt=""
            class="h-9 w-9 rounded-lg object-contain"
          />
          <span class="truncate font-semibold">{{ app.siteName }}</span>
        </RouterLink>
        <nav
          class="flex shrink-0 items-center gap-3 text-sm"
          :aria-label="t('tutorials.guestNavigation')"
        >
          <RouterLink
            to="/tutorials"
            class="tutorial-focus text-primary-800 dark:text-primary-200"
            >{{ t('tutorials.title') }}</RouterLink
          >
          <RouterLink :to="loginRoute" class="btn btn-primary btn-sm">{{
            t('tutorials.signIn')
          }}</RouterLink>
        </nav>
      </div>
    </header>
    <main class="mx-auto max-w-[1600px] p-4 md:p-6 lg:p-8"><slot /></main>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores'
import AppLayout from '@/components/layout/AppLayout.vue'
const auth = useAuthStore(),
  app = useAppStore(),
  route = useRoute()
const { t } = useI18n()
const loginRoute = computed(() => ({
  path: '/login',
  query: { redirect: route.fullPath },
}))
onMounted(() => {
  if (!app.publicSettingsLoaded) void app.fetchPublicSettings().catch(() => {})
})
</script>
