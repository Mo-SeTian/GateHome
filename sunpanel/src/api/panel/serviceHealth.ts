import request from '@/utils/request/axios'
import { useAuthStore } from '@/store'
import type { Response } from '@/utils/request'

export interface HealthResult {
  state: string
  label: string
  message: string
  healthy: boolean
  status?: number
  duration_ms: number
  checked_at: string
  tls_verified: boolean
  tls_expires_at?: string
}
export interface SiteHealth {
  id: number
  url: string
  lanUrl: string
  default: HealthResult
  lan: HealthResult
}
export function getSiteHealth(ids: number[], tlsOnly = false) {
  return request.post<Response<SiteHealth[]>>('/site/health', { ids, tlsOnly }, { headers: { token: useAuthStore().token }, timeout: 30000 }).then(response => response.data)
}
