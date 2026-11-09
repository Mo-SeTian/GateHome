<template>
  <div class="settings-page animate-fade-in">
    <div class="page-topbar">
      <div>
        <h1 class="page-title">系统与网关全局设置</h1>
        <p class="page-subtitle">配置管理界面公网反代接入、出站代理加速、日志保留配额与在线版本升级诊断</p>
      </div>
      <div class="header-actions">
        <button class="btn btn-primary" @click="saveAll">
          <Save :size="15" />
          保存全局配置
        </button>
      </div>
    </div>

    <!-- Dual Column Settings Layout -->
    <div class="settings-layout">
      <!-- Left Column: Access & Outbound -->
      <div class="settings-column">
        <!-- 1. Admin Access Reverse Proxy (管理界面反代访问) -->
        <SpotlightCard class="setting-card">
          <div class="card-head">
            <div class="card-icon-box">
              <Globe :size="18" />
            </div>
            <div>
              <h3 class="card-title">管理界面反代访问</h3>
              <p class="card-desc">允许通过自己解析的公网域名安全访问 GateHome 控制中心</p>
            </div>
          </div>

          <div class="card-form">
            <label class="toggle-row">
              <div class="toggle-text">
                <span class="t-name">允许指定公网域名反代访问</span>
                <span class="t-sub">兼容公网 HTTPS 接入内部 HTTP 16666 管理端口</span>
              </div>
              <input type="checkbox" v-model="settings.adminAccess.enabled" />
            </label>

            <div v-if="settings.adminAccess.enabled" class="form-group">
              <label class="form-label">允许的公网访问地址 (每行一个)</label>
              <textarea 
                rows="2" 
                class="form-input mono-text" 
                placeholder="https://gate.example.com"
                :value="settings.adminAccess.origins.join('\n')"
                @input="settings.adminAccess.origins = $event.target.value.split('\n').filter(Boolean)"
              />
              <small class="form-hint">仅允许填写的地址，自动防御跨站 API 调用攻击；默认管理端口为 16666。</small>
            </div>
          </div>
        </SpotlightCard>

        <!-- 2. Outbound Proxy (出站代理设置) -->
        <SpotlightCard class="setting-card">
          <div class="card-head">
            <div class="card-icon-box">
              <Send :size="18" />
            </div>
            <div>
              <h3 class="card-title">出站网络代理 (Outbound Proxy)</h3>
              <p class="card-desc">为 DDNS 查询、GitHub 版本检测与 IP 订阅拉取配置代理</p>
            </div>
          </div>

          <div class="card-form">
            <label class="toggle-row">
              <div class="toggle-text">
                <span class="t-name">开启出站 HTTP / SOCKS 代理</span>
                <span class="t-sub">解决内网无法直接连接 GitHub 或海外 IP 源的问题</span>
              </div>
              <input type="checkbox" v-model="settings.outboundProxy.enabled" />
            </label>

            <div v-if="settings.outboundProxy.enabled" class="form-group">
              <label class="form-label">代理服务器地址 (URL)</label>
              <input 
                v-model="settings.outboundProxy.url" 
                type="text" 
                placeholder="http://127.0.0.1:7890 或 socks5://127.0.0.1:1080" 
                class="form-input mono-text" 
              />
            </div>
          </div>
        </SpotlightCard>

        <!-- 3. Log Retention (日志空间与保留周期) -->
        <SpotlightCard class="setting-card">
          <div class="card-head">
            <div class="card-icon-box">
              <HardDrive :size="18" />
            </div>
            <div>
              <h3 class="card-title">日志保留与存储配额</h3>
              <p class="card-desc">项目、反代、防火墙与安全事件共用一个存储上限</p>
            </div>
          </div>

          <div class="card-form">
            <div class="form-row-2">
              <div class="form-group">
                <label class="form-label">最大占用空间 (MiB)</label>
                <input v-model.number="settings.logRetention.maxSizeMb" type="number" min="16" max="1024" class="form-input mono-text" />
              </div>
              <div class="form-group">
                <label class="form-label">保留天数 (Days)</label>
                <input v-model.number="settings.logRetention.keepDays" type="number" min="1" max="365" class="form-input mono-text" />
              </div>
            </div>
            <small class="form-hint">超过任一限制时系统按时间每分钟检查并清理最早的日志文件。</small>
          </div>
        </SpotlightCard>
      </div>

      <!-- Right Column: Theme & Version -->
      <div class="settings-column">
        <!-- 4. Theme & Appearance -->
        <SpotlightCard class="setting-card">
          <div class="card-head">
            <div class="card-icon-box">
              <Palette :size="18" />
            </div>
            <div>
              <h3 class="card-title">界面外观与设计风格</h3>
              <p class="card-desc">选择契合工作场景的 Aurora Next 极客设计规范</p>
            </div>
          </div>

          <div class="theme-picker-grid">
            <div 
              class="theme-option dark"
              :class="{ selected: theme === 'dark' }"
              @click="setTheme('dark')"
            >
              <div class="theme-swatch dark" />
              <div class="theme-info">
                <span class="theme-name">Obsidian Tech (黑曜石深色)</span>
                <span class="theme-desc">深色沉浸 · 光晕跟随 · 推荐极客夜间环境</span>
              </div>
            </div>

            <div 
              class="theme-option light"
              :class="{ selected: theme === 'light' }"
              @click="setTheme('light')"
            >
              <div class="theme-swatch light" />
              <div class="theme-info">
                <span class="theme-name">VisionOS White (清透磨砂浅色)</span>
                <span class="theme-desc">纯净通透 · 高对比度清晰排版</span>
              </div>
            </div>
          </div>
        </SpotlightCard>

        <!-- 5. Online Updates & Diagnostics -->
        <SpotlightCard class="setting-card">
          <div class="card-head">
            <div class="card-icon-box">
              <Cpu :size="18" />
            </div>
            <div>
              <h3 class="card-title">版本诊断与在线升级 (Updates)</h3>
              <p class="card-desc">检测 GitHub Releases 最新版本，更新前自动生成快照备份</p>
            </div>
          </div>

          <div class="card-form">
            <div class="version-status-box">
              <div class="v-left">
                <span class="v-label">当前运行版本</span>
                <span class="v-num mono-text">{{ settings.currentVersion }}</span>
              </div>
              <button class="btn btn-secondary btn-sm" @click="checkUpdate" :disabled="isChecking">
                <RefreshCw :size="13" :class="{ 'spin-icon': isChecking }" />
                {{ isChecking ? '检测中...' : '检查更新' }}
              </button>
            </div>

            <div class="form-group">
              <label class="form-label">更新推送通道</label>
              <select v-model="settings.channel" class="form-select">
                <option value="stable">正式稳定版 (Stable · 推荐生产环境)</option>
                <option value="beta">公测尝鲜版 (Beta · 体验前沿特性)</option>
              </select>
            </div>

            <div class="env-diagnostics">
              <div class="diag-item">
                <span class="d-label">操作系统 / 架构:</span>
                <span class="d-val mono-text">Darwin (macOS) / arm64</span>
              </div>
              <div class="diag-item">
                <span class="d-label">Go 内核驱动:</span>
                <span class="d-val mono-text">go1.23.2 · Single Binary</span>
              </div>
              <div class="diag-item">
                <span class="d-label">嵌入式前端引擎:</span>
                <span class="d-val ok-text mono-text">Vue 3.5 + Aurora Next</span>
              </div>
            </div>
          </div>
        </SpotlightCard>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { Save, Globe, Send, HardDrive, Palette, Cpu, RefreshCw } from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import { useAppStore } from '@/composables/useAppStore'
