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

export async function request(path, { method = 'GET', body, signal, notifyUnauthorized = true } = {}) {
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

  const json = response.headers.get('Content-Type')?.includes('application/json')
  const data = json ? await response.json().catch(() => null) : null
  if (!response.ok) {
    const message = data?.error || `管理接口请求失败（HTTP ${response.status}）`
    if (response.status === 401 && notifyUnauthorized && generation === sessionGeneration) {
      unauthorizedHandler?.(message)
    }
    throw new ApiError(message, response.status)
  }
  if (response.status === 204) return null
  if (data === null) throw new ApiError('管理接口返回了无效响应，请检查 API 转发配置', response.status)
  return data
}
