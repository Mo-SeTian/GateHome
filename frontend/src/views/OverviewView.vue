<template>
  <div class="overview-page animate-fade-in">
    <!-- Page Header -->
    <div class="page-topbar">
      <div>
        <h1 class="page-title">系统概览</h1>
        <p class="page-subtitle">GateHome 实时遥测指标、网关流量雷达与运行状态监控</p>
      </div>
      <div class="header-actions">
        <button class="btn btn-secondary" @click="triggerRefresh">
          <RefreshCw :size="14" :class="{ 'spin-icon': isRefreshing }" />
          刷新数据
        </button>
        <button class="btn btn-primary" @click="$router.push('/routes')">
          <Plus :size="14" />
          新建反代
        </button>
      </div>
    </div>

    <!-- Bento Top Row: 4 Metric Cards -->
    <div class="bento-grid">
      <!-- Card 1: CPU -->
      <SpotlightCard class="metric-card">
        <div class="card-header">
          <div class="metric-info">
            <span class="metric-label">CPU 负载率</span>
            <div class="metric-value">
              {{ telemetry.cpu }}<span class="metric-unit">%</span>
            </div>
          </div>
          <div class="metric-icon-box cpu">
            <Cpu :size="20" />
          </div>
        </div>
        <div class="progress-track">
          <div class="progress-fill brand" :style="{ width: `${telemetry.cpu}%` }" />
        </div>
        <div class="metric-footer">
          <span>4 核心 / 8 线程</span>
          <span class="mono-text">load: 0.24, 0.18</span>
        </div>
      </SpotlightCard>

      <!-- Card 2: Memory -->
      <SpotlightCard class="metric-card">
        <div class="card-header">
          <div class="metric-info">
            <span class="metric-label">内存占用</span>
            <div class="metric-value">
              {{ telemetry.memory }}<span class="metric-unit">%</span>
            </div>
          </div>
          <div class="metric-icon-box mem">
            <Activity :size="20" />
          </div>
        </div>
        <div class="progress-track">
          <div class="progress-fill cyan" :style="{ width: `${telemetry.memory}%` }" />
        </div>
        <div class="metric-footer">
          <span>已用 {{ telemetry.memoryUsed }}</span>
          <span class="mono-text">共 {{ telemetry.memoryTotal }}</span>
        </div>
      </SpotlightCard>

      <!-- Card 3: Storage -->
      <SpotlightCard class="metric-card">
        <div class="card-header">
          <div class="metric-info">
            <span class="metric-label">磁盘空间</span>
            <div class="metric-value">
              {{ telemetry.disk }}<span class="metric-unit">%</span>
            </div>
          </div>
          <div class="metric-icon-box disk">
            <HardDrive :size="20" />
          </div>
        </div>
        <div class="progress-track">
          <div class="progress-fill ok" :style="{ width: `${telemetry.disk}%` }" />
        </div>
        <div class="metric-footer">
          <span>已用 {{ telemetry.diskUsed }}</span>
          <span class="mono-text">共 {{ telemetry.diskTotal }}</span>
        </div>
      </SpotlightCard>

      <!-- Card 4: QPS & Traffic -->
      <SpotlightCard class="metric-card">
        <div class="card-header">
          <div class="metric-info">
            <span class="metric-label">实时 QPS</span>
            <div class="metric-value">
              {{ telemetry.qps }}<span class="metric-unit">req/s</span>
            </div>
          </div>
          <div class="metric-icon-box net">
            <Zap :size="20" />
          </div>
        </div>
        <div class="metric-extra-badges">
          <span class="badge badge-brand">并发连接 {{ telemetry.activeConns }}</span>
          <span class="badge badge-ok">正常率 99.98%</span>
        </div>
        <div class="metric-footer">
          <span>今日已处理 1.42M 次</span>
          <span class="mono-text">平均 2.1ms</span>
        </div>
      </SpotlightCard>
    </div>

    <!-- Center Section: Network Wave Radar & Status Tiles -->
    <div class="middle-grid">
      <!-- Traffic Radar Chart Card -->
      <SpotlightCard class="chart-card">
        <div class="section-title-bar">
          <div class="title-with-icon">
            <Radio :size="17" class="brand-text" />
            <span class="section-title">实时网络流量雷达</span>
          </div>
          <div class="chart-legend">
            <span class="legend-item"><span class="legend-dot rx" />下行 {{ telemetry.rxRate }}</span>
            <span class="legend-item"><span class="legend-dot tx" />上行 {{ telemetry.txRate }}</span>
          </div>
        </div>

        <!-- SVG Interactive Waveform -->
        <div class="chart-container">
          <svg viewBox="0 0 700 160" class="traffic-svg" preserveAspectRatio="none">
            <defs>
              <linearGradient id="gradRx" x1="0%" y1="0%" x2="0%" y2="100%">
                <stop offset="0%" stop-color="#06b6d4" stop-opacity="0.35" />
                <stop offset="100%" stop-color="#06b6d4" stop-opacity="0.0" />
              </linearGradient>
              <linearGradient id="gradTx" x1="0%" y1="0%" x2="0%" y2="100%">
                <stop offset="0%" stop-color="#6366f1" stop-opacity="0.4" />
                <stop offset="100%" stop-color="#6366f1" stop-opacity="0.0" />
              </linearGradient>
            </defs>

            <!-- Grid Lines -->
            <line x1="0" y1="40" x2="700" y2="40" stroke="var(--line)" stroke-dasharray="4" />
            <line x1="0" y1="80" x2="700" y2="80" stroke="var(--line)" stroke-dasharray="4" />
            <line x1="0" y1="120" x2="700" y2="120" stroke="var(--line)" stroke-dasharray="4" />

            <!-- TX Wave (Violet) -->
            <path 
              d="M 0 130 Q 70 80 140 100 T 280 60 T 420 90 T 560 40 T 700 70 L 700 160 L 0 160 Z" 
              fill="url(#gradTx)" 
            />
            <path 
              d="M 0 130 Q 70 80 140 100 T 280 60 T 420 90 T 560 40 T 700 70" 
              fill="none" 
              stroke="#6366f1" 
              stroke-width="2.5" 
            />

            <!-- RX Wave (Cyan) -->
            <path 
              d="M 0 140 Q 70 120 140 110 T 280 100 T 420 120 T 560 90 T 700 110 L 700 160 L 0 160 Z" 
              fill="url(#gradRx)" 
            />
            <path 
              d="M 0 140 Q 70 120 140 110 T 280 100 T 420 120 T 560 90 T 700 110" 
              fill="none" 
              stroke="#06b6d4" 
              stroke-width="2" 
            />

            <!-- Pulse Dots -->
            <circle cx="560" cy="40" r="4" fill="#6366f1" class="pulse-point" />
            <circle cx="560" cy="90" r="3.5" fill="#06b6d4" class="pulse-point" />
          </svg>
        </div>

        <div class="chart-footer-stats">
          <div class="stat-pill">
            <span class="label">累计接收 (RX)</span>
            <span class="val mono-text">{{ telemetry.rxTotal }}</span>
          </div>
          <div class="stat-pill">
            <span class="label">累计发送 (TX)</span>
            <span class="val mono-text">{{ telemetry.txTotal }}</span>
          </div>
          <div class="stat-pill">
            <span class="label">网卡状态</span>
            <span class="val ok-text">eth0 · 10Gbps 全双工</span>
          </div>
        </div>
      </SpotlightCard>

      <!-- Services Quick Status Radar -->
      <SpotlightCard class="status-summary-card">
        <div class="section-title-bar">
          <span class="section-title">核心模块健康度</span>
          <span class="badge badge-ok">ALL SYSTEMS NORMAL</span>
        </div>

        <div class="status-rows">
          <div class="status-item" @click="$router.push('/routes')">
            <div class="item-left">
              <span class="status-dot ok" />
              <span class="item-name">反向代理矩阵</span>
            </div>
            <span class="item-val">{{ telemetry.routesActive }} / {{ telemetry.routesTotal }} 在线</span>
          </div>

          <div class="status-item" @click="$router.push('/firewall')">
            <div class="item-left">
              <span class="status-dot ok" />
              <span class="item-name">WAF 智能防火墙</span>
            </div>
            <span class="item-val">已拦截 {{ telemetry.wafBlockedToday }} 次</span>
          </div>

          <div class="status-item" @click="$router.push('/ddns')">
            <div class="item-left">
              <span class="status-dot ok" />
              <span class="item-name">DDNS 动态域名</span>
            </div>
            <span class="item-val ok-text">已全部同步 (4/4)</span>
          </div>

          <div class="status-item" @click="$router.push('/certificates')">
            <div class="item-left">
              <span class="status-dot warn" />
              <span class="item-name">SSL 证书到期预警</span>
            </div>
            <span class="item-val warn-text">1 证书 11天后到期</span>
          </div>

          <div class="status-item" @click="$router.push('/mcp')">
            <div class="item-left">
              <span class="status-dot ok" />
              <span class="item-name">AI MCP 服务</span>
            </div>
            <span class="item-val">3 客户端已连接</span>
          </div>
        </div>

        <div class="quick-links-bar">
          <button class="btn btn-secondary btn-sm" @click="$router.push('/logs')">
            <ScrollText :size="13" /> 访问明细
          </button>
          <button class="btn btn-secondary btn-sm" @click="$router.push('/firewall-logs')">
            <ShieldAlert :size="13" /> 拦截审计
          </button>
          <button class="btn btn-secondary btn-sm" @click="$router.push('/backups')">
            <HardDriveDownload :size="13" /> 备份快照
          </button>
        </div>
      </SpotlightCard>
    </div>

    <!-- Bottom Row: Recent Routes Snapshot -->
    <div class="bottom-section">
      <div class="section-title-bar">
        <span class="section-title">高频访问服务 (Top 4)</span>
        <router-link to="/routes" class="btn btn-ghost btn-sm">
          查看全部 {{ routes.length }} 个代理路由 →
        </router-link>
      </div>

      <div class="recent-routes-grid">
        <SpotlightCard 
          v-for="rt in routes.slice(0, 4)" 
          :key="rt.id" 
          class="mini-route-card"
          @click="$router.push('/routes')"
        >
          <div class="mini-card-top">
            <div class="mini-card-icon">
              <Shield v-if="rt.icon === 'shield'" :size="16" />
              <HardDrive v-else-if="rt.icon === 'hard-drive'" :size="16" />
              <Key v-else-if="rt.icon === 'key'" :size="16" />
              <Home v-else :size="16" />
            </div>
            <span class="status-dot ok" />
          </div>
          <div class="mini-card-name">{{ rt.name }}</div>
          <div class="mini-card-domain mono-text">{{ rt.domain }}</div>
          <div class="mini-card-footer">
            <span class="hits-text">{{ rt.hitCount }} 请求</span>
            <span class="latency-text">{{ rt.latency }}</span>
          </div>
        </SpotlightCard>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { 
  RefreshCw, Plus, Cpu, Activity, HardDrive, Zap, Radio, 
  ScrollText, ShieldAlert, HardDriveDownload, Shield, Key, Home
} from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import { useAppStore } from '@/composables/useAppStore'
import { useToast } from '@/composables/useToast'

