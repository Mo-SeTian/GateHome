<template>
  <div class="mcp-page animate-fade-in">
    <div class="page-topbar">
      <div>
        <h1 class="page-title">AI 智能管理 (Model Context Protocol)</h1>
        <p class="page-subtitle">GateHome 原生内置 MCP Server，赋予 Claude、Cursor、Antigravity 等智能体对网关的完全可控治理能力</p>
      </div>
      <div class="header-actions">
        <span class="badge badge-brand">
          <Bot :size="13" /> MCP 协议 v1.0 就绪
        </span>
      </div>
    </div>

    <!-- Status & Clients Grid -->
    <div class="mcp-top-grid">
      <!-- MCP Server Status Card -->
      <SpotlightCard class="status-card">
        <div class="card-title-bar">
          <span class="card-title">MCP 网关服务端</span>
          <span class="badge badge-ok">
            <span class="status-dot ok" /> 运行中
          </span>
        </div>

        <div class="server-meta">
          <div class="meta-row">
            <span class="label">传输通道:</span>
            <span class="val mono-text">{{ mcp.transport }}</span>
          </div>
          <div class="meta-row">
            <span class="label">协议标准:</span>
            <span class="val mono-text">JSON-RPC 2.0 (Stream)</span>
          </div>
          <div class="meta-row">
            <span class="label">工具总数:</span>
            <span class="val">{{ mcp.tools.length }} 个标准化工具</span>
          </div>
        </div>

        <div class="card-footer">
          <button class="btn btn-secondary btn-sm" @click="copyConfig">
            <Copy :size="12" /> 复制 client_config.json
          </button>
        </div>
      </SpotlightCard>

      <!-- Connected AI Clients -->
      <SpotlightCard class="clients-card">
        <div class="card-title-bar">
          <span class="card-title">已连接的 AI 客户端 ({{ mcp.clients.length }})</span>
          <span class="badge badge-neutral">活跃会话</span>
        </div>

        <div class="clients-list">
          <div v-for="client in mcp.clients" :key="client.id" class="client-item">
            <div class="client-left">
              <span class="status-dot ok" />
              <div class="client-info">
                <span class="client-name">{{ client.name }}</span>
                <span class="client-ip mono-text">{{ client.ip }}</span>
              </div>
            </div>
            <span class="client-time">{{ client.connectedAt }}</span>
          </div>
        </div>
      </SpotlightCard>
    </div>

    <!-- Tools Showcase & Interactive Test Playground -->
    <div class="tools-grid">
      <!-- Registered Tools List -->
      <SpotlightCard class="tools-card">
        <div class="card-title-bar">
          <span class="card-title">已暴露的网关控制工具集 (Tools)</span>
          <span class="badge badge-neutral">只读 & 可控操作</span>
        </div>

        <div class="tools-list">
          <div 
            v-for="t in mcp.tools" 
            :key="t.name"
            class="tool-item"
            :class="{ active: selectedTool.name === t.name }"
            @click="selectTool(t)"
          >
            <div class="tool-head">
              <span class="tool-name mono-text">{{ t.name }}</span>
              <span class="call-count mono-text">调用 {{ t.calls }} 次</span>
            </div>
            <p class="tool-desc">{{ t.desc }}</p>
          </div>
        </div>
      </SpotlightCard>

      <!-- Interactive Test Playground -->
      <SpotlightCard class="playground-card">
        <div class="card-title-bar">
          <div class="playground-title-row">
            <Terminal :size="16" class="brand-text" />
            <span class="card-title">工具调试沙箱: {{ selectedTool.name }}</span>
          </div>
          <button class="btn btn-primary btn-sm" @click="testInvoke" :disabled="isInvoking">
            <Play :size="12" /> {{ isInvoking ? '调用中...' : '测试调用 (Invoke)' }}
          </button>
        </div>

        <div class="terminal-box">
          <div class="terminal-bar">
            <span>JSON-RPC 2.0 Response</span>
            <span class="latency-text mono-text">0.4ms</span>
          </div>
          <pre class="terminal-code"><code>{{ invocationResult }}</code></pre>
        </div>
      </SpotlightCard>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { Bot, Copy, Terminal, Play } from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import { useAppStore } from '@/composables/useAppStore'
import { useToast } from '@/composables/useToast'

const { mcp, routes } = useAppStore()
const { success } = useToast()

const selectedTool = ref(mcp.tools[0])
const isInvoking = ref(false)

const invocationResult = ref(`{
  "jsonrpc": "2.0",
  "result": {
    "total_routes": 8,
    "active_routes": 7,
    "gateways": [
      { "name": "GateHome 核心控制台", "domain": "gate.internal.lan", "status": "active" },
      { "name": "Nextcloud 云盘服务", "domain": "cloud.nas.tech", "status": "active" }
    ]
  },
  "id": "req_101"
}`)

const selectTool = (t) => {
  selectedTool.value = t
  testInvoke()
}

