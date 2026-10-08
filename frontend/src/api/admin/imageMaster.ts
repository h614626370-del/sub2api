import apiClient from '../client'

export interface ImageMasterConfig {
  enabled: boolean
  timeout_seconds: number
  heartbeat_seconds: number
  max_body_mib: number
  max_response_mib: number
  done_sentinel: boolean
  raw_request_logging: boolean
}

export interface ImageMasterRecord {
  id: string
  started_at: number
  finished_at?: number
  outcome: string
  route: string
  requested_model: string
  model: string
  user_id: number
  api_key_id: number
  source_images: number
  image_count: number
  stream: boolean
  heartbeats: number
  duration_ms: number
  gateway_status?: number
  gateway_request_id?: string
  error_code?: string
  raw_saved: boolean
  stages: { name: string; at: number }[]
}

export interface ImageMasterStatus {
  config: ImageMasterConfig
  active: number
  storage_error: boolean
  items: ImageMasterRecord[]
}

const root = '/admin/image-master'
export const imageMasterAPI = {
  async status() { return (await apiClient.get<ImageMasterStatus>(root)).data },
  async save(config: ImageMasterConfig) { return (await apiClient.put<ImageMasterConfig>(`${root}/settings`, config)).data },
  async cancel(id: string) { await apiClient.post(`${root}/requests/${encodeURIComponent(id)}/cancel`) },
  async remove(id?: string) { await apiClient.delete(`${root}/requests${id ? `/${encodeURIComponent(id)}` : ''}`) },
  async raw(id: string) { return (await apiClient.get<unknown>(`${root}/requests/${encodeURIComponent(id)}/raw`)).data },
  async export() { return (await apiClient.get<Blob>(`${root}/export`, { responseType: 'blob' })).data }
}
