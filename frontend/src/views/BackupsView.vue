<template>
  <div class="backups-page animate-fade-in">
    <div class="page-topbar">
      <div>
        <h1 class="page-title">系统快照与数据备份</h1>
        <p class="page-subtitle">一键导出反向代理配置、SSL 证书私钥与 WAF 拦截规则，支持 WebDAV 异地容灾同步</p>
      </div>
      <div class="header-actions">
        <button class="btn btn-primary" @click="createSnapshot" :disabled="isCreating">
          <Plus :size="15" />
          {{ isCreating ? '快照打包中...' : '立即创建快照' }}
        </button>
      </div>
    </div>

    <!-- WebDAV Sync & Storage Status Bento Grid -->
    <div class="backup-top-grid">
      <!-- WebDAV Cloud Sync Card -->
      <SpotlightCard class="webdav-card">
        <div class="card-title-bar">
          <div class="title-with-icon">
            <Cloud :size="18" class="brand-text" />
            <span class="card-title">WebDAV 异地云端备份</span>
          </div>
          <span class="badge badge-ok">连接正常</span>
        </div>

        <div class="webdav-form">
          <div class="form-group">
            <label class="form-label">WebDAV 服务器端点</label>
            <input v-model="settings.webdavUrl" type="text" class="form-input mono-text" />
          </div>
          <div class="form-row-2">
            <div class="form-group">
              <label class="form-label">账号 / 用户名</label>
              <input type="text" value="gate_sync_nas" class="form-input" />
            </div>
            <div class="form-group">
              <label class="form-label">密码 / 授权 Token</label>
              <input type="password" value="••••••••••••••••" class="form-input" />
            </div>
          </div>
          <div class="sync-actions-row">
            <label class="checkbox-label">
              <input type="checkbox" v-model="settings.autoBackupEnabled" />
              <span>每天凌晨 03:00 自动加密同步到 WebDAV</span>
            </label>
            <button class="btn btn-secondary btn-sm" @click="testWebdav">
              测试连通性
            </button>
          </div>
        </div>
      </SpotlightCard>

      <!-- Storage Quota Card -->
      <SpotlightCard class="quota-card">
        <div class="card-title-bar">
          <span class="card-title">快照存储池空间</span>
          <span class="badge badge-neutral">本地存储</span>
        </div>

        <div class="quota-val-box">
          <div class="quota-num mono-text">41.7 <span class="unit">MB</span></div>
          <div class="quota-sub">上限配额: 500 MB (已用 8.3%)</div>
        </div>

        <div class="progress-track">
          <div class="progress-fill ok" style="width: 8.3%;" />
        </div>

        <div class="quota-tips">
          <span>系统将保留最近 7 份快照，超出后将自动按时间滚动轮转清理</span>
        </div>
      </SpotlightCard>
    </div>

    <!-- Snapshots Table Card -->
    <SpotlightCard class="table-card">
      <div class="table-responsive">
        <table class="backup-table">
          <thead>
            <tr>
              <th>快照归档名称</th>
              <th>大小</th>
              <th>生成类型</th>
              <th>备份目的地</th>
              <th>创建时间</th>
              <th class="text-right">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="bk in backups" :key="bk.id">
              <td class="name-cell">
                <Archive :size="16" class="archive-icon" />
                <span class="mono-text">{{ bk.name }}</span>
              </td>
              <td class="mono-text size-cell">{{ bk.size }}</td>
              <td>
                <span class="badge" :class="bk.type === 'AUTO' ? 'badge-brand' : 'badge-neutral'">
                  {{ bk.type === 'AUTO' ? '自动定时' : '手动快照' }}
                </span>
              </td>
              <td class="dest-cell">{{ bk.storage }}</td>
              <td class="mono-text date-cell">{{ bk.createdAt }}</td>
              <td class="text-right">
                <button class="btn btn-ghost btn-sm" @click="downloadSnapshot(bk)">
                  <Download :size="13" /> 下载
                </button>
                <button class="btn btn-ghost btn-sm" @click="restoreSnapshot(bk)">
                  <RotateCcw :size="13" /> 回滚
                </button>
                <button class="btn btn-ghost btn-sm text-bad" @click="deleteSnapshot(bk)">
                  <Trash2 :size="13" />
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </SpotlightCard>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { Plus, Cloud, Archive, Download, RotateCcw, Trash2 } from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import { useAppStore } from '@/composables/useAppStore'
import { useToast } from '@/composables/useToast'

const { backups, settings } = useAppStore()
const { success, info } = useToast()

const isCreating = ref(false)

