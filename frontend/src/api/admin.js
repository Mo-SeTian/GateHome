import { request } from './client'

export const adminApi = {
  account: () => request('/api/account', { notifyUnauthorized: false }),
  login: (username, password) => request('/api/login', {
    method: 'POST', body: { username, password }, notifyUnauthorized: false
  }),
  logout: () => request('/api/logout', { method: 'POST', body: {}, notifyUnauthorized: false }),
  updateAccount: (body) => request('/api/account', { method: 'PUT', body }),
  status: (signal) => request('/api/status', { signal }),
  config: (signal) => request('/api/config', { signal }),
  saveConfig: (body) => request('/api/config', { method: 'PUT', body }),
  restart: () => request('/api/maintenance/restart', { method: 'POST', body: {} }),
  dashboard: (signal) => request('/api/dashboard', { signal }),
  serviceHealth: (signal) => request('/api/service-health', { signal }),
  uploadRouteImage: (file) => {
    const body = new FormData()
    body.append('file', file)
    return request('/api/route-images/upload', { method: 'POST', body })
  },
  importRouteImage: (body) => request('/api/route-images/import', { method: 'POST', body }),
  discoverServices: (body) => request('/api/service-discovery', { method: 'POST', body }),
  discoveryStatus: (id, signal) => request(`/api/service-discovery/${encodeURIComponent(id)}`, { signal }),
  cancelDiscovery: (id) => request(`/api/service-discovery/${encodeURIComponent(id)}/cancel`, { method: 'POST', body: {} })
}
