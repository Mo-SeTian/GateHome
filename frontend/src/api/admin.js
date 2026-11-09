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
  dashboard: (signal) => request('/api/dashboard', { signal }),
  serviceHealth: (signal) => request('/api/service-health', { signal })
}
