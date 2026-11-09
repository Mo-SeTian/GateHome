<template>
  <div class="statistics-page animate-fade-in">
    <div class="page-topbar">
      <div>
        <h1 class="page-title">访问数据统计</h1>
        <p class="page-subtitle">24 小时流量走势、HTTP 状态码分布、Top 热门反代服务与客户端地理溯源</p>
      </div>
      <div class="header-actions">
        <div class="timeframe-selector">
          <button 
            v-for="tf in timeframes" 
            :key="tf.key"
            class="tf-btn"
            :class="{ active: selectedTf === tf.key }"
            @click="selectedTf = tf.key"
          >
            {{ tf.label }}
          </button>
        </div>
      </div>
    </div>

    <!-- 4 Stats Cards -->
    <div class="stats-metric-grid">
      <SpotlightCard class="metric-card">
        <span class="m-label">总请求数 (Hits)</span>
        <div class="m-val mono-text">1,428,904</div>
        <div class="m-sub ok-text">↑ 14.2% 较昨日</div>
      </SpotlightCard>

      <SpotlightCard class="metric-card">
        <span class="m-label">独立访客 (UV)</span>
        <div class="m-val mono-text">48,920</div>
        <div class="m-sub ok-text">↑ 8.6% 较昨日</div>
      </SpotlightCard>

      <SpotlightCard class="metric-card">
        <span class="m-label">平均响应耗时</span>
        <div class="m-val mono-text">2.14<span class="m-unit">ms</span></div>
        <div class="m-sub info-text">95% 请求 &lt; 5ms</div>
      </SpotlightCard>

      <SpotlightCard class="metric-card">
        <span class="m-label">网关健康正常率</span>
        <div class="m-val mono-text">99.98<span class="m-unit">%</span></div>
        <div class="m-sub ok-text">SLA 正常运转</div>
      </SpotlightCard>
    </div>

    <!-- Middle Row: QPS Area Chart & Status Code Donut -->
    <div class="charts-middle-grid">
      <!-- 24h QPS Trend Chart -->
      <SpotlightCard class="trend-card">
        <div class="card-title-bar">
          <span class="card-title">24 小时 QPS 吞吐走势图</span>
          <span class="badge badge-brand">峰值 384 req/s</span>
        </div>

        <div class="svg-chart-wrapper">
          <svg viewBox="0 0 760 200" class="qps-chart-svg">
            <defs>
              <linearGradient id="qpsGrad" x1="0%" y1="0%" x2="0%" y2="100%">
                <stop offset="0%" stop-color="#6366f1" stop-opacity="0.45" />
                <stop offset="100%" stop-color="#6366f1" stop-opacity="0.0" />
              </linearGradient>
            </defs>

            <!-- Grid Lines -->
            <line x1="0" y1="50" x2="760" y2="50" stroke="var(--line)" stroke-dasharray="3" />
            <line x1="0" y1="100" x2="760" y2="100" stroke="var(--line)" stroke-dasharray="3" />
            <line x1="0" y1="150" x2="760" y2="150" stroke="var(--line)" stroke-dasharray="3" />

            <!-- Area Path -->
            <path 
              d="M 0 170 Q 70 120 140 140 T 280 80 T 420 50 T 560 90 T 700 70 L 760 110 L 760 200 L 0 200 Z" 
              fill="url(#qpsGrad)" 
            />
            <!-- Stroke Path -->
            <path 
              d="M 0 170 Q 70 120 140 140 T 280 80 T 420 50 T 560 90 T 700 70 L 760 110" 
              fill="none" 
              stroke="#6366f1" 
              stroke-width="3" 
            />

            <!-- Peak point -->
            <circle cx="420" cy="50" r="5" fill="#6366f1" stroke="#fff" stroke-width="2" />
          </svg>
        </div>

        <div class="time-axis-labels">
          <span>00:00</span>
          <span>04:00</span>
          <span>08:00</span>
          <span>12:00</span>
          <span>16:00</span>
          <span>20:00</span>
          <span>当前</span>
        </div>
      </SpotlightCard>

      <!-- Status Code Distribution -->
      <SpotlightCard class="status-dist-card">
        <div class="card-title-bar">
          <span class="card-title">HTTP 状态码分布</span>
          <span class="badge badge-ok">97.3% 成功</span>
        </div>

        <div class="dist-stacked-bar">
          <div class="stack-slice s2" style="width: 94.2%;" title="2xx 正常: 94.2%" />
          <div class="stack-slice s3" style="width: 3.1%;" title="3xx 重定向: 3.1%" />
          <div class="stack-slice s4" style="width: 1.8%;" title="4xx 错误: 1.8%" />
          <div class="stack-slice s5" style="width: 0.9%;" title="5xx 异常: 0.9%" />
        </div>

        <div class="status-legend-list">
          <div class="s-legend-item">
            <span class="dot s2" />
            <span class="name">2xx 正常请求</span>
            <span class="percent mono-text">94.2%</span>
            <span class="count mono-text">1.34M</span>
          </div>
          <div class="s-legend-item">
            <span class="dot s3" />
            <span class="name">3xx 缓存/重定向</span>
            <span class="percent mono-text">3.1%</span>
            <span class="count mono-text">44.3K</span>
          </div>
          <div class="s-legend-item">
            <span class="dot s4" />
            <span class="name">4xx 客户端错误</span>
            <span class="percent mono-text">1.8%</span>
            <span class="count mono-text">25.7K</span>
          </div>
          <div class="s-legend-item">
            <span class="dot s5" />
            <span class="name">5xx 网关/上游异常</span>
            <span class="percent mono-text">0.9%</span>
            <span class="count mono-text">12.8K</span>
          </div>
        </div>
      </SpotlightCard>
    </div>

    <!-- Bottom Row: Top 5 Routes Ranking -->
    <SpotlightCard class="ranking-card">
      <div class="card-title-bar">
        <span class="card-title">热门服务访问排行榜 (Top 5)</span>
        <span class="badge badge-neutral">按请求量排序</span>
      </div>

      <div class="ranking-list">
        <div v-for="(item, idx) in topRoutes" :key="item.name" class="ranking-item">
          <div class="rank-num" :class="`top-${idx + 1}`">{{ idx + 1 }}</div>
          <div class="rank-info">
            <div class="rank-name-row">
              <span class="rank-name">{{ item.name }}</span>
              <span class="rank-count mono-text">{{ item.hits }} 请求 ({{ item.pct }}%)</span>
            </div>
            <div class="progress-track">
              <div class="progress-fill brand" :style="{ width: `${item.pct}%` }" />
            </div>
          </div>
        </div>
      </div>
    </SpotlightCard>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import SpotlightCard from '@/components/SpotlightCard.vue'

