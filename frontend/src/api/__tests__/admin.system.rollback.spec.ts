import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
}))

vi.mock('../client', () => ({
  apiClient: {
    get,
    post,
  },
}))

import {
  checkCustomUpdates,
  getRollbackVersions,
  rollback,
  type RollbackVersionInfo
} from '@/api/admin/system'

describe('admin system rollback API', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
  })

  it('getRollbackVersions fetches the rollback version list', async () => {
    const versions: RollbackVersionInfo[] = [
      {
        version: '0.1.146',
        published_at: '2026-07-07T00:00:00Z',
        html_url: 'https://github.com/h614626370-del/sub2api/releases/tag/v0.1.146'
      }
    ]
    get.mockResolvedValue({ data: { versions } })

    const result = await getRollbackVersions()

    expect(get).toHaveBeenCalledWith('/admin/system/rollback-versions')
    expect(result.versions).toEqual(versions)
  })

  it('checkCustomUpdates uses the customized repository endpoint', async () => {
    get.mockResolvedValue({
      data: {
        current_version: '0.2.7',
        latest_version: '0.2.8',
        has_update: true,
        cached: false,
        build_type: 'release'
      }
    })

    await checkCustomUpdates(true)

    expect(get).toHaveBeenCalledWith('/admin/system/check-custom-updates', {
      params: { force: 'true' }
    })
  })

  it('rollback posts the target version in the request body', async () => {
    post.mockResolvedValue({ data: { message: 'ok', need_restart: true } })

    const result = await rollback('0.1.146')

    expect(post).toHaveBeenCalledWith(
      '/admin/system/rollback',
      { version: '0.1.146' },
      { timeout: 15 * 60 * 1000 }
    )
    expect(result.need_restart).toBe(true)
  })

  it('rollback without a version posts no body (legacy backup rollback)', async () => {
    post.mockResolvedValue({ data: { message: 'ok', need_restart: true } })

    await rollback()

    expect(post).toHaveBeenCalledWith(
      '/admin/system/rollback',
      undefined,
      { timeout: 15 * 60 * 1000 }
    )
  })
})
