<template>
  <div class="updates-page animate-fade-in">
    <div class="page-topbar">
      <div>
        <h1 class="page-title">在线更新与版本维护</h1>
        <p class="page-subtitle">选择正式版或公测 Beta 通道，更新前自动打包完整备份，随时可安全回滚历史版本</p>
      </div>
      <div class="header-actions">
        <button class="btn btn-primary" @click="checkOnlineUpdate" :disabled="isChecking">
          <RefreshCw :size="14" :class="{ 'spin-icon': isChecking }" />
          {{ isChecking ? '正在连接 GitHub 检测...' : '检查最新版本' }}
        </button>
      </div>
    </div>

    <!-- Main 2-Column Grid -->
    <div class="updates-grid">
      <!-- Left Column: Current Version & Updates -->
      <div class="update-col">
        <!-- 1. Version Card -->
        <SpotlightCard class="update-card">
          <div class="card-head">
            <div class="card-icon-box">
              <Download :size="18" />
            </div>
            <div class="card-title-group">
              <h3 class="card-title">版本通道与在线更新</h3>
              <p class="card-desc">自动检测 releases 归档，点击更新自动下载二进制替换并热重启</p>
            </div>
          </div>

          <div class="version-banner">
            <div class="v-left">
              <span class="v-label">当前运行版本</span>
              <span class="v-val mono-text">{{ settings.currentVersion }}</span>
            </div>
            <span class="badge badge-ok">已是最新稳定版</span>
          </div>

          <div class="channel-group">
            <label class="form-label">发布更新通道</label>
            <div class="channel-options">
              <label class="channel-pill" :class="{ active: settings.channel === 'stable' }">
                <input type="radio" value="stable" v-model="settings.channel" />
                <div class="pill-info">
                  <span class="pill-title">正式稳定版 (Stable)</span>
                  <span class="pill-desc">推荐生产环境，仅推送经过广泛验证的稳定构建</span>
                </div>
              </label>
              <label class="channel-pill" :class="{ active: settings.channel === 'beta' }">
                <input type="radio" value="beta" v-model="settings.channel" />
                <div class="pill-info">
                  <span class="pill-title">公测尝鲜版 (Beta)</span>
                  <span class="pill-desc">第一时间体验前沿特性、实验性 WAF 规则与性能调优</span>
                </div>
              </label>
            </div>
          </div>
        </SpotlightCard>

        <!-- 2. Update Backups Rollback History (更新前自动备份快照) -->
        <SpotlightCard class="update-card">
          <div class="card-head">
            <div class="card-icon-box">
              <RotateCcw :size="18" />
            </div>
            <div class="card-title-group">
              <h3 class="card-title">升级前自动快照与历史回滚</h3>
              <p class="card-desc">每次执行在线更新前网关自动生成的保护备份，可一键恢复</p>
            </div>
          </div>

          <div class="rollback-list">
            <div v-for="bk in updateBackups" :key="bk.id" class="rollback-item">
              <div class="bk-left">
                <Archive :size="16" class="bk-icon" />
                <div class="bk-info">
                  <span class="bk-name mono-text">{{ bk.name }}</span>
                  <span class="bk-meta">{{ bk.createdAt }} · 版本 {{ bk.version }} · {{ bk.size }}</span>
                </div>
              </div>
              <button class="btn btn-secondary btn-sm" @click="rollbackVersion(bk)">
                <RotateCcw :size="12" /> 回滚此版本
              </button>
            </div>
          </div>
        </SpotlightCard>
      </div>

      <!-- Right Column: GitHub Token & Capacity -->
      <div class="update-col">
        <!-- 3. GitHub Token -->
        <SpotlightCard class="update-card">
          <div class="card-head">
            <div class="card-icon-box">
              <Key :size="18" />
            </div>
            <div class="card-title-group">
              <h3 class="card-title">GitHub API 访问令牌 (可选)</h3>
              <p class="card-desc">配置个人 Token 以避免公共 IP 触发 GitHub API 速率限制 (403)</p>
            </div>
          </div>

          <div class="token-form">
            <div class="form-group">
              <label class="form-label">Personal Access Token (经典 / 细粒度)</label>
              <input type="password" placeholder="ghp_••••••••••••••••••••••••••••" class="form-input mono-text" />
            </div>
            <small class="form-hint">无需赋予任何额外权限，仅用于读取公开 Releases 版本列表。</small>
            <div class="form-actions-row">
              <button class="btn btn-secondary btn-sm" @click="saveGithubToken">保存令牌</button>
            </div>
          </div>
        </SpotlightCard>

        <!-- 4. Backup Capacity Pool -->
        <SpotlightCard class="update-card">
          <div class="card-head">
            <div class="card-icon-box">
              <HardDrive :size="18" />
            </div>
            <div class="card-title-group">
              <h3 class="card-title">升级快照存储池空间</h3>
              <p class="card-desc">超额后自动滚动清理最早生成的历史版本归档</p>
            </div>
          </div>

          <div class="pool-metrics">
            <div class="pool-val mono-text">28.1 <span class="unit">MB</span></div>
            <span class="pool-quota">存储上限: 256 MB (已占用 10.9%)</span>
          </div>

          <div class="progress-track">
            <div class="progress-fill brand" style="width: 10.9%;" />
          </div>
        </SpotlightCard>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { RefreshCw, Download, RotateCcw, Key, HardDrive, Archive } from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import { useAppStore } from '@/composables/useAppStore'