const { telemetry, routes } = useAppStore()
const { success } = useToast()

const isRefreshing = ref(false)

const triggerRefresh = () => {
  isRefreshing.value = true
  setTimeout(() => {
    isRefreshing.value = false
    success('系统遥测与流量雷达数据已刷新')
  }, 600)
}
</script>

<style scoped>
.overview-page {
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.page-topbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 16px;
}

.page-title {
  font-size: 22px;
  font-weight: 700;
  color: var(--ink);
  letter-spacing: -0.3px;
}

.page-subtitle {
  font-size: 13px;
  color: var(--muted);
  margin-top: 3px;
}

.header-actions {
  display: flex;
  align-items: center;
  gap: 10px;
}

.spin-icon {
  animation: spin 0.8s linear infinite;
}
@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

/* Bento Grid */
.bento-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 16px;
}

.metric-card {
  padding: 18px 20px;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
}

.card-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
}

.metric-label {
  font-size: 12px;
  color: var(--muted);
  font-weight: 500;
}

.metric-value {
  font-size: 26px;
  font-weight: 700;
  color: var(--ink);
  font-family: var(--sans);
  margin-top: 4px;
}

.metric-unit {
  font-size: 14px;
  font-weight: 500;
  color: var(--faint);
  margin-left: 4px;
}