const testInvoke = () => {
  isInvoking.value = true
  setTimeout(() => {
    isInvoking.value = false
    selectedTool.value.calls++

    if (selectedTool.value.name === 'list_proxy_routes') {
      invocationResult.value = JSON.stringify({
        jsonrpc: "2.0",
        result: {
          routes_count: routes.value.length,
          items: routes.value.slice(0, 3).map(r => ({ name: r.name, domain: r.domain, target: r.target, status: r.status }))
        },
        id: `call_${Date.now()}`
      }, null, 2)
    } else if (selectedTool.value.name === 'get_system_telemetry') {
      invocationResult.value = JSON.stringify({
        jsonrpc: "2.0",
        result: {
          cpu_percent: 18.4,
          memory_used_gb: "3.37 / 8.00 GB",
          current_qps: 184,
          health: "OK"
        },
        id: `call_${Date.now()}`
      }, null, 2)
    } else if (selectedTool.value.name === 'block_ip_address') {
      invocationResult.value = JSON.stringify({
        jsonrpc: "2.0",
        result: {
          blocked_ip: "198.51.100.45",
          action: "BLOCK_PERMANENT",
          waf_rule_updated: true
        },
        id: `call_${Date.now()}`
      }, null, 2)
    } else {
      invocationResult.value = JSON.stringify({
        jsonrpc: "2.0",
        result: {
          status: "SUCCESS",
          route_created: "demo.internal.lan -> http://127.0.0.1:3000",
          ssl_issued: true
        },
        id: `call_${Date.now()}`
      }, null, 2)
    }

    success(`MCP 工具 "${selectedTool.value.name}" 调用成功`)
  }, 400)
}

const copyConfig = () => {
  const cfg = {
    mcpServers: {
      gatehome: {
        command: "gatehouse",
        args: ["mcp", "--transport=sse", "--port=8089"]
      }
    }
  }
  navigator.clipboard?.writeText(JSON.stringify(cfg, null, 2))
  success('已复制 Claude / Cursor 的 MCP 配置文件')
}
</script>

<style scoped>
.mcp-page { display: flex; flex-direction: column; gap: 20px; }
.page-topbar { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 16px; }
.page-title { font-size: 22px; font-weight: 700; color: var(--ink); }
.page-subtitle { font-size: 13px; color: var(--muted); margin-top: 3px; }

.mcp-top-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
.status-card, .clients-card { padding: 20px; display: flex; flex-direction: column; justify-content: space-between; }
.card-title-bar { display: flex; align-items: center; justify-content: space-between; margin-bottom: 14px; }
.card-title { font-size: 14px; font-weight: 700; color: var(--ink); }

.server-meta { display: flex; flex-direction: column; gap: 8px; font-size: 12.5px; }
.meta-row { display: flex; align-items: center; justify-content: space-between; }
.meta-row .label { color: var(--faint); }
.meta-row .val { color: var(--ink); font-weight: 500; }

.clients-list { display: flex; flex-direction: column; gap: 8px; }
.client-item {
  display: flex; align-items: center; justify-content: space-between;
  padding: 8px 12px; border-radius: var(--radius-sm);
  background: var(--strip-bg); border: 1px solid var(--line);
}
.client-left { display: flex; align-items: center; gap: 10px; }
.client-info { display: flex; flex-direction: column; }
.client-name { font-size: 12.5px; font-weight: 600; color: var(--ink); }
.client-ip { font-size: 11px; color: var(--faint); }
.client-time { font-size: 11px; color: var(--faint); }

.card-footer { padding-top: 14px; border-top: 1px solid var(--line); margin-top: 14px; }

/* Tools & Playground Grid */
.tools-grid { display: grid; grid-template-columns: 1fr 1.2fr; gap: 16px; }
.tools-card, .playground-card { padding: 20px; display: flex; flex-direction: column; }

.tools-list { display: flex; flex-direction: column; gap: 10px; }
.tool-item {
  padding: 12px 14px; border-radius: var(--radius-sm);
  background: var(--strip-bg); border: 1px solid var(--line);
  cursor: pointer; transition: var(--transition-smooth);
}
.tool-item:hover, .tool-item.active {
  background: var(--card-hover-bg); border-color: var(--brand);
}
.tool-item.active { box-shadow: 0 0 16px rgba(99, 102, 241, 0.2); }

.tool-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 4px; }
.tool-name { font-size: 13px; font-weight: 700; color: var(--brand-light); }
.call-count { font-size: 11px; color: var(--faint); }
.tool-desc { font-size: 12px; color: var(--muted); margin: 0; }

.playground-title-row { display: flex; align-items: center; gap: 8px; }
.brand-text { color: var(--brand-light); }

.terminal-box {
  background: #030712; border: 1px solid var(--line);
  border-radius: var(--radius-sm); overflow: hidden; flex: 1; display: flex; flex-direction: column;
}
.terminal-bar {
  display: flex; align-items: center; justify-content: space-between;
  padding: 8px 14px; background: rgba(255, 255, 255, 0.04);
  border-bottom: 1px solid rgba(255, 255, 255, 0.06); font-size: 11px; color: var(--faint);
}
.latency-text { color: var(--ok); }

.terminal-code {
  padding: 14px; font-family: var(--mono); font-size: 12px;
  color: #38bdf8; overflow-x: auto; margin: 0; flex: 1;
}

@media (max-width: 900px) {
  .mcp-top-grid, .tools-grid { grid-template-columns: 1fr; }
}
</style>
