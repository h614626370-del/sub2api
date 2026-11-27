import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import TutorialReaderLayout from '../TutorialReaderLayout.vue'

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ isAuthenticated: false }),
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({ siteName: 'Sub2API', publicSettingsLoaded: true }),
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

vi.mock('@/components/layout/AppLayout.vue', () => ({ default: {} }))

describe('TutorialReaderLayout', () => {
  it.each(['/tutorials', '/tutorials/first-guide'])(
    'lets the browser request the public homepage from %s',
    async (path) => {
      const page = { template: '<div />' }
      const router = createRouter({
        history: createMemoryHistory(),
        routes: [
          { path: '/', redirect: '/home' },
          { path: '/home', component: page },
          { path: '/tutorials/:id?', component: page },
          { path: '/login', component: page },
        ],
      })
      await router.push(path)
      const wrapper = mount(TutorialReaderLayout, { global: { plugins: [router] } })
      try {
        const logo = wrapper.get('header img').element.closest('a')!
        expect(logo.getAttribute('href')).toBe('/')

        let intercepted: boolean | undefined
        wrapper.element.addEventListener('click', (event) => {
          intercepted = event.defaultPrevented
          event.preventDefault()
        }, { once: true })
        logo.click()
        expect(intercepted).toBe(false)
      } finally {
        wrapper.unmount()
      }
    },
  )
})