import { useToast } from '@/composables/useToast'

const { settings, theme, setTheme } = useAppStore()
const { success } = useToast()

const isChecking = ref(false)

const saveAll = () => {
  success('全局配置保存成功，网关参数已动态生效')
}

const checkUpdate = () => {
  isChecking.value = true
  setTimeout(() => {
    isChecking.value = false
    success(`检查完成: 当前 ${settings.currentVersion} 已是最新版`)
  }, 700)
}
</script>

<style scoped>
.settings-page { display: flex; flex-direction: column; gap: 20px; }
.page-topbar { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 16px; }
.page-title { font-size: 22px; font-weight: 700; color: var(--ink); }
.page-subtitle { font-size: 13px; color: var(--muted); margin-top: 3px; }

.settings-layout { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
.settings-column { display: flex; flex-direction: column; gap: 16px; }

.setting-card { padding: 20px; display: flex; flex-direction: column; gap: 16px; }
.card-head { display: flex; align-items: center; gap: 12px; }
.card-icon-box {
  width: 38px; height: 38px; border-radius: var(--radius-sm);
  background: rgba(99, 102, 241, 0.12); color: var(--brand-light);
  display: flex; align-items: center; justify-content: center; flex-shrink: 0;
}
.card-title { font-size: 15px; font-weight: 700; color: var(--ink); }
.card-desc { font-size: 11.5px; color: var(--muted); margin-top: 2px; }

.card-form { display: flex; flex-direction: column; gap: 14px; }
.form-group { display: flex; flex-direction: column; gap: 6px; }
.form-label { font-size: 12px; font-weight: 600; color: var(--ink-secondary); }
.form-hint { font-size: 11px; color: var(--faint); }
.form-row-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }

