import { request } from './client'

function queryPath(path, params) {
  const query = new URLSearchParams(Object.entries(params).filter(([, value]) => value !== '' && value != null))
  return `${path}?${query}`
}

export const adminApi = {
  health: (signal) => request('/healthz', { signal }),
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
  maintenance: (signal) => request('/api/maintenance', { signal }),
  sunPanelRoutes: (signal) => request('/api/sunpanel/routes', { signal }),
  mcp: (signal) => request('/api/mcp', { signal }),
  mcpUsers: (signal) => request('/api/mcp/sunpanel-users', { signal }),
  saveMCP: (body) => request('/api/mcp', { method: 'PUT', body }),
  rotateMCPToken: (revision) => request('/api/mcp/rotate-token', { method: 'POST', body: { revision } }),
  saveMCPClient: (body) => request('/api/mcp/clients', { method: 'POST', body }),
  dashboard: (signal) => request('/api/dashboard', { signal }),
  serviceHealth: (signal) => request('/api/service-health', { signal }),
  ddnsRecords: (refresh, signal) => request(`/api/ddns/records${refresh ? '?refresh=1' : ''}`, { signal }),
  ddnsNetwork: (input, force, signal) => request(force ? '/api/ddns/network' : queryPath('/api/ddns/network', input), { method: force ? 'POST' : 'GET', body: force ? input : undefined, signal }),
  syncDDNS: (id) => request(`/api/ddns/${encodeURIComponent(id)}/run`, { method: 'POST', body: {} }),
  runJobs: (kind) => request(`/api/jobs/${encodeURIComponent(kind)}`, { method: 'POST', body: {} }),
  logs: (params, signal) => request(queryPath('/api/logs', params), { signal }),
  statistics: (params, signal) => request(queryPath('/api/statistics', params), { signal }),
  traffic: (params, signal) => request(queryPath('/api/statistics/traffic', params), { signal }),
  refreshSubscription: (id) => request(`/api/subscriptions/${encodeURIComponent(id)}/refresh`, { method: 'POST', body: {} }),
  ipBlocks: (signal) => request('/api/ip-blocks', { signal }),
  freezeIP: (body) => request('/api/ip-blocks', { method: 'POST', body }),
  releaseIP: (body) => request('/api/ip-blocks', { method: 'DELETE', body }),
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
