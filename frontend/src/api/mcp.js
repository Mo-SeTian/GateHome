// MCP uses its own Bearer credential; an expired client token must not end the admin session.
let nextRequestId = 0

export async function mcpRequest(token, method, params, signal) {
  let response
  try {
    response = await fetch('/mcp', {
      method: 'POST',
      credentials: 'omit',
      signal,
      headers: {
        Authorization: `Bearer ${token}`,
        'Content-Type': 'application/json',
        Accept: 'application/json, text/event-stream',
        'MCP-Protocol-Version': '2025-03-26'
      },
      body: JSON.stringify({ jsonrpc: '2.0', id: ++nextRequestId, method, params })
    })
  } catch (err) {
    if (err.name === 'AbortError') throw err
    throw new Error('无法连接 MCP 服务，请检查网关和转发配置')
  }
  if (!response.ok) throw new Error(`MCP 请求失败（HTTP ${response.status}），请检查服务开关、Token 和权限`)
  const message = await response.json().catch(() => null)
  if (!message) throw new Error('MCP 返回了无效响应，请检查 /mcp 转发')
  if (message.error) throw new Error(`MCP 协议错误（${message.error.code}）`)
  return message.result
}
