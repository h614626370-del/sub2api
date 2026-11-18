import { apiClient } from '@/api/client'

export interface HomepageConfig {
  enabled: boolean
  public_origin: string
  html: string
}

export interface HomepageState extends HomepageConfig {
  revision: number
  has_previous: boolean
  backend_only: boolean
  warnings: string[]
}

const endpoint = '/admin/homepage'

export const homepageAPI = {
  async get() {
    return (await apiClient.get<HomepageState>(endpoint)).data
  },
  async save(config: HomepageConfig, revision: number) {
    return (await apiClient.put<HomepageState>(endpoint, { ...config, revision })).data
  },
  async restore(revision: number) {
    return (await apiClient.post<HomepageState>(`${endpoint}/restore`, { revision })).data
  },
}
