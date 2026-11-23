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
  exposure: signal => request('/api/exposure', { signal }),
  sessions: signal => request('/api/account/sessions', { signal }),
  revokeSession: (id, signal) => request('/api/account/sessions/' + encodeURIComponent(id), { method: 'DELETE', body: {}, signal }),
  verifyAdmin: (password, signal) => request('/api/account/verify', { method: 'POST', body: { password }, signal }),
  updateAccount: (body) => request('/api/account', { method: 'PUT', body }),
  status: (signal) => request('/api/status', { signal }),
  config: (signal) => request('/api/config', { signal }),
  saveConfig: (body) => request('/api/config', { method: 'PUT', body }),
  revealCredential: (body, signal) => request('/api/credentials/reveal', { method: 'POST', body, signal }),
  restart: () => request('/api/maintenance/restart', { method: 'POST', body: {} }),
  maintenance: (signal) => request('/api/maintenance', { signal }),
  pendingSunPanelAdmins: (signal) => request('/api/sunpanel/admin-setup', { signal }),
  initializeSunPanelAdmin: (id, password, signal) => request('/api/sunpanel/admin-setup', {
    method: 'POST', body: { id, password }, signal
  }),
  sunPanelRoutes: (signal) => request('/api/sunpanel/routes', { signal }),
  connectSunPanel: (origin, signal) => request('/api/sunpanel/connect', { method: 'POST', body: { origin }, signal }),
  mcp: (signal) => request('/api/mcp', { signal }),
  mcpUsers: (signal) => request('/api/mcp/sunpanel-users', { signal }),
  saveMCP: (body) => request('/api/mcp', { method: 'PUT', body }),
  rotateMCPToken: (revision) => request('/api/mcp/rotate-token', { method: 'POST', body: { revision } }),
  saveMCPClient: (body) => request('/api/mcp/clients', { method: 'POST', body }),
  dashboard: (signal) => request('/api/dashboard', { signal }),
  runtime: signal => request('/api/runtime', { signal }),
  routeHistory: (id, signal) => request('/api/service-health/' + encodeURIComponent(id) + '/history', { signal }),
  notificationHistory: signal => request('/api/notifications/history', { signal }),
  wxPusher: signal => request('/api/notifications/wxpusher', { signal }),
  saveWxPusher: (body, signal) => request('/api/notifications/wxpusher', { method: 'PUT', body, signal }),
  testWxPusher: signal => request('/api/notifications/wxpusher/test', { method: 'POST', body: {}, signal }),
  webhook: signal => request('/api/notifications/webhook', { signal }),
  saveWebhook: (body, signal) => request('/api/notifications/webhook', { method: 'PUT', body, signal }),
  testWebhook: signal => request('/api/notifications/webhook/test', { method: 'POST', body: {}, signal }),
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
  cancelDiscovery: (id) => request(`/api/service-discovery/${encodeURIComponent(id)}/cancel`, { method: 'POST', body: {} }),

  // Mihomo (代理分流)
  mihomoStatus: (signal) => request('/api/mihomo/status', { signal }),
  saveMihomoSettings: (body) => request('/api/mihomo/settings', { method: 'PUT', body }),
  startMihomo: () => request('/api/mihomo/start', { method: 'POST', body: {} }),
  stopMihomo: () => request('/api/mihomo/stop', { method: 'POST', body: {} }),
  restartMihomo: () => request('/api/mihomo/restart', { method: 'POST', body: {} }),
  reloadMihomo: () => request('/api/mihomo/reload', { method: 'POST', body: {} }),
  setMihomoMode: (mode) => request('/api/mihomo/mode', { method: 'PATCH', body: { mode } }),
  mihomoGroups: (signal) => request('/api/mihomo/groups', { signal }),
  saveMihomoGroup: (body) => request('/api/mihomo/groups', { method: 'POST', body }),
  deleteMihomoGroup: (id) => request(`/api/mihomo/groups/${encodeURIComponent(id)}`, { method: 'DELETE', body: {} }),
  selectMihomoProxy: (group, node) => request('/api/mihomo/select', { method: 'PUT', body: { group, node } }),
  testMihomoGroupDelay: (group, body = {}) => request('/api/mihomo/delay', { method: 'POST', body: { group, ...body } }),
  testMihomoNodeDelay: (node, body = {}) => request('/api/mihomo/node-delay', { method: 'POST', body: { node, ...body } }),
  addMihomoRule: (body) => request('/api/mihomo/rules', { method: 'POST', body }),
  saveMihomoRules: (rules) => request('/api/mihomo/rules', { method: 'PUT', body: { rules } }),
  deleteMihomoRule: (id) => request(`/api/mihomo/rules/${encodeURIComponent(id)}`, { method: 'DELETE', body: {} }),
  addMihomoDNSPolicy: (body) => request('/api/mihomo/dns-policies', { method: 'POST', body }),
  saveMihomoDNSPolicies: (policies) => request('/api/mihomo/dns-policies', { method: 'PUT', body: { policies } }),
  deleteMihomoDNSPolicy: (id) => request(`/api/mihomo/dns-policies/${encodeURIComponent(id)}`, { method: 'DELETE', body: {} }),
  addMihomoSubscription: (body) => request('/api/mihomo/subscriptions', { method: 'POST', body }),
  updateMihomoSubscription: (id, body) => request(`/api/mihomo/subscriptions/${encodeURIComponent(id)}`, { method: 'PUT', body }),
  activateMihomoSubscription: (id) => request(`/api/mihomo/subscriptions/${encodeURIComponent(id)}/activate`, { method: 'PUT', body: {} }),
  refreshMihomoSubscription: (id) => request(`/api/mihomo/subscriptions/${encodeURIComponent(id)}/refresh`, { method: 'POST', body: {} }),
  deleteMihomoSubscription: (id) => request(`/api/mihomo/subscriptions/${encodeURIComponent(id)}`, { method: 'DELETE', body: {} }),
  mihomoYaml: (kind = 'runtime', signal) => request(queryPath('/api/mihomo/config/yaml', { kind }), { signal }),
  validateMihomoYaml: (content) => request('/api/mihomo/config/validate', { method: 'POST', body: { content } }),
  saveMihomoYaml: (content) => request('/api/mihomo/config/yaml', { method: 'PUT', body: { content } }),
  resyncMihomoYaml: () => request('/api/mihomo/config/resync', { method: 'POST', body: {} }),
  downloadMihomoCore: () => request('/api/mihomo/core/download', { method: 'POST', body: {} }),
  downloadMihomoZashboard: () => request('/api/mihomo/zashboard/download', { method: 'POST', body: {} }),
  downloadMihomoMMDB: () => request('/api/mihomo/geodata/download', { method: 'POST', body: {} }),
  uploadMihomoMMDB: (file) => {
    const body = new FormData()
    body.append('file', file)
    return request('/api/mihomo/geodata/upload', { method: 'POST', body })
  },
  mihomoLogs: (signal) => request('/api/mihomo/logs', { signal }),
  mihomoCustomYaml: (kind, signal) => request(`/api/mihomo/custom/${encodeURIComponent(kind)}/yaml`, { signal }),
  saveMihomoCustomYaml: (kind, content, validateOnly = false) => request(`/api/mihomo/custom/${encodeURIComponent(kind)}/yaml`, {
    method: 'PUT', body: { content, validate_only: validateOnly }
  }),
  clearMihomoLogs: () => request('/api/mihomo/logs', { method: 'DELETE', body: {} }),
  uploadMihomoCore: (file) => {
    const body = new FormData()
    body.append('file', file)
    return request('/api/mihomo/core/upload', { method: 'POST', body })
  }
}