const selectedTf = ref('today')
const timeframes = [
  { key: 'today', label: '今日 (24h)' },
  { key: '7d', label: '近 7 天' },
  { key: '30d', label: '近 30 天' }
]

const topRoutes = [
  { name: 'Prometheus 遥测采集 (monitor.ops.lan)', hits: '6.7M', pct: 46.8 },
  { name: 'GateHome 核心控制台 (gate.internal.lan)', hits: '2.4M', pct: 28.2 },
  { name: 'HomeAssistant 智能家居 (home.iot.tech)', hits: '1.8M', pct: 14.5 },
  { name: 'Nextcloud 云盘服务 (cloud.nas.tech)', hits: '890K', pct: 6.8 },
  { name: 'Jellyfin 影音媒体中心 (media.stream.org)', hits: '520K', pct: 3.7 }
]
</script>

<style scoped>
.statistics-page { display: flex; flex-direction: column; gap: 20px; }
.page-topbar { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 16px; }
.page-title { font-size: 22px; font-weight: 700; color: var(--ink); }
.page-subtitle { font-size: 13px; color: var(--muted); margin-top: 3px; }

.timeframe-selector {
  display: flex; background: var(--strip-bg); border: 1px solid var(--line);
  border-radius: var(--radius-sm); padding: 3px;
}
.tf-btn {
  padding: 5px 12px; border-radius: 6px; font-size: 12px;
  color: var(--muted); font-weight: 500; transition: var(--transition-smooth);
}
.tf-btn.active {
  background: var(--card-bg); color: var(--ink); font-weight: 600;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.2);
}

