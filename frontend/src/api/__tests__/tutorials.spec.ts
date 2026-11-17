import { beforeEach, describe, expect, it, vi } from 'vitest'
import { tutorialAPI, emptyTutorial } from '../tutorials'

const state = vi.hoisted(() => ({ isAuthenticated: false }))
const get = vi.hoisted(() => vi.fn())
vi.mock('@/stores/auth', () => ({ useAuthStore: () => state }))
vi.mock('../client', () => ({ apiClient: { get } }))

describe('tutorial access', () => {
  beforeEach(() => {
    state.isAuthenticated = false
    get.mockReset().mockResolvedValue({ data: {} })
  })
  it('uses anonymous endpoints for every guest reading surface', async () => {
    await tutorialAPI.list(false)
    await tutorialAPI.get(42)
    await tutorialAPI.categories()
    await tutorialAPI.asset(42, 'image', false)
    expect(get.mock.calls.map(([url]) => url)).toEqual([
      '/public/tutorials', '/public/tutorials/42', '/public/tutorials/categories', '/public/tutorials/42/assets/image',
    ])
  })
  it('uses authenticated endpoints for members after sign-in', async () => {
    state.isAuthenticated = true
    await tutorialAPI.list(false)
    await tutorialAPI.get(42)
    await tutorialAPI.categories()
    await tutorialAPI.asset(42, 'image', false)
    expect(get.mock.calls.map(([url]) => url)).toEqual([
      '/tutorials', '/tutorials/42', '/tutorials/categories', '/tutorials/42/assets/image',
    ])
  })
  it('keeps admin preview on protected endpoints', async () => {
    await tutorialAPI.get(42, true)
    await tutorialAPI.asset(42, 'image', true)
    expect(get.mock.calls.map(([url]) => url)).toEqual([
      '/admin/tutorials/42', '/admin/tutorials/42/assets/image',
    ])
  })
  it('creates drafts with login-only visibility', () => {
    expect(emptyTutorial().visibility).toBe('login')
  })
})
