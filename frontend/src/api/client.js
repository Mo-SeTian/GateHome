export class ApiError extends Error {
  constructor(message, status) {
    super(message)
    this.status = status
  }
}

let unauthorizedHandler
let sessionGeneration = 0

export function setUnauthorizedHandler(handler) {
  unauthorizedHandler = handler
}

export function markSessionChanged() {
  sessionGeneration++
}

function synchronizeVersion(version) {
  const loadedVersion = typeof __GATEHOME_VERSION__ === 'string' ? __GATEHOME_VERSION__ : ''
  if (import.meta.env?.DEV || !loadedVersion || version === loadedVersion ||
      !/^\d+\.\d+\.\d+(?:-beta\.\d+)?$/.test(version || '')) return

  // Keep a stale/cached document from reloading indefinitely, including concurrent responses.
  const key = 'gh_console_reload_version'
  const transition = `${loadedVersion}/${version}`
  if (window.sessionStorage.getItem(key) === transition)
    throw new ApiError('控制台资源与服务版本不一致，请稍后重新打开页面', 409)
  window.sessionStorage.setItem(key, transition)
  window.location.reload()
  throw new ApiError('GateHome 已更新，正在载入新界面', 409)
}

export async function request(path, { method = 'GET', body, signal, notifyUnauthorized = true, responseType = 'json' } = {}) {
  const generation = sessionGeneration
  const headers = { Accept: 'application/json' }
  const multipart = body instanceof FormData
  if (body !== undefined) {
    if (!multipart) headers['Content-Type'] = 'application/json'
    headers['X-Gatehouse-Request'] = '1'
  }

  let response
  try {
    response = await fetch(path, {
      method, headers, signal, credentials: 'same-origin',
      body: body === undefined || multipart ? body : JSON.stringify(body)
    })
  } catch (err) {
    if (err.name === 'AbortError') throw err
    throw new ApiError('无法连接 GateHome，请检查服务是否正在运行', 0)
  }

  synchronizeVersion(response.headers.get('X-GateHome-Version'))
  const json = response.headers.get('Content-Type')?.includes('application/json')
  const data = json ? await response.json().catch(() => null) : null
  if (response.ok && ['/healthz', '/api/status', '/api/maintenance'].includes(path))
    synchronizeVersion(data?.version)
  if (!response.ok) {
    const message = data?.error || `管理接口请求失败（HTTP ${response.status}）`
    if (response.status === 401 && notifyUnauthorized && generation === sessionGeneration) {
      unauthorizedHandler?.(message)
    }
    throw new ApiError(message, response.status)
  }
  if (response.status === 204) return null
  if (responseType === 'blob') return response.blob()
  if (data === null) throw new ApiError('管理接口返回了无效响应，请检查 API 转发配置', response.status)
  return data
}