.stats-metric-grid { display: grid; grid-template-columns: repeat(4, 1fr); gap: 16px; }
.metric-card { padding: 18px 20px; display: flex; flex-direction: column; gap: 4px; }
.m-label { font-size: 12px; color: var(--muted); }
.m-val { font-size: 24px; font-weight: 700; color: var(--ink); }
.m-unit { font-size: 14px; color: var(--faint); margin-left: 2px; }
.m-sub { font-size: 11px; font-weight: 500; }
.ok-text { color: var(--ok); }
.info-text { color: var(--brand-cyan); }

.charts-middle-grid { display: grid; grid-template-columns: 2fr 1fr; gap: 16px; }

.trend-card, .status-dist-card, .ranking-card { padding: 20px; display: flex; flex-direction: column; }
.card-title-bar { display: flex; align-items: center; justify-content: space-between; margin-bottom: 16px; }
.card-title { font-size: 14px; font-weight: 700; color: var(--ink); }

.svg-chart-wrapper { height: 180px; width: 100%; }
.qps-chart-svg { width: 100%; height: 100%; }

.time-axis-labels {
  display: flex; justify-content: space-between; font-size: 11px;
  color: var(--faint); font-family: var(--mono); padding-top: 10px;
}

/* Stacked bar */
.dist-stacked-bar {
  display: flex; height: 12px; border-radius: 999px; overflow: hidden;
  margin-bottom: 16px; background: var(--strip-bg);
}
.stack-slice.s2 { background: var(--ok); }
.stack-slice.s3 { background: #38bdf8; }
.stack-slice.s4 { background: var(--warn); }
.stack-slice.s5 { background: var(--bad); }

.status-legend-list { display: flex; flex-direction: column; gap: 10px; }
.s-legend-item { display: flex; align-items: center; font-size: 12px; }
.dot { width: 8px; height: 8px; border-radius: 50%; margin-right: 8px; }
.dot.s2 { background: var(--ok); }
.dot.s3 { background: #38bdf8; }
.dot.s4 { background: var(--warn); }
.dot.s5 { background: var(--bad); }
.name { flex: 1; color: var(--ink-secondary); }
.percent { font-weight: 600; color: var(--ink); margin-right: 12px; }
.count { color: var(--faint); }

/* Ranking List */
.ranking-list { display: flex; flex-direction: column; gap: 14px; }
.ranking-item { display: flex; align-items: center; gap: 14px; }
.rank-num {
  width: 24px; height: 24px; border-radius: 6px; font-size: 12px;
  font-weight: 700; display: flex; align-items: center; justify-content: center;
  background: var(--strip-bg); color: var(--muted); font-family: var(--mono);
}
.rank-num.top-1 { background: rgba(245, 158, 11, 0.2); color: var(--warn); }
.rank-num.top-2 { background: rgba(99, 102, 241, 0.2); color: var(--brand-light); }
.rank-num.top-3 { background: rgba(6, 182, 212, 0.2); color: var(--brand-cyan); }

.rank-info { flex: 1; display: flex; flex-direction: column; gap: 6px; }
.rank-name-row { display: flex; justify-content: space-between; font-size: 12.5px; }
.rank-name { color: var(--ink); font-weight: 500; }
.rank-count { color: var(--muted); font-size: 11.5px; }

.progress-track { height: 6px; background: var(--strip-bg); border-radius: 999px; overflow: hidden; }
.progress-fill.brand { height: 100%; background: var(--brand-gradient); border-radius: 999px; }

@media (max-width: 1100px) {
  .stats-metric-grid { grid-template-columns: repeat(2, 1fr); }
  .charts-middle-grid { grid-template-columns: 1fr; }
}
@media (max-width: 600px) {
  .stats-metric-grid { grid-template-columns: 1fr; }
}
</style>
