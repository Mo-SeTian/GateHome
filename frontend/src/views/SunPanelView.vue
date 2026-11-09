<template>
  <div class="sunpanel-page animate-fade-in">
    <div class="page-topbar">
      <div>
        <h1 class="page-title">SunPanel 导航首页</h1>
        <p class="page-subtitle">内置私有导航页，集中管理 NAS、开发服务器与常用内网服务的快速书签入口</p>
      </div>
      <div class="header-actions">
        <button class="btn btn-secondary" @click="reloadIframe">
          <RotateCw :size="14" :class="{ 'spin-icon': isReloading }" />
          重载页面
        </button>
        <button class="btn btn-primary" @click="openExternal">
          <ExternalLink :size="14" />
          新标签页打开
        </button>
      </div>
    </div>

    <!-- Quick Bookmark Cards Hub -->
    <div class="bookmarks-grid">
      <SpotlightCard 
        v-for="item in bookmarks" 
        :key="item.name"
        class="bm-card"
        @click="openUrl(item.url)"
      >
        <div class="bm-icon-box" :style="{ color: item.color }">
          <component :is="item.icon" :size="20" />
        </div>
        <div class="bm-info">
          <span class="bm-title">{{ item.name }}</span>
          <span class="bm-desc">{{ item.desc }}</span>
        </div>
        <ArrowUpRight :size="14" class="bm-arrow" />
      </SpotlightCard>
    </div>

    <!-- Embedded Container Card -->
    <SpotlightCard class="frame-card">
      <div class="frame-bar">
        <div class="frame-dots">
          <span class="f-dot red" />
          <span class="f-dot yellow" />
          <span class="f-dot green" />
        </div>
        <div class="frame-address mono-text">
          <Lock :size="12" class="ok-text" />
          {{ sunpanelUrl }}
        </div>
        <div class="frame-status">
          <span class="status-dot ok" />
          <span>服务在线</span>
        </div>
      </div>

      <div class="frame-viewport">
        <!-- Mock SunPanel Showcase or Iframe -->
        <div class="mock-sunpanel-content">
          <div class="sun-hero">
            <div class="sun-logo-glow">
              <Compass :size="38" class="sun-icon" />
            </div>
            <h2 class="sun-heading">GateHome · SunPanel 极客导航</h2>
            <p class="sun-tagline">快速直达您的私有云与物联网集群</p>
          </div>

          <div class="sun-category-title">常用控制面板</div>
          <div class="sun-cards-row">
            <div class="sun-card-tile" @click="$router.push('/routes')">
              <Network :size="24" class="tile-icon" />
              <div class="tile-title">反代控制矩阵</div>
              <div class="tile-sub">已连接 8 个上游服务</div>
            </div>
            <div class="sun-card-tile" @click="$router.push('/overview')">
              <Activity :size="24" class="tile-icon" />
              <div class="tile-title">系统遥测监控</div>
              <div class="tile-sub">实时负载 18.4%</div>
            </div>
            <div class="sun-card-tile" @click="$router.push('/firewall')">
              <ShieldAlert :size="24" class="tile-icon" />
              <div class="tile-title">WAF 安全拦截</div>
              <div class="tile-sub">拦截 1,384 次恶意探测</div>
            </div>
          </div>
        </div>
      </div>
    </SpotlightCard>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { 
  RotateCw, ExternalLink, ArrowUpRight, Lock, Compass, Network, Activity, 
  ShieldAlert, HardDrive, Key, Home, Play, Code
} from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import { useToast } from '@/composables/useToast'

const { success } = useToast()

const sunpanelUrl = ref('http://127.0.0.1:8080/sunpanel/')
const isReloading = ref(false)

const bookmarks = [
  { name: 'Nextcloud 网盘', desc: '私有文件同步与 WebDAV 存储', url: 'https://cloud.nas.tech', color: '#06b6d4', icon: HardDrive },
  { name: 'Vaultwarden 密码库', desc: '端到端加密密码保险箱', url: 'https://vault.nas.tech', color: '#6366f1', icon: Key },
  { name: 'HomeAssistant 物联网', desc: '全屋智能家居自动化中枢', url: 'https://home.iot.tech', color: '#10b981', icon: Home },
  { name: 'Jellyfin 影视中心', desc: '4K HDR 硬件转码多媒体库', url: 'https://media.stream.org', color: '#f43f5e', icon: Play },
  { name: 'Gitea 私有代码库', desc: '自建 Git 仓库与流水线', url: 'https://git.dev.lan', color: '#c084fc', icon: Code }
]