.metric-icon-box {
  width: 38px;
  height: 38px;
  border-radius: var(--radius-sm);
  display: flex;
  align-items: center;
  justify-content: center;
  border: 1px solid var(--line);
}
.metric-icon-box.cpu { background: rgba(99, 102, 241, 0.12); color: var(--brand-light); }
.metric-icon-box.mem { background: rgba(6, 182, 212, 0.12); color: var(--brand-cyan); }
.metric-icon-box.disk { background: rgba(16, 185, 129, 0.12); color: var(--ok); }
.metric-icon-box.net { background: rgba(245, 158, 11, 0.12); color: var(--warn); }

.progress-track {
  width: 100%;
  height: 6px;
  background: var(--strip-bg);
  border-radius: 999px;
  overflow: hidden;
  margin: 14px 0 10px 0;
}

.progress-fill {
  height: 100%;
  border-radius: 999px;
  transition: width 0.5s ease;
}
.progress-fill.brand { background: var(--brand-gradient); }
.progress-fill.cyan { background: linear-gradient(90deg, #06b6d4, #38bdf8); }
.progress-fill.ok { background: linear-gradient(90deg, #10b981, #34d399); }

.metric-extra-badges {
  display: flex;
  gap: 8px;
  margin: 12px 0 8px 0;
}

.metric-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 11.5px;
  color: var(--faint);
}

.mono-text {
  font-family: var(--mono);
}

/* Middle Grid */
.middle-grid {
  display: grid;
  grid-template-columns: 2fr 1fr;
  gap: 16px;
}

.chart-card {
  padding: 20px;
  display: flex;
  flex-direction: column;
}

.section-title-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}

.title-with-icon {
  display: flex;
  align-items: center;
  gap: 8px;
}
.brand-text { color: var(--brand-light); }

.section-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--ink);
}

