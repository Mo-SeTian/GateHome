<template>
  <div class="firewall-page animate-fade-in">
    <div class="page-topbar">
      <div>
        <h1 class="page-title">防火墙与 WAF 攻击防护</h1>
        <p class="page-subtitle">配置来源 IP / CIDR 访问规则组（联动 IP 订阅），集成 Coraza · OWASP CRS 4.25.0 智能应用防火墙</p>
      </div>
      <div class="header-actions">
        <button class="btn btn-secondary" @click="$router.push('/firewall-logs')">
          <FileSearch :size="14" />
          拦截审计记录
        </button>
        <button class="btn btn-primary" @click="openCreateFwModal">
          <Plus :size="15" />
          添加防火墙策略
        </button>
      </div>
    </div>

    <!-- Active Firewalls Strategy Selector -->
    <div class="strategy-selector-bar">
      <div class="strategy-tabs">
        <button 
          v-for="fw in firewalls" 
          :key="fw.id"
          class="strategy-tab-btn"
          :class="{ active: currentFwId === fw.id }"
          @click="currentFwId = fw.id"
        >
          <Shield :size="15" />
          {{ fw.name }}
          <span class="badge" :class="fw.defaultAction === 'deny' ? 'badge-bad' : 'badge-ok'">
            未命中: {{ fw.defaultAction === 'deny' ? '禁止' : '允许' }}
          </span>
        </button>
      </div>
    </div>

    <!-- Strategy Detail Section -->
    <div v-if="activeFw" class="firewall-main-grid">
      <!-- 1. Left: IP Rules Groups & Referenced Subscriptions -->
      <div class="fw-column">
        <SpotlightCard class="panel-card">
          <div class="panel-head">
            <div class="head-left">
              <Layers :size="18" class="brand-text" />
              <div>
                <h3 class="panel-title">IP 访问规则组 (从上到下按序匹配)</h3>
                <p class="panel-desc">第一条命中即决定允许或禁止；手动 IP 与引用的订阅自动合并</p>
              </div>
            </div>
            <button class="btn btn-secondary btn-sm" @click="addGroupToActiveFw">
              <Plus :size="12" /> 添加 IP 组
            </button>
          </div>

          <!-- Groups List -->
          <div class="groups-list">
            <div 
              v-for="(grp, idx) in activeFw.groups" 
              :key="grp.name"
              class="group-card-item"
            >
              <div class="grp-top">
                <div class="grp-title-row">
                  <span class="grp-index mono-text">#{{ idx + 1 }}</span>
                  <span class="grp-name">{{ grp.name }}</span>
                </div>
                <div class="grp-badges">
                  <span class="badge" :class="grp.match === 'include' ? 'badge-brand' : 'badge-neutral'">
                    {{ grp.match === 'include' ? '包含 (组内)' : '排除 (组外)' }}
                  </span>
                  <span class="badge" :class="grp.action === 'allow' ? 'badge-ok' : 'badge-bad'">
                    命中: {{ grp.action === 'allow' ? '允许访问' : '禁止访问' }}
                  </span>
                </div>
              </div>

              <!-- Manual CIDRs -->
              <div v-if="grp.cidrs.length" class="grp-cidrs-box">
                <span class="box-label">手动 IP / CIDR:</span>
                <span class="mono-text cidrs-text">{{ grp.cidrs.join(', ') }}</span>
              </div>

              <!-- Referenced Subscriptions -->
              <div v-if="grp.subscriptions.length" class="grp-subs-box">
                <span class="box-label">引用的 IP 规则订阅:</span>
                <div class="sub-tags">
                  <span 
                    v-for="subId in grp.subscriptions" 
                    :key="subId"
                    class="badge badge-brand sub-ref-tag"
                  >
                    <Download :size="10" /> {{ getSubName(subId) }}
                  </span>
                </div>
              </div>
            </div>
          </div>
        </SpotlightCard>
      </div>

      <!-- 2. Right: Coraza WAF Web Protection -->
      <div class="fw-column">
        <SpotlightCard class="panel-card">
          <div class="panel-head">
            <div class="head-left">
              <ShieldAlert :size="18" class="brand-text" />
              <div>
                <h3 class="panel-title">Web 攻击防护 (Coraza 3.8.1 · OWASP CRS)</h3>
                <p class="panel-desc">深度防护 SQLi、XSS、RCE 及探测攻击；响应及 WebSocket 保持直通</p>
              </div>
            </div>
            <span class="badge" :class="activeFw.protection.wafMode === 'block' ? 'badge-ok' : activeFw.protection.wafMode === 'detect' ? 'badge-warn' : 'badge-neutral'">
              {{ activeFw.protection.wafMode === 'block' ? '拦截攻击' : activeFw.protection.wafMode === 'detect' ? '仅记录' : '已关闭' }}
            </span>
          </div>

          <div class="waf-config-form">
            <div class="form-row-2">
              <div class="form-group">
                <label class="form-label">WAF 运行模式</label>
                <select v-model="activeFw.protection.wafMode" class="form-select">
                  <option value="block">拦截攻击 (生产模式)</option>
                  <option value="detect">仅记录不拦截 (调优观察)</option>
                  <option value="off">关闭 WAF 防护</option>
                </select>
              </div>
              <div class="form-group">
                <label class="form-label">检测等级 (Paranoia Level)</label>
                <select v-model="activeFw.protection.wafLevel" class="form-select">
                  <option :value="1">PL1 · 基础 (推荐 · 极低误报)</option>
                  <option :value="2">PL2 · 增强 (防御进阶混淆)</option>
                  <option :value="3">PL3 · 严格 (高安全场景)</option>
                  <option :value="4">PL4 · 极严格 (需专项豁免)</option>
                </select>
              </div>
            </div>

            <div class="form-group">
              <label class="form-label">请求体检查上限 (KiB)</label>
              <input 
                v-model.number="activeFw.protection.wafBodyKib" 
                type="number" 
                min="64" 
                max="32768" 
                class="form-input mono-text" 
              />
              <small class="form-hint">拦截模式超限返回 413；上传大文件服务可适当调高或配置例外。</small>
            </div>

            <!-- WAF Exceptions -->
            <div class="waf-exceptions-box">
              <div class="ex-head">
                <span class="ex-title">规则例外与豁免名单 ({{ activeFw.protection.exceptions.length }})</span>
                <button class="btn btn-ghost btn-sm text-brand" @click="addWafException">
                  <Plus :size="12" /> 添加例外
                </button>
              </div>
              <div v-for="(ex, i) in activeFw.protection.exceptions" :key="i" class="ex-item">
                <div class="ex-info">
                  <span class="badge badge-neutral mono-text">CRS #{{ ex.ruleId }}</span>
                  <span class="mono-text">{{ ex.host || '全部域名' }}</span>
                  <span class="ex-path mono-text">{{ ex.path || '全部路径' }}</span>
                </div>
                <button class="btn btn-ghost btn-sm text-bad" @click="removeWafException(i)">
                  <X :size="12" />
                </button>
              </div>
            </div>

            <div class="panel-save-row">
              <button class="btn btn-primary btn-sm" @click="saveFwChanges">
                <Save :size="13" /> 保存防火墙配置
              </button>
            </div>
          </div>
        </SpotlightCard>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'
