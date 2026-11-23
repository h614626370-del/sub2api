import { apiClient } from '@/api/client'

export interface HomepageConfig {
  enabled: boolean
  public_origin: string
  html: string
  entry_file?: string
}

export interface HomepageState extends HomepageConfig {
  assets?: HomepageAsset[]
  revision: number
  has_previous: boolean
  backend_only: boolean
  warnings: string[]
}

export interface HomepageAsset {
  name: string
  content_type: string
  size: number
  digest: string
}

interface HomepageAssetInput {
  name: string
  data?: string
}

const endpoint = '/admin/homepage'

export const homepageAPI = {
  async get() {
    return (await apiClient.get<HomepageState>(endpoint)).data
  },
  async save(config: HomepageConfig & { assets?: HomepageAssetInput[] }, revision: number) {
    return (await apiClient.put<HomepageState>(endpoint, { ...config, revision })).data
  },
  async restore(revision: number) {
    return (await apiClient.post<HomepageState>(`${endpoint}/restore`, { revision })).data
  },
}