const reloadIframe = () => {
  isReloading.value = true
  setTimeout(() => {
    isReloading.value = false
    success('SunPanel 导航内容已重新加载')
  }, 500)
}

const openExternal = () => {
  window.open(sunpanelUrl.value, '_blank')
}

const openUrl = (url) => {
  window.open(url, '_blank')
}
</script>

<style scoped>
.sunpanel-page { display: flex; flex-direction: column; gap: 20px; }
.page-topbar { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 16px; }
.page-title { font-size: 22px; font-weight: 700; color: var(--ink); }
.page-subtitle { font-size: 13px; color: var(--muted); margin-top: 3px; }
.header-actions { display: flex; gap: 10px; }
.spin-icon { animation: spin 0.8s linear infinite; }
@keyframes spin { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }

.bookmarks-grid {
  display: grid; grid-template-columns: repeat(5, 1fr); gap: 14px;
}
.bm-card {
  padding: 14px 16px; display: flex; align-items: center; gap: 12px;
  cursor: pointer; position: relative;
}
.bm-icon-box {
  width: 36px; height: 36px; border-radius: var(--radius-sm);
  background: var(--strip-bg); border: 1px solid var(--line);
  display: flex; align-items: center; justify-content: center; flex-shrink: 0;
}
.bm-info { flex: 1; min-width: 0; display: flex; flex-direction: column; }
.bm-title { font-size: 13px; font-weight: 600; color: var(--ink); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.bm-desc { font-size: 11px; color: var(--faint); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.bm-arrow { color: var(--faint); transition: transform 0.2s ease; }
.bm-card:hover .bm-arrow { transform: translate(2px, -2px); color: var(--brand-light); }

.frame-card { padding: 0; overflow: hidden; }
.frame-bar {
  padding: 10px 18px; background: var(--strip-bg); border-bottom: 1px solid var(--line);
  display: flex; align-items: center; justify-content: space-between; font-size: 12px;
}
.frame-dots { display: flex; gap: 6px; }
.f-dot { width: 10px; height: 10px; border-radius: 50%; }
.f-dot.red { background: #ef4444; }
.f-dot.yellow { background: #f59e0b; }
.f-dot.green { background: #10b981; }

.frame-address {
  display: flex; align-items: center; gap: 6px; color: var(--muted);
  background: var(--input-bg); border: 1px solid var(--line);
  border-radius: var(--radius-pill); padding: 3px 14px; font-size: 11.5px;
}
.frame-status { display: flex; align-items: center; gap: 6px; color: var(--ok); font-size: 11.5px; }

.frame-viewport {
  min-height: 480px; padding: 40px 24px;
  display: flex; align-items: center; justify-content: center;
}

.mock-sunpanel-content {
  max-width: 760px; width: 100%; display: flex; flex-direction: column; align-items: center; text-align: center;
}
.sun-hero { margin-bottom: 32px; }
.sun-logo-glow {
  width: 72px; height: 72px; border-radius: 20px;
  background: var(--brand-gradient); margin: 0 auto 16px auto;
  display: flex; align-items: center; justify-content: center;
  box-shadow: var(--brand-glow); color: #fff;
}
.sun-heading { font-size: 22px; font-weight: 700; color: var(--ink); }
.sun-tagline { font-size: 13px; color: var(--muted); margin-top: 4px; }

.sun-category-title {
  font-size: 12px; font-weight: 700; text-transform: uppercase;
  color: var(--faint); letter-spacing: 0.8px; margin-bottom: 16px;
}

.sun-cards-row {
  display: grid; grid-template-columns: repeat(3, 1fr); gap: 16px; width: 100%;
}
.sun-card-tile {
  background: var(--strip-bg); border: 1px solid var(--line);
  border-radius: var(--radius-card); padding: 22px 18px;
  cursor: pointer; transition: var(--transition-smooth);
}
.sun-card-tile:hover {
  background: var(--card-hover-bg); border-color: var(--card-border-hover);
  transform: translateY(-2px);
}
.tile-icon { color: var(--brand-light); margin-bottom: 10px; }
.tile-title { font-size: 14px; font-weight: 600; color: var(--ink); }
.tile-sub { font-size: 11.5px; color: var(--faint); margin-top: 4px; }

@media (max-width: 1200px) {
  .bookmarks-grid { grid-template-columns: repeat(3, 1fr); }
}
@media (max-width: 800px) {
  .bookmarks-grid { grid-template-columns: 1fr; }
  .sun-cards-row { grid-template-columns: 1fr; }
}
</style>