.chart-legend {
  display: flex;
  gap: 14px;
  font-size: 12px;
  color: var(--muted);
}
.legend-item {
  display: flex;
  align-items: center;
  gap: 6px;
}
.legend-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
}
.legend-dot.rx { background: #06b6d4; box-shadow: 0 0 8px rgba(6, 182, 212, 0.5); }
.legend-dot.tx { background: #6366f1; box-shadow: 0 0 8px rgba(99, 102, 241, 0.5); }

.chart-container {
  height: 160px;
  width: 100%;
}

.traffic-svg {
  width: 100%;
  height: 100%;
}

.pulse-point {
  animation: pulse 1.8s infinite;
}

.chart-footer-stats {
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-top: 1px solid var(--line);
  padding-top: 14px;
  margin-top: 14px;
}

.stat-pill {
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.stat-pill .label { font-size: 11px; color: var(--faint); }
.stat-pill .val { font-size: 12.5px; font-weight: 600; color: var(--ink); }
.ok-text { color: var(--ok); }
.warn-text { color: var(--warn); }

/* Status Summary Card */
.status-summary-card {
  padding: 20px;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
}

.status-rows {
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-bottom: 16px;
}

.status-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 10px;
  border-radius: var(--radius-sm);
  background: var(--strip-bg);
  cursor: pointer;
  transition: var(--transition-smooth);
}
.status-item:hover {
  background: var(--card-hover-bg);
  transform: translateX(2px);
}

.item-left {
  display: flex;
  align-items: center;
  gap: 8px;
}

.item-name {
  font-size: 12.5px;
  color: var(--ink-secondary);
  font-weight: 500;
}

.item-val {
  font-size: 11.5px;
  color: var(--muted);
  font-family: var(--mono);
}

.quick-links-bar {
  display: flex;
  gap: 8px;
  padding-top: 12px;
  border-top: 1px solid var(--line);
}

/* Bottom Recent Routes */
.bottom-section {
  display: flex;
  flex-direction: column;
}

.recent-routes-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 16px;
}

.mini-route-card {
  padding: 16px;
  cursor: pointer;
}

.mini-card-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 10px;
}

.mini-card-icon {
  width: 30px;
  height: 30px;
  border-radius: var(--radius-sm);
  background: rgba(99, 102, 241, 0.12);
  color: var(--brand-light);
  display: flex;
  align-items: center;
  justify-content: center;
}

.mini-card-name {
  font-size: 13.5px;
  font-weight: 600;
  color: var(--ink);
  margin-bottom: 2px;
}

.mini-card-domain {
  font-size: 11.5px;
  color: var(--faint);
  margin-bottom: 12px;
}

.mini-card-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 11px;
  color: var(--muted);
  border-top: 1px solid var(--line);
  padding-top: 8px;
}

.latency-text {
  color: var(--ok);
  font-family: var(--mono);
}

/* Responsive */
@media (max-width: 1200px) {
  .bento-grid {
    grid-template-columns: repeat(2, 1fr);
  }
  .middle-grid {
    grid-template-columns: 1fr;
  }
  .recent-routes-grid {
    grid-template-columns: repeat(2, 1fr);
  }
}

@media (max-width: 767px) {
  .bento-grid {
    grid-template-columns: 1fr;
  }
  .recent-routes-grid {
    grid-template-columns: 1fr;
  }
  .chart-footer-stats {
    flex-direction: column;
    align-items: flex-start;
    gap: 8px;
  }
}
</style>