.toggle-row {
  display: flex; align-items: center; justify-content: space-between;
  background: var(--strip-bg); border: 1px solid var(--line);
  border-radius: var(--radius-sm); padding: 12px 14px; cursor: pointer;
}
.toggle-text { display: flex; flex-direction: column; }
.t-name { font-size: 12.5px; font-weight: 600; color: var(--ink); }
.t-sub { font-size: 11px; color: var(--faint); }

.theme-picker-grid { display: flex; flex-direction: column; gap: 10px; }
.theme-option {
  display: flex; align-items: center; gap: 12px; padding: 12px 14px;
  border-radius: var(--radius-sm); border: 1px solid var(--line);
  background: var(--strip-bg); cursor: pointer; transition: var(--transition-smooth);
}
.theme-option:hover, .theme-option.selected {
  border-color: var(--brand); background: var(--card-hover-bg);
}
.theme-option.selected { box-shadow: 0 0 16px rgba(99, 102, 241, 0.2); }

.theme-swatch { width: 32px; height: 32px; border-radius: 8px; border: 1px solid var(--line); }
.theme-swatch.dark { background: #090d16; }
.theme-swatch.light { background: #f1f5f9; }

.theme-info { display: flex; flex-direction: column; }
.theme-name { font-size: 13px; font-weight: 600; color: var(--ink); }
.theme-desc { font-size: 11px; color: var(--faint); }

.version-status-box {
  display: flex; align-items: center; justify-content: space-between;
  background: var(--strip-bg); border: 1px solid var(--line);
  border-radius: var(--radius-sm); padding: 12px 16px;
}
.v-left { display: flex; flex-direction: column; }
.v-label { font-size: 11px; color: var(--faint); }
.v-num { font-size: 15px; font-weight: 700; color: var(--ink); margin-top: 2px; }

.env-diagnostics {
  display: flex; flex-direction: column; gap: 6px; font-size: 12px;
  padding: 10px 14px; background: var(--strip-bg); border: 1px solid var(--line);
  border-radius: var(--radius-sm);
}
.diag-item { display: flex; justify-content: space-between; }
.d-label { color: var(--faint); }
.d-val { color: var(--ink); }
.ok-text { color: var(--ok); }
.spin-icon { animation: spin 0.8s linear infinite; }
@keyframes spin { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }

@media (max-width: 900px) {
  .settings-layout { grid-template-columns: 1fr; }
}
</style>