const createSnapshot = () => {
  isCreating.value = true
  setTimeout(() => {
    isCreating.value = false
    const now = new Date()
    const ts = now.toISOString().slice(0, 10).replace(/-/g, '')
    backups.value.unshift({
      id: `bk_${Date.now()}`,
      name: `gatehome_manual_snapshot_${ts}.tar.gz`,
      size: '14.5 MB',
      type: 'MANUAL',
      storage: 'Local + WebDAV',
      createdAt: '刚刚'
    })
    success('系统快照已生成，配置与证书私钥打包完毕')
  }, 900)
}

const testWebdav = () => {
  success('WebDAV 服务器握手成功，写入权限验证通过')
}

const downloadSnapshot = (bk) => {
  info(`准备下载快照归档: ${bk.name}`)
}

const restoreSnapshot = (bk) => {
  if (confirm(`确定要从快照 "${bk.name}" 回滚系统配置吗？当前未保存的更改将被覆盖`)) {
    success(`已成功应用快照 "${bk.name}"，网关已重新加载配置`)
  }
}

const deleteSnapshot = (bk) => {
  if (confirm(`确定要删除快照 "${bk.name}" 吗？`)) {
    backups.value = backups.value.filter(b => b.id !== bk.id)
    info(`已删除快照: ${bk.name}`)
  }
}
</script>

<style scoped>
.backups-page { display: flex; flex-direction: column; gap: 20px; }
.page-topbar { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 16px; }
.page-title { font-size: 22px; font-weight: 700; color: var(--ink); }
.page-subtitle { font-size: 13px; color: var(--muted); margin-top: 3px; }

.backup-top-grid { display: grid; grid-template-columns: 2fr 1fr; gap: 16px; }
.webdav-card, .quota-card { padding: 20px; display: flex; flex-direction: column; justify-content: space-between; }
.card-title-bar { display: flex; align-items: center; justify-content: space-between; margin-bottom: 14px; }
.card-title { font-size: 14px; font-weight: 700; color: var(--ink); }
.title-with-icon { display: flex; align-items: center; gap: 8px; }
.brand-text { color: var(--brand-light); }

.webdav-form { display: flex; flex-direction: column; gap: 12px; }
.form-group { display: flex; flex-direction: column; gap: 5px; }
.form-label { font-size: 12px; font-weight: 600; color: var(--ink-secondary); }
.form-row-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }

.sync-actions-row { display: flex; align-items: center; justify-content: space-between; padding-top: 6px; }
.checkbox-label { display: flex; align-items: center; gap: 8px; font-size: 12px; color: var(--ink-secondary); cursor: pointer; }

.quota-val-box { margin: 10px 0 14px 0; }
.quota-num { font-size: 26px; font-weight: 700; color: var(--ink); }
.quota-num .unit { font-size: 14px; font-weight: 500; color: var(--faint); }
.quota-sub { font-size: 11.5px; color: var(--muted); margin-top: 2px; }

.progress-track { height: 6px; background: var(--strip-bg); border-radius: 999px; overflow: hidden; margin-bottom: 14px; }
.progress-fill.ok { height: 100%; background: var(--ok); border-radius: 999px; }
.quota-tips { font-size: 11px; color: var(--faint); line-height: 1.5; }

.table-card { padding: 0; overflow: hidden; }
.table-responsive { width: 100%; overflow-x: auto; }
.backup-table { width: 100%; border-collapse: collapse; text-align: left; font-size: 12.5px; }
.backup-table th {
  padding: 12px 18px; font-weight: 600; color: var(--faint);
  border-bottom: 1px solid var(--line); background: var(--strip-bg);
  white-space: nowrap; font-size: 11.5px;
}
.backup-table td {
  padding: 12px 18px; border-bottom: 1px solid var(--line);
  color: var(--ink-secondary); vertical-align: middle;
}
.backup-table tr:hover td { background: rgba(148, 163, 184, 0.05); }

.name-cell { display: flex; align-items: center; gap: 8px; font-weight: 600; color: var(--ink); }
.archive-icon { color: var(--brand-light); }
.size-cell { color: var(--muted); }
.dest-cell { color: var(--faint); }
.date-cell { color: var(--faint); font-size: 11px; }
.text-right { text-align: right; }
.text-bad { color: var(--bad); }

@media (max-width: 900px) {
  .backup-top-grid { grid-template-columns: 1fr; }
  .form-row-2 { grid-template-columns: 1fr; }
  .sync-actions-row { flex-direction: column; align-items: flex-start; gap: 10px; }
}
</style>