import { useToast } from '@/composables/useToast'

const { settings } = useAppStore()
const { success, info } = useToast()

const isChecking = ref(false)

const updateBackups = ref([
  { id: 'ubk_1', name: 'gatehouse_autobackup_v0.0.67-beta.2.zip', version: 'v0.0.67-beta.2', size: '13.9 MB', createdAt: '2026-11-08 19:40' },
  { id: 'ubk_2', name: 'gatehouse_autobackup_v0.0.66.zip', version: 'v0.0.66', size: '14.2 MB', createdAt: '2026-10-24 10:15' }
])

const checkOnlineUpdate = () => {
  isChecking.value = true
  setTimeout(() => {
    isChecking.value = false
    success(`检查完成: 当前 ${settings.currentVersion} 已经是最新版，暂无可用更新`)
  }, 800)
}

const rollbackVersion = (bk) => {
  if (confirm(`确定要从快照回滚至版本 "${bk.version}" 吗？系统将自动恢复配置文件和数据库状态`)) {
    success(`正在回滚至 ${bk.version}，服务正在热重载...`)
  }
}

const saveGithubToken = () => {
  success('GitHub API Token 已保存')
}
</script>

<style scoped>
.updates-page { display: flex; flex-direction: column; gap: 20px; }
.page-topbar { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 16px; }
.page-title { font-size: 22px; font-weight: 700; color: var(--ink); }
.page-subtitle { font-size: 13px; color: var(--muted); margin-top: 3px; }
.header-actions { display: flex; gap: 10px; }
.spin-icon { animation: spin 0.8s linear infinite; }
@keyframes spin { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }

.updates-grid { display: grid; grid-template-columns: 1.2fr 1fr; gap: 16px; }
.update-col { display: flex; flex-direction: column; gap: 16px; }
.update-card { padding: 20px; display: flex; flex-direction: column; gap: 16px; }

.card-head { display: flex; align-items: center; gap: 12px; }
.card-icon-box {
  width: 38px; height: 38px; border-radius: var(--radius-sm);
  background: rgba(99, 102, 241, 0.12); color: var(--brand-light);
  display: flex; align-items: center; justify-content: center; flex-shrink: 0;
}
.card-title-group { display: flex; flex-direction: column; }
.card-title { font-size: 14.5px; font-weight: 700; color: var(--ink); }
.card-desc { font-size: 11.5px; color: var(--muted); margin-top: 2px; }

.version-banner {
  display: flex; align-items: center; justify-content: space-between;
  background: var(--strip-bg); border: 1px solid var(--line);
  border-radius: var(--radius-sm); padding: 12px 16px;
}
.v-left { display: flex; flex-direction: column; }
.v-label { font-size: 11px; color: var(--faint); }
.v-val { font-size: 16px; font-weight: 700; color: var(--ink); margin-top: 2px; }

.channel-group { display: flex; flex-direction: column; gap: 8px; }
.channel-options { display: flex; flex-direction: column; gap: 8px; }
.channel-pill {
  display: flex; align-items: center; gap: 12px; padding: 12px 14px;
  border-radius: var(--radius-sm); border: 1px solid var(--line);
  background: var(--strip-bg); cursor: pointer; transition: var(--transition-smooth);
}
.channel-pill:hover, .channel-pill.active {
  border-color: var(--brand); background: var(--card-hover-bg);
}
.pill-info { display: flex; flex-direction: column; }
.pill-title { font-size: 13px; font-weight: 600; color: var(--ink); }
.pill-desc { font-size: 11px; color: var(--faint); }

/* Rollback list */
.rollback-list { display: flex; flex-direction: column; gap: 10px; }
.rollback-item {
  display: flex; align-items: center; justify-content: space-between;
  padding: 10px 12px; background: var(--strip-bg); border: 1px solid var(--line);
  border-radius: var(--radius-sm);
}
.bk-left { display: flex; align-items: center; gap: 10px; }
.bk-icon { color: var(--brand-light); }
.bk-info { display: flex; flex-direction: column; }
.bk-name { font-size: 12.5px; font-weight: 600; color: var(--ink); }
.bk-meta { font-size: 11px; color: var(--faint); margin-top: 2px; }

.token-form { display: flex; flex-direction: column; gap: 10px; }
.form-group { display: flex; flex-direction: column; gap: 6px; }
.form-label { font-size: 12px; font-weight: 600; color: var(--ink-secondary); }
.form-hint { font-size: 11px; color: var(--faint); }
.form-actions-row { display: flex; justify-content: flex-end; }

.pool-metrics { display: flex; flex-direction: column; gap: 2px; }
.pool-val { font-size: 26px; font-weight: 700; color: var(--ink); }
.pool-val .unit { font-size: 14px; font-weight: 500; color: var(--faint); }
.pool-quota { font-size: 11.5px; color: var(--muted); }
.progress-track { height: 6px; background: var(--strip-bg); border-radius: 999px; overflow: hidden; margin-top: 8px; }
.progress-fill.brand { height: 100%; background: var(--brand-gradient); border-radius: 999px; }

@media (max-width: 900px) {
  .updates-grid { grid-template-columns: 1fr; }
}
</style>