import { FileSearch, Plus, Shield, Layers, ShieldAlert, Download, Save, X } from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import { useAppStore } from '@/composables/useAppStore'
import { useToast } from '@/composables/useToast'

const { firewalls, subscriptions } = useAppStore()
const { success } = useToast()

const currentFwId = ref(firewalls.value[0]?.id || '')

const activeFw = computed(() => {
  return firewalls.value.find(f => f.id === currentFwId.value)
})

const getSubName = (subId) => {
  const sub = subscriptions.value.find(s => s.id === subId)
  return sub ? `${sub.name} (${sub.count.toLocaleString()}条)` : subId
}

const addGroupToActiveFw = () => {
  if (!activeFw.value) return
  activeFw.value.groups.push({
    name: '新建受信任网段组',
    match: 'include',
    action: 'allow',
    cidrs: ['10.0.0.0/8'],
    subscriptions: ['sub_cn_v4']
  })
  success('已在当前策略中增加规则组')
}

const addWafException = () => {
  if (!activeFw.value) return
  activeFw.value.protection.exceptions.push({
    ruleId: 941100,
    host: '',
    path: '/api/upload'
  })
  success('已添加 CRS 规则例外')
}

const removeWafException = (idx) => {
  if (!activeFw.value) return
  activeFw.value.protection.exceptions.splice(idx, 1)
}

const saveFwChanges = () => {
  success(`防火墙策略 "${activeFw.value?.name}" 已更新，规则已热重载生效`)
}

const openCreateFwModal = () => {
  const newId = `fw_${Date.now()}`
  firewalls.value.push({
    id: newId,
    name: `自定义安全策略 #${firewalls.value.length + 1}`,
    defaultAction: 'deny',
    groups: [
      { name: '局域网直通组', match: 'include', action: 'allow', cidrs: ['192.168.0.0/16'], subscriptions: [] }
    ],
    protection: {
      wafMode: 'block',
      wafLevel: 1,
      wafBodyKib: 1024,
      exceptions: []
    }
  })
  currentFwId.value = newId
  success('已建立新防火墙安全策略')
}
</script>

