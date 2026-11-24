import { apiClient } from './client'
import { useAuthStore } from '@/stores/auth'
const readBase = () =>
  useAuthStore().isAuthenticated ? '/tutorials' : '/public/tutorials'
export interface TutorialContent {
  slug?: string
  visibility: 'login' | 'public'
  title: string
  summary: string
  content: string
  category_id: number | null
  pinned: boolean
  sort_order: number
}
export interface Tutorial extends TutorialContent {
  previous_id?: number
  previous_slug?: string
  next_id?: number
  next_slug?: string
  route_slug?: string
  id: number
  version: number
  published: boolean
  has_changes: boolean
  updated_at: string
  published_at: string | null
}
export interface TutorialCategory {
  id: number
  name: string
  sort_order: number
}
export interface TutorialPage {
  items: Tutorial[]
  total: number
  pages: number
  page: number
}
export const maxTutorialPackageBytes = 52 * 1024 * 1024
export interface TutorialPackageError {
  status?: number
  reason?: string
  message?: string
  metadata?: { file?: string }
}
export const emptyTutorial = (): TutorialContent => ({
  slug: '',
  visibility: 'login',
  title: '',
  summary: '',
  content: '',
  category_id: null,
  pinned: false,
  sort_order: 0,
})
export const tutorialPath = (id: number, slug?: string) => `/tutorials/${slug || id}`

export const tutorialAPI = {
  async list(
    admin: boolean,
    params: Record<string, unknown> = {},
    signal?: AbortSignal,
  ) {
    return (
      await apiClient.get<TutorialPage>(
        admin ? '/admin/tutorials' : readBase(),
        {
          params,
          signal,
        },
      )
    ).data
  },
  async get(id: number | string, admin = false) {
    return (
      await apiClient.get<Tutorial>(
        `${admin ? '/admin/tutorials' : readBase()}/${encodeURIComponent(id)}`,
      )
    ).data
  },
  async create(content: TutorialContent) {
    return (await apiClient.post<Tutorial>('/admin/tutorials', content)).data
  },
  async importPackage(file: File, target?: Pick<Tutorial, 'id' | 'version'>) {
    const form = new FormData()
    form.append('file', file)
    const path = target
      ? `/admin/tutorials/${target.id}/import`
      : '/admin/tutorials/import'
    return (
      await apiClient.post<Tutorial>(path, form, {
        params: target ? { version: target.version } : undefined,
        headers: { 'Content-Type': 'multipart/form-data' },
        timeout: 120_000,
      })
    ).data
  },
  async exportPackage(id: number) {
    const { data } = await apiClient.get<ArrayBuffer>(
      `/admin/tutorials/${id}/export`,
      {
        responseType: 'arraybuffer',
        timeout: 120_000,
        transformResponse: [(data, headers) => {
          if (String(headers['content-type']).includes('application/json')) {
            try {
              return JSON.parse(new TextDecoder().decode(data))
            } catch {
              return data
            }
          }
          return data
        }],
      },
    )
    return new Blob([data], { type: 'application/zip' })
  },
  async save(id: number, version: number, content: TutorialContent) {
    return (
      await apiClient.put<Tutorial>(`/admin/tutorials/${id}`, {
        ...content,
        version,
      })
    ).data
  },
  async action(
    id: number,
    version: number,
    action: 'publish' | 'unpublish' | 'delete',
  ) {
    const url = `/admin/tutorials/${id}`
    return action === 'delete'
      ? (await apiClient.delete<Tutorial>(url, { data: { version } })).data
      : (await apiClient.post<Tutorial>(`${url}/${action}`, { version })).data
  },
  async categories(admin = false) {
    return (
      await apiClient.get<TutorialCategory[]>(
        `${admin ? '/admin/tutorials' : readBase()}/categories`,
      )
    ).data
  },
  async saveCategory(category: Omit<TutorialCategory, 'id'> & { id?: number }) {
    return category.id
      ? (
          await apiClient.put<TutorialCategory>(
            `/admin/tutorials/categories/${category.id}`,
            category,
          )
        ).data
      : (
          await apiClient.post<TutorialCategory>(
            '/admin/tutorials/categories',
            category,
          )
        ).data
  },
  async deleteCategory(id: number) {
    await apiClient.delete(`/admin/tutorials/categories/${id}`)
  },
  async upload(id: number, file: File, onProgress?: (percent: number) => void) {
    const form = new FormData()
    form.append('file', file)
    return (
      await apiClient.post<{ id: string; reference: string }>(
        `/admin/tutorials/${id}/assets`,
        form,
        {
          headers: { 'Content-Type': 'multipart/form-data' },
          onUploadProgress: (event) =>
            onProgress?.(
              event.total ? Math.round((event.loaded / event.total) * 100) : 0,
            ),
        },
      )
    ).data
  },
  async asset(id: number, asset: string, admin: boolean, signal?: AbortSignal) {
    return (
      await apiClient.get<Blob>(
        `${admin ? '/admin/tutorials' : readBase()}/${id}/assets/${asset}`,
        { responseType: 'blob', signal },
      )
    ).data
  },
}
