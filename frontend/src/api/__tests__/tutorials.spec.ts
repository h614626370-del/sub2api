import { beforeEach, describe, expect, it, vi } from 'vitest'
import { tutorialAPI, emptyTutorial } from '../tutorials'

const state = vi.hoisted(() => ({ isAuthenticated: false }))
const get = vi.hoisted(() => vi.fn())
const post = vi.hoisted(() => vi.fn())
vi.mock('@/stores/auth', () => ({ useAuthStore: () => state }))
vi.mock('../client', () => ({ apiClient: { get, post } }))

describe('tutorial access', () => {
  beforeEach(() => {
    state.isAuthenticated = false
    get.mockReset().mockResolvedValue({ data: {} })
    post.mockReset().mockResolvedValue({ data: { id: 42 } })
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
  it('uploads a ZIP to the admin import route and versions replacements', async () => {
    const zip = new File(['data'], 'guide.zip', { type: 'application/zip' })
    await tutorialAPI.importPackage(zip)
    await tutorialAPI.importPackage(zip, { id: 42, version: 3 })
    expect(post.mock.calls[0][0]).toBe('/admin/tutorials/import')
    expect(post.mock.calls[0][1].get('file')).toBe(zip)
    expect(post.mock.calls[1][0]).toBe('/admin/tutorials/42/import')
    expect(post.mock.calls[1][2].params).toEqual({ version: 3 })
  })
  it('downloads ZIP bytes and retains structured errors from the download route', async () => {
    get.mockResolvedValue({ data: new Uint8Array([80, 75, 3, 4]).buffer })
    const zip = await tutorialAPI.exportPackage(42)
    expect(zip.type).toBe('application/zip')
    expect(zip.size).toBe(4)
    expect(get.mock.calls[0][0]).toBe('/admin/tutorials/42/export')
    const config = get.mock.calls[0][1]
    const message = { reason: 'TUTORIAL_PACKAGE_IMAGE_MISSING', metadata: { file: 'image.png' } }
    expect(config.transformResponse[0](new TextEncoder().encode(JSON.stringify(message)), { 'content-type': 'application/json; charset=utf-8' })).toEqual(message)
  })
})