<style scoped>
.firewall-page { display: flex; flex-direction: column; gap: 20px; }
.page-topbar { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 16px; }
.page-title { font-size: 22px; font-weight: 700; color: var(--ink); }
.page-subtitle { font-size: 13px; color: var(--muted); margin-top: 3px; }
.header-actions { display: flex; gap: 10px; }

/* Strategy Selector Bar */
.strategy-selector-bar {
  border-bottom: 1px solid var(--line); padding-bottom: 12px;
}
.strategy-tabs { display: flex; gap: 8px; flex-wrap: wrap; }
.strategy-tab-btn {
  display: flex; align-items: center; gap: 8px; padding: 7px 14px;
  border-radius: var(--radius-sm); background: var(--strip-bg);
  border: 1px solid var(--line); color: var(--muted); font-size: 13px;
  font-weight: 500; transition: var(--transition-smooth);
}
.strategy-tab-btn:hover { background: var(--card-hover-bg); color: var(--ink); }
.strategy-tab-btn.active {
  background: rgba(99, 102, 241, 0.18); border-color: rgba(99, 102, 241, 0.4);
  color: #fff; font-weight: 600;
}
.theme-light .strategy-tab-btn.active { color: var(--brand); }

/* Main Grid */
.firewall-main-grid {
  display: grid; grid-template-columns: 1.1fr 1fr; gap: 16px;
}
.fw-column { display: flex; flex-direction: column; }
.panel-card { padding: 20px; display: flex; flex-direction: column; gap: 16px; height: 100%; }

.panel-head { display: flex; align-items: center; justify-content: space-between; }
.head-left { display: flex; align-items: center; gap: 10px; }
.brand-text { color: var(--brand-light); }
.panel-title { font-size: 14.5px; font-weight: 700; color: var(--ink); }
.panel-desc { font-size: 11.5px; color: var(--muted); margin-top: 2px; }

/* Groups List */
.groups-list { display: flex; flex-direction: column; gap: 12px; }
.group-card-item {
  background: var(--strip-bg); border: 1px solid var(--line);
  border-radius: var(--radius-sm); padding: 14px; display: flex; flex-direction: column; gap: 10px;
}
.grp-top { display: flex; align-items: center; justify-content: space-between; }
.grp-title-row { display: flex; align-items: center; gap: 8px; }
.grp-index { color: var(--brand-cyan); font-weight: 700; font-size: 13px; }
.grp-name { font-size: 13.5px; font-weight: 600; color: var(--ink); }
.grp-badges { display: flex; gap: 6px; }

.grp-cidrs-box, .grp-subs-box {
  display: flex; flex-direction: column; gap: 4px; font-size: 12px;
}
.box-label { font-size: 11px; color: var(--faint); font-weight: 500; }
.cidrs-text { color: var(--ink-secondary); font-size: 11.5px; }

.sub-tags { display: flex; gap: 6px; flex-wrap: wrap; }
.sub-ref-tag { display: inline-flex; align-items: center; gap: 4px; }

/* WAF Form */
.waf-config-form { display: flex; flex-direction: column; gap: 14px; }
.form-row-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.form-group { display: flex; flex-direction: column; gap: 6px; }
.form-label { font-size: 12px; font-weight: 600; color: var(--ink-secondary); }
.form-hint { font-size: 11px; color: var(--faint); }

.waf-exceptions-box {
  background: var(--strip-bg); border: 1px solid var(--line);
  border-radius: var(--radius-sm); padding: 12px 14px; display: flex; flex-direction: column; gap: 8px;
}
.ex-head { display: flex; align-items: center; justify-content: space-between; font-size: 12px; font-weight: 600; color: var(--ink); }
.ex-item {
  display: flex; align-items: center; justify-content: space-between; padding: 6px 8px;
  background: var(--card-bg); border: 1px solid var(--line); border-radius: 6px; font-size: 11.5px;
}
.ex-info { display: flex; align-items: center; gap: 8px; color: var(--ink-secondary); }
.ex-path { color: var(--brand-light); }
.text-brand { color: var(--brand-light) !important; }
.text-bad { color: var(--bad) !important; }

.panel-save-row { display: flex; justify-content: flex-end; padding-top: 8px; }

@media (max-width: 1000px) {
  .firewall-main-grid { grid-template-columns: 1fr; }
  .form-row-2 { grid-template-columns: 1fr; }
}
</style>
