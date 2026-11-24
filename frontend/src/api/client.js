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

function safeSessionGet(key) {
  try {
    return window.sessionStorage.getItem(key)
  } catch {
    return null
  }
}

function safeSessionSet(key, value) {
  try {
    window.sessionStorage.setItem(key, value)
  } catch {
    // Ignore storage errors in restricted browsing modes.
  }
}

function synchronizeVersion(version) {
  const loadedVersion = typeof __GATEHOME_VERSION__ === 'string' ? __GATEHOME_VERSION__ : ''
  if (import.meta.env?.DEV || !loadedVersion || version === loadedVersion ||
      !/^\d+\.\d+\.\d+(?:-beta\.\d+)?$/.test(version || '')) return

  // Keep a stale/cached document from reloading indefinitely, including concurrent responses.
  const key = 'gh_console_reload_version'
  const transition = `${loadedVersion}/${version}`
  if (safeSessionGet(key) === transition)
    throw new ApiError('控制台资源与服务版本不一致，请稍后重新打开页面', 409)
  safeSessionSet(key, transition)
  window.location.reload()
  throw new ApiError('GateHome 已更新，正在载入新界面', 409)
}

export async function request(path, { method = 'GET', body, signal, notifyUnauthorized = true, responseType = 'json' } = {}) {
  const generation = sessionGeneration
  const headers = { Accept: 'application/json' }
  const multipart = body instanceof FormData
  const isSafeMethod = ['GET', 'HEAD'].includes(String(method).toUpperCase())
  if (body !== undefined || !isSafeMethod) {
    headers['X-Gatehouse-Request'] = '1'
  }
  if (body !== undefined && !multipart) {
    headers['Content-Type'] = 'application/json'
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

export async function requestStream(path, { method = 'POST', body, signal, notifyUnauthorized = true, onEvent } = {}) {
  const generation = sessionGeneration
  const headers = { Accept: 'text/event-stream, application/json' }
  const isSafeMethod = ['GET', 'HEAD'].includes(String(method).toUpperCase())
  if (body !== undefined || !isSafeMethod) {
    headers['X-Gatehouse-Request'] = '1'
  }
  if (body !== undefined) {
    headers['Content-Type'] = 'application/json'
  }

  let response
  try {
    response = await fetch(path, {
      method, headers, signal, credentials: 'same-origin',
      body: body === undefined ? undefined : JSON.stringify(body)
    })
  } catch (err) {
    if (err.name === 'AbortError') throw err
    throw new ApiError('无法连接 GateHome，请检查服务是否正在运行', 0)
  }

  synchronizeVersion(response.headers.get('X-GateHome-Version'))
  const contentType = response.headers.get('Content-Type') || ''
  if (!response.ok) {
    const data = contentType.includes('application/json') ? await response.json().catch(() => null) : null
    const message = data?.error || `管理接口请求失败（HTTP ${response.status}）`
    if (response.status === 401 && notifyUnauthorized && generation === sessionGeneration) {
      unauthorizedHandler?.(message)
    }
    throw new ApiError(message, response.status)
  }

  if (contentType.includes('application/json')) {
    const data = await response.json().catch(() => null)
    if (data === null) throw new ApiError('管理接口返回了无效响应，请检查 API 转发配置', response.status)
    onEvent?.('done', data)
    return data
  }

  if (!response.body) throw new ApiError('浏览器不支持流式读取响应', response.status)

  const reader = response.body.getReader()
  const decoder = new TextDecoder('utf-8')
  let buffer = ''
  let donePayload = null

  const dispatchBlock = block => {
    const cleaned = block.trim()
    if (!cleaned) return
    let eventName = 'message'
    const dataLines = []
    for (const line of block.split('\n')) {
      if (!line || line.startsWith(':')) continue
      if (line.startsWith('event:')) {
        eventName = line.slice(6).trim()
      } else if (line.startsWith('data:')) {
        const value = line.slice(5)
        dataLines.push(value.startsWith(' ') ? value.slice(1) : value)
      }
    }
    if (!dataLines.length) return
    let payload
    try {
      payload = JSON.parse(dataLines.join('\n'))
    } catch {
      return
    }
    if (eventName === 'error') {
      throw new ApiError(payload?.error || '模型流式回复失败', 502)
    }
    if (eventName === 'done') donePayload = payload
    onEvent?.(eventName, payload)
  }

  try {
    while (true) {
      const { value, done } = await reader.read()
      if (done) {
        buffer += decoder.decode()
        buffer = buffer.replace(/\r\n/g, '\n').replace(/\r/g, '\n')
        const blocks = buffer.split('\n\n')
        for (const block of blocks) dispatchBlock(block)
        break
      }
      buffer += decoder.decode(value, { stream: true })
      buffer = buffer.replace(/\r\n/g, '\n')
      let sep
      while ((sep = buffer.indexOf('\n\n')) !== -1) {
        const block = buffer.slice(0, sep)
        buffer = buffer.slice(sep + 2)
        dispatchBlock(block)
      }
    }
  } finally {
    reader.releaseLock()
  }

  return donePayload
}
