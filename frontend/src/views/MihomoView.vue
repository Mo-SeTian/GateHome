<template>
  <div class="report-page mihomo-page animate-fade-in">
    <header class="report-heading">
      <h1>代理分流</h1>
      <div class="report-actions header-controls">
        <div class="mode-segment" role="group" aria-label="代理模式">
          <button
            v-for="m in modes"
            :key="m.id"
            type="button"
            class="mode-btn"
            :class="{ active: currentMode === m.id }"
            :disabled="busyAction"
            @click="switchMode(m.id)"
          >
            {{ m.label }}
          </button>
        </div>
        <button
          type="button"
          class="btn btn-secondary"
          :disabled="busyAction || !status?.running"
          @click="reloadCore"
        >
          <RefreshCw :size="14" /> 重载配置
        </button>
        <button
          type="button"
          class="btn btn-secondary"
          :disabled="busyAction || !status?.running"
          @click="restartCore"
        >
          <RotateCw :size="14" /> 重启内核
        </button>
        <button
          v-if="status?.running"
          type="button"
          class="btn btn-danger"
          :disabled="busyAction"
          @click="stopCore"
        >
          <Square :size="14" /> 停止
        </button>
        <button
          v-else
          type="button"
          class="btn btn-primary"
          :disabled="busyAction || loading"
          @click="startCore"
        >
          <Play :size="14" /> 启动
        </button>
      </div>
    </header>

    <p v-if="error" class="report-error" role="alert">{{ error }}</p>

    <!-- Compact Runtime Strip -->
    <SpotlightCard v-if="status" class="runtime-strip">
      <div class="strip-items">
        <span class="badge" :class="status.running ? 'badge-ok' : status.core_installed ? 'badge-neutral' : 'badge-warn'">
          <span class="status-dot" :class="status.running ? 'ok' : status.core_installed ? 'neutral' : 'warn'" />
          {{ status.running ? '运行中' : status.core_installed ? '已停止' : '未安装内核' }}
        </span>

        <span v-if="status.core_version" class="strip-chip mono-text">
          {{ shortVersion(status.core_version) }}
        </span>

        <button
          v-for="entry in activePortEntries"
          :key="entry.label"
          type="button"
          class="strip-chip port-copy-chip mono-text"
          :title="`点击复制 ${entry.address}`"
          @click="copyText(entry.address, `${entry.label} 地址已复制`)"
        >
          <span>{{ entry.label }} {{ entry.address }}</span>
          <Copy :size="12" aria-hidden="true" />
        </button>

        <span class="strip-chip" :class="{ 'chip-active': status.settings?.ipv6 }">
          IPv6 {{ status.settings?.ipv6 ? '开启' : '关闭' }}
        </span>

        <span class="strip-chip" :class="{ 'chip-brand': status.outbound_takeover_active }">
          出站联动 {{ status.outbound_takeover_active ? '接管中' : status.takeover_outbound ? '待内核启动' : '未启用' }}
        </span>
      </div>
    </SpotlightCard>

    <p v-if="status?.last_error" class="report-warning" role="status">{{ status.last_error }}</p>

    <!-- Main 4-Tab Bar -->
    <div class="mihomo-tabs-wrap">
      <div class="mihomo-tabs" role="tablist" aria-label="代理分流功能标签">
        <button
          v-for="tab in tabs"
          :key="tab.id"
          type="button"
          role="tab"
          :aria-selected="activeTab === tab.id"
          class="tab-btn"
          :class="{ active: activeTab === tab.id }"
          @click="switchTab(tab.id)"
        >
          <component :is="tab.icon" :size="15" aria-hidden="true" />
          <span>{{ tab.label }}</span>
        </button>
      </div>
    </div>

    <!-- Tab 1: Proxy Groups & Nodes -->
    <section v-show="activeTab === 'groups'" class="tab-section" role="tabpanel" aria-label="策略组与节点">
      <div class="section-toolbar">
        <div class="toolbar-meta">
          <span class="meta-pill">{{ groups.length }} 个策略组</span>
          <span class="meta-pill">{{ allNodes.length }} 个节点</span>
          <span v-if="groupsLive" class="badge badge-ok">实时控制</span>
          <span v-else class="badge badge-neutral">离线预览</span>
        </div>
        <div class="report-actions">
          <button type="button" class="btn btn-secondary btn-sm" @click="loadGroups()">刷新节点</button>
          <button type="button" class="btn btn-primary btn-sm" @click="openGroupModal()">
            <Plus :size="14" /> 新建策略组
          </button>
        </div>
      </div>

      <div v-if="groups.length" class="groups-stack">
        <SpotlightCard v-for="group in groups" :key="group.name" class="group-card">
          <div class="group-head" @click="toggleGroupExpand(group.name)">
            <div class="group-title-row">
              <ChevronDown
                :size="16"
                class="chevron-icon"
                :class="{ rotated: expandedGroups[group.name] }"
                aria-hidden="true"
              />
              <strong>{{ group.name }}</strong>
              <span class="badge badge-neutral">{{ group.type }}</span>
              <span v-if="group.custom" class="badge badge-brand">自定义</span>
              <span v-if="group.now" class="current-node mono-text">{{ group.now }}</span>
            </div>
            <div class="group-head-actions" @click.stop>
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                :disabled="!status?.running || testingGroup === group.name"
                @click="testGroupDelay(group.name)"
              >
                <Zap :size="13" /> {{ testingGroup === group.name ? '测速中…' : '测速' }}
              </button>
              <button
                v-if="group.custom && group.group_id"
                type="button"
                class="btn btn-ghost btn-sm text-bad"
                :aria-label="`删除策略组 ${group.name}`"
                @click="deleteCustomGroup(group)"
              >
                <Trash2 :size="13" />
              </button>
            </div>
          </div>

          <div v-show="expandedGroups[group.name]" class="nodes-grid">
            <button
              v-for="node in group.all"
              :key="node"
              type="button"
              class="node-tile"
              :class="{ selected: group.now === node }"
              :disabled="!status?.running || (group.type !== 'Selector' && group.type !== 'select')"
              @click="selectNode(group.name, node)"
            >
              <span class="node-name" :title="node">{{ node }}</span>
              <span class="node-delay mono-text" :class="delayClass(delays[node])">
                {{ formatDelay(delays[node]) }}
              </span>
            </button>
          </div>
        </SpotlightCard>
      </div>
      <SpotlightCard v-else class="report-panel">
        <p class="report-empty">暂无策略组</p>
      </SpotlightCard>
    </section>

    <!-- Tab 2: Custom Routing Rules -->
    <section v-show="activeTab === 'rules'" class="tab-section" role="tabpanel" aria-label="自定义分流">
      <div class="section-toolbar">
        <div class="filter-inputs">
          <input
            v-model.trim="ruleSearch"
            type="search"
            class="form-input toolbar-input"
            aria-label="搜索规则"
            placeholder="搜索域名 / 网段 / 端口…"
          />
          <select v-model="ruleTargetFilter" class="form-input toolbar-select" aria-label="按目标策略组筛选">
            <option value="">全部出站目标</option>
            <option v-for="target in ruleTargetOptions" :key="target" :value="target">{{ target }}</option>
          </select>
        </div>
        <button type="button" class="btn btn-primary btn-sm" @click="openRuleModal()">
          <Plus :size="14" /> 添加规则
        </button>
      </div>

      <SpotlightCard class="report-panel rules-panel">
        <div v-if="filteredRules.length" class="rules-list">
          <div v-for="rule in filteredRules" :key="rule.id" class="rule-row" :class="{ disabled: !rule.enabled }">
            <div class="rule-main">
              <span class="badge badge-neutral mono-text">{{ rule.type }}</span>
              <span class="rule-value mono-text">{{ rule.value }}</span>
              <ArrowRight :size="14" class="rule-arrow" aria-hidden="true" />
              <span class="badge badge-brand">{{ rule.target }}</span>
              <span v-if="rule.no_resolve" class="badge badge-neutral">no-resolve</span>
              <span v-if="rule.comment" class="rule-comment">{{ rule.comment }}</span>
            </div>
            <div class="rule-actions">
              <button
                type="button"
                class="btn btn-ghost btn-sm"
                :disabled="savingRules || isFirstRule(rule.id)"
                title="上移优先级"
                :aria-label="`上移规则 ${rule.value}`"
                @click="moveRule(rule.id, -1)"
              >
                <ArrowUp :size="14" />
              </button>
              <button
                type="button"
                class="btn btn-ghost btn-sm"
                :disabled="savingRules || isLastRule(rule.id)"
                title="下移优先级"
                :aria-label="`下移规则 ${rule.value}`"
                @click="moveRule(rule.id, 1)"
              >
                <ArrowDown :size="14" />
              </button>
              <label class="switch" :aria-label="`启用规则 ${rule.value}`">
                <input
                  type="checkbox"
                  :checked="rule.enabled"
                  :disabled="savingRules"
                  @change="toggleRule(rule.id)"
                />
                <span class="switch-slider" />
              </label>
              <button
                type="button"
                class="btn btn-ghost btn-sm text-bad"
                :disabled="savingRules"
                :aria-label="`删除规则 ${rule.value}`"
                @click="removeRule(rule.id)"
              >
                <Trash2 :size="14" />
              </button>
            </div>
          </div>
        </div>
        <p v-else class="report-empty">{{ status?.custom_rules?.length ? '无匹配规则' : '暂无自定义分流规则' }}</p>
      </SpotlightCard>
    </section>

    <!-- Tab 3: Subscriptions, Ports & Core Settings -->
    <section v-show="activeTab === 'settings'" class="tab-section settings-tab-grid" role="tabpanel" aria-label="订阅与配置">
      <!-- Core Override & Ports Card -->
      <SpotlightCard v-if="settingsDraft" class="report-panel">
        <div class="panel-header-row">
          <h2>代理端口与内核覆盖配置</h2>
          <button type="button" class="btn btn-primary btn-sm" :disabled="savingSettings" @click="saveCoreSettings">
            {{ savingSettings ? '应用中…' : '保存并生效' }}
          </button>
        </div>

        <form class="settings-form" @submit.prevent="saveCoreSettings">
          <div class="ports-grid">
            <label class="form-field">
              <span>混合端口 (Mixed HTTP+SOCKS5)</span>
              <input
                v-model.number="settingsDraft.settings.mixed_port"
                name="mixed_port"
                type="number"
                min="0"
                max="65535"
                class="form-input mono-text"
                placeholder="0 表示停用"
              />
            </label>
            <label class="form-field">
              <span>HTTP / HTTPS 端口</span>
              <input
                v-model.number="settingsDraft.settings.http_port"
                name="http_port"
                type="number"
                min="0"
                max="65535"
                class="form-input mono-text"
                placeholder="0 表示停用"
              />
            </label>
            <label class="form-field">
              <span>SOCKS5 端口</span>
              <input
                v-model.number="settingsDraft.settings.socks_port"
                name="socks_port"
                type="number"
                min="0"
                max="65535"
                class="form-input mono-text"
                placeholder="0 表示停用"
              />
            </label>
            <label class="form-field">
              <span>内部控制端口 (仅 127.0.0.1)</span>
              <input
                v-model.number="settingsDraft.controller_port"
                name="controller_port"
                type="number"
                min="1"
                max="65535"
                class="form-input mono-text"
                required
              />
            </label>
          </div>

          <div class="selects-grid">
            <label class="form-field">
              <span>DNS 增强模式</span>
              <select v-model="settingsDraft.settings.dns_enhanced_mode" name="dns_enhanced_mode" class="form-input">
                <option value="fake-ip">fake-ip</option>
                <option value="redir-host">redir-host</option>
                <option value="keep">沿用订阅原配置</option>
              </select>
            </label>
            <label class="form-field">
              <span>日志级别</span>
              <select v-model="settingsDraft.settings.log_level" name="log_level" class="form-input">
                <option value="info">info</option>
                <option value="warning">warning</option>
                <option value="error">error</option>
                <option value="debug">debug</option>
                <option value="silent">silent</option>
              </select>
            </label>
          </div>

          <div class="toggles-grid">
            <label class="toggle-card">
              <span>允许局域网接入 (allow-lan)</span>
              <input v-model="settingsDraft.settings.allow_lan" name="allow_lan" type="checkbox" />
            </label>
            <label class="toggle-card">
              <span>启用 IPv6（含 DNS IPv6）</span>
              <input v-model="settingsDraft.settings.ipv6" name="ipv6" type="checkbox" />
            </label>
            <label class="toggle-card">
              <span>GateHome 出站自动使用此代理</span>
              <input v-model="settingsDraft.takeover_outbound" name="takeover_outbound" type="checkbox" />
            </label>
            <label class="toggle-card">
              <span>随 GateHome 启动自动运行</span>
              <input v-model="settingsDraft.enabled" name="enabled" type="checkbox" />
            </label>
            <label class="toggle-card">
              <span>统一延迟 (unified-delay)</span>
              <input v-model="settingsDraft.settings.unified_delay" name="unified_delay" type="checkbox" />
            </label>
            <label class="toggle-card">
              <span>TCP 并发 (tcp-concurrent)</span>
              <input v-model="settingsDraft.settings.tcp_concurrent" name="tcp_concurrent" type="checkbox" />
            </label>
          </div>
        </form>
      </SpotlightCard>

      <!-- Subscriptions Card -->
      <SpotlightCard class="report-panel">
        <div class="panel-header-row">
          <h2>订阅管理</h2>
          <button type="button" class="btn btn-primary btn-sm" @click="openSubModal()">
            <Plus :size="14" /> 添加订阅
          </button>
        </div>

        <div v-if="status?.subscriptions?.length" class="subs-list">
          <div
            v-for="sub in status.subscriptions"
            :key="sub.id"
            class="sub-card"
            :class="{ active: status.active_subscription_id === sub.id }"
          >
            <div class="sub-info">
              <div class="sub-title-line">
                <strong>{{ sub.name }}</strong>
                <span v-if="status.active_subscription_id === sub.id" class="badge badge-ok">当前启用</span>
                <span class="badge badge-neutral">{{ sub.node_count }} 节点</span>
              </div>
              <div class="sub-meta mono-text">
                <span>{{ sub.url_masked || '本地 YAML 导入' }}</span>
                <span v-if="sub.updated_at"> · {{ formatTime(sub.updated_at) }}</span>
                <span v-if="sub.total"> · {{ formatQuota(sub) }}</span>
              </div>
              <p v-if="sub.last_error" class="report-error">{{ sub.last_error }}</p>
            </div>
            <div class="sub-actions">
              <button
                v-if="status.active_subscription_id !== sub.id"
                type="button"
                class="btn btn-secondary btn-sm"
                :disabled="busySub === sub.id"
                @click="activateSub(sub.id)"
              >
                启用
              </button>
              <button
                v-if="sub.has_url"
                type="button"
                class="btn btn-secondary btn-sm"
                :disabled="busySub === sub.id"
                @click="refreshSub(sub.id)"
              >
                <RefreshCw :size="13" /> {{ busySub === sub.id ? '更新中…' : '更新' }}
              </button>
              <button
                type="button"
                class="btn btn-ghost btn-sm text-bad"
                :disabled="busySub === sub.id"
                :aria-label="`删除订阅 ${sub.name}`"
                @click="deleteSub(sub)"
              >
                <Trash2 :size="13" />
              </button>
            </div>
          </div>
        </div>
        <p v-else class="report-empty">未导入订阅（当前基于冷启动默认基线运行）</p>
      </SpotlightCard>

      <!-- Core Binary Management Card -->
      <SpotlightCard v-if="status" class="report-panel">
        <div class="panel-header-row">
          <h2>Mihomo 内核</h2>
          <div class="report-actions">
            <button type="button" class="btn btn-secondary btn-sm" :disabled="coreBusy" @click="triggerUpload">
              <Upload :size="14" /> 上传内核文件
            </button>
            <button type="button" class="btn btn-primary btn-sm" :disabled="coreBusy" @click="downloadCore">
              <Download :size="14" /> {{ coreBusy ? '处理中…' : '在线下载内核' }}
            </button>
            <input ref="coreFileInput" type="file" class="sr-only" @change="handleFileUpload" />
          </div>
        </div>
        <dl class="core-meta">
          <div>
            <dt>安装状态</dt>
            <dd>{{ status.core_installed ? '已就绪' : '未安装' }}</dd>
          </div>
          <div>
            <dt>当前版本</dt>
            <dd class="mono-text">{{ status.core_version || '—' }}</dd>
          </div>
          <div>
            <dt>执行路径</dt>
            <dd class="mono-text">{{ status.binary_path || '—' }}</dd>
          </div>
        </dl>
      </SpotlightCard>
    </section>

    <!-- Tab 4: Config YAML Dual View -->
    <section v-show="activeTab === 'yaml'" class="tab-section" role="tabpanel" aria-label="配置文件">
      <SpotlightCard class="report-panel yaml-panel">
        <div class="panel-header-row yaml-toolbar">
          <div class="yaml-left">
            <div class="mode-segment" role="group" aria-label="YAML 视图切换">
              <button
                type="button"
                class="mode-btn"
                :class="{ active: yamlKind === 'runtime' }"
                @click="loadYaml('runtime')"
              >
                运行配置 (runtime.yaml)
              </button>
              <button
                type="button"
                class="mode-btn"
                :class="{ active: yamlKind === 'source' }"
                @click="loadYaml('source')"
              >
                订阅原件 (source.yaml)
              </button>
            </div>
            <span v-if="yamlKind === 'runtime'" class="badge" :class="yamlManualOverride ? 'badge-warn' : 'badge-ok'">
              {{ yamlManualOverride ? '已手动编辑' : '流水线自动合成' }}
            </span>
            <span v-else class="badge badge-neutral">只读原件</span>
          </div>

          <div v-if="yamlKind === 'runtime'" class="report-actions">
            <button type="button" class="btn btn-secondary btn-sm" :disabled="yamlBusy" @click="resyncYaml">
              <RotateCw :size="13" /> 重新合成
            </button>
            <button type="button" class="btn btn-primary btn-sm" :disabled="yamlBusy" @click="saveYaml">
              <Save :size="13" /> {{ yamlBusy ? '保存中…' : '保存并热重载' }}
            </button>
          </div>
        </div>

        <textarea
          v-model="yamlContent"
          class="form-input yaml-editor mono-text"
          :readonly="yamlKind === 'source'"
          aria-label="Mihomo YAML 配置内容"
          spellcheck="false"
        />
      </SpotlightCard>
    </section>

    <!-- Dialog: Create Custom Proxy Group -->
    <dialog ref="groupDialog" class="modal-dialog" aria-labelledby="mihomo-group-dialog-title">
      <div class="modal-header">
        <h2 id="mihomo-group-dialog-title" class="modal-title">新建自定义策略组</h2>
        <button type="button" class="close-modal-btn" aria-label="关闭" @click="groupDialog?.close()">
          <X :size="18" />
        </button>
      </div>
      <form id="mihomo-group-form" class="modal-body mihomo-modal-form" @submit.prevent="submitCustomGroup">
        <p v-if="modalError" class="report-error" role="alert">{{ modalError }}</p>
        <label class="form-field">
          <span>策略组名称</span>
          <input v-model.trim="groupForm.name" name="group_name" class="form-input" required maxlength="128" placeholder="例如：AI 专线" />
        </label>
        <div class="ports-grid">
          <label class="form-field">
            <span>类型</span>
            <select v-model="groupForm.type" name="group_type" class="form-input">
              <option value="select">手动选择 (select)</option>
              <option value="url-test">自动测速 (url-test)</option>
              <option value="fallback">故障转移 (fallback)</option>
              <option value="load-balance">负载均衡 (load-balance)</option>
            </select>
          </label>
          <label class="form-field">
            <span>节点筛选正则 (可选)</span>
            <input v-model.trim="groupForm.filter" name="group_filter" class="form-input mono-text" placeholder="例如：香港|HK|SG" />
          </label>
        </div>
        <div v-if="groupForm.type !== 'select'" class="ports-grid">
          <label class="form-field">
            <span>测速 URL</span>
            <input v-model.trim="groupForm.url" class="form-input mono-text" placeholder="https://www.gstatic.com/generate_204" />
          </label>
          <label class="form-field">
            <span>间隔 (秒)</span>
            <input v-model.number="groupForm.interval" type="number" min="30" max="86400" class="form-input mono-text" />
          </label>
        </div>
        <div v-if="availableNodeCandidates.length" class="node-picker">
          <span class="picker-label">手选成员节点 / 出站</span>
          <div class="picker-chips">
            <label v-for="cand in availableNodeCandidates" :key="cand" class="picker-chip">
              <input v-model="groupForm.proxies" type="checkbox" :value="cand" />
              <span>{{ cand }}</span>
            </label>
          </div>
        </div>
      </form>
      <div class="modal-footer">
        <button type="button" class="btn btn-secondary" @click="groupDialog?.close()">取消</button>
        <button type="submit" form="mihomo-group-form" class="btn btn-primary" :disabled="modalBusy">保存策略组</button>
      </div>
    </dialog>

    <!-- Dialog: Add Custom Rule -->
    <dialog ref="ruleDialog" class="modal-dialog" aria-labelledby="mihomo-rule-dialog-title">
      <div class="modal-header">
        <h2 id="mihomo-rule-dialog-title" class="modal-title">添加自定义分流规则</h2>
        <button type="button" class="close-modal-btn" aria-label="关闭" @click="ruleDialog?.close()">
          <X :size="18" />
        </button>
      </div>
      <form id="mihomo-rule-form" class="modal-body mihomo-modal-form" @submit.prevent="submitRule">
        <p v-if="modalError" class="report-error" role="alert">{{ modalError }}</p>
        <div class="ports-grid">
          <label class="form-field">
            <span>规则类型</span>
            <select v-model="ruleForm.type" name="rule_type" class="form-input">
              <option value="DOMAIN-SUFFIX">DOMAIN-SUFFIX</option>
              <option value="DOMAIN-KEYWORD">DOMAIN-KEYWORD</option>
              <option value="DOMAIN">DOMAIN</option>
              <option value="IP-CIDR">IP-CIDR</option>
              <option value="IP-CIDR6">IP-CIDR6</option>
              <option value="DST-PORT">DST-PORT</option>
              <option value="SRC-IP-CIDR">SRC-IP-CIDR</option>
              <option value="GEOSITE">GEOSITE</option>
              <option value="GEOIP">GEOIP</option>
              <option value="PROCESS-NAME">PROCESS-NAME</option>
            </select>
          </label>
          <label class="form-field">
            <span>目标策略组</span>
            <select v-model="ruleForm.target" name="rule_target" class="form-input" required>
              <option v-for="t in ruleTargetOptions" :key="t" :value="t">{{ t }}</option>
            </select>
          </label>
        </div>
        <label class="form-field">
          <span>匹配值</span>
          <input v-model.trim="ruleForm.value" name="rule_value" class="form-input mono-text" required placeholder="例如：openai.com 或 192.168.1.0/24" />
        </label>
        <label class="form-field">
          <span>备注 (可选)</span>
          <input v-model.trim="ruleForm.comment" name="rule_comment" class="form-input" maxlength="256" />
        </label>
        <label v-if="['IP-CIDR', 'IP-CIDR6', 'SRC-IP-CIDR', 'GEOIP'].includes(ruleForm.type)" class="toggle-card">
          <span>跳过 DNS 解析 (no-resolve)</span>
          <input v-model="ruleForm.no_resolve" type="checkbox" />
        </label>
      </form>
      <div class="modal-footer">
        <button type="button" class="btn btn-secondary" @click="ruleDialog?.close()">取消</button>
        <button type="submit" form="mihomo-rule-form" class="btn btn-primary" :disabled="modalBusy">添加至顶部</button>
      </div>
    </dialog>

    <!-- Dialog: Add Subscription -->
    <dialog ref="subDialog" class="modal-dialog" aria-labelledby="mihomo-sub-dialog-title">
      <div class="modal-header">
        <h2 id="mihomo-sub-dialog-title" class="modal-title">添加代理订阅</h2>
        <button type="button" class="close-modal-btn" aria-label="关闭" @click="subDialog?.close()">
          <X :size="18" />
        </button>
      </div>
      <form id="mihomo-sub-form" class="modal-body mihomo-modal-form" @submit.prevent="submitSubscription">
        <p v-if="modalError" class="report-error" role="alert">{{ modalError }}</p>
        <label class="form-field">
          <span>订阅名称</span>
          <input v-model.trim="subForm.name" name="sub_name" class="form-input" required maxlength="100" placeholder="例如：主力机场" />
        </label>
        <label class="form-field">
          <span>订阅链接 (URL)</span>
          <input v-model.trim="subForm.url" name="sub_url" type="url" class="form-input mono-text" placeholder="https://example.com/sub?..." />
        </label>
        <label class="form-field">
          <span>或直接粘贴 Clash YAML 配置</span>
          <textarea v-model="subForm.yaml_content" name="sub_yaml" class="form-input mono-text" rows="5" placeholder="留空则从上方 URL 下载" />
        </label>
      </form>
      <div class="modal-footer">
        <button type="button" class="btn btn-secondary" @click="subDialog?.close()">取消</button>
        <button type="submit" form="mihomo-sub-form" class="btn btn-primary" :disabled="modalBusy">{{ modalBusy ? '导入中…' : '保存订阅' }}</button>
      </div>
    </dialog>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import {
  ArrowDown,
  ArrowRight,
  ArrowUp,
  ChevronDown,
  Copy,
  Download,
  FileCode2,
  GitBranch,
  Layers,
  Play,
  Plus,
  RefreshCw,
  RotateCw,
  Save,
  Sliders,
  Square,
  Trash2,
  Upload,
  X,
  Zap
} from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import { adminApi } from '@/api/admin'
import { useToast } from '@/composables/useToast'
import '@/styles/reports.css'

const toast = useToast()
const controller = new AbortController()

const modes = [
  { id: 'rule', label: '规则' },
  { id: 'global', label: '全局' },
  { id: 'direct', label: '直连' }
]

const tabs = [
  { id: 'groups', label: '策略组与节点', icon: Layers },
  { id: 'rules', label: '自定义分流', icon: GitBranch },
  { id: 'settings', label: '订阅与配置', icon: Sliders },
  { id: 'yaml', label: '配置文件', icon: FileCode2 }
]

const status = ref(null)
const settingsDraft = ref(null)
const loading = ref(false)
const busyAction = ref(false)
const savingSettings = ref(false)
const savingRules = ref(false)
const busySub = ref('')
const coreBusy = ref(false)
const testingGroup = ref('')
const error = ref('')

const activeTab = ref('groups')
const groups = ref([])
const delays = ref({})
const allNodes = ref([])
const groupsLive = ref(false)
const expandedGroups = reactive({})

const ruleSearch = ref('')
const ruleTargetFilter = ref('')

const yamlKind = ref('runtime')
const yamlContent = ref('')
const yamlManualOverride = ref(false)
const yamlBusy = ref(false)

const groupDialog = ref(null)
const ruleDialog = ref(null)
const subDialog = ref(null)
const coreFileInput = ref(null)
const modalError = ref('')
const modalBusy = ref(false)

const groupForm = reactive({
  name: '',
  type: 'select',
  filter: '',
  url: 'https://www.gstatic.com/generate_204',
  interval: 300,
  proxies: []
})

const ruleForm = reactive({
  type: 'DOMAIN-SUFFIX',
  value: '',
  target: 'DIRECT',
  no_resolve: false,
  comment: ''
})

const subForm = reactive({
  name: '',
  url: '',
  yaml_content: ''
})

const currentMode = computed(() => status.value?.settings?.mode || 'rule')

const activePortEntries = computed(() => {
  const s = status.value?.settings
  if (!s) return []
  const host = s.allow_lan ? status.value.lan_ip || '127.0.0.1' : '127.0.0.1'
  const entries = []
  if (s.mixed_port > 0) entries.push({ label: 'Mixed', address: `${host}:${s.mixed_port}` })
  if (s.http_port > 0) entries.push({ label: 'HTTP', address: `${host}:${s.http_port}` })
  if (s.socks_port > 0) entries.push({ label: 'SOCKS5', address: `${host}:${s.socks_port}` })
  return entries
})

const availableNodeCandidates = computed(() => {
  const seen = new Set(['DIRECT', 'REJECT'])
  for (const g of groups.value) {
    if (g.name) seen.add(g.name)
  }
  for (const n of allNodes.value) {
    if (n) seen.add(n)
  }
  return [...seen]
})

const ruleTargetOptions = computed(() => {
  const set = new Set(['DIRECT', 'REJECT'])
  for (const cg of status.value?.custom_groups || []) {
    if (cg.name) set.add(cg.name)
  }
  for (const g of groups.value) {
    if (g.name) set.add(g.name)
  }
  return [...set]
})

const filteredRules = computed(() => {
  const list = status.value?.custom_rules || []
  const q = ruleSearch.value.toLowerCase()
  const target = ruleTargetFilter.value
  return list.filter((r) => {
    if (target && r.target !== target) return false
    if (!q) return true
    return (
      r.value.toLowerCase().includes(q) ||
      r.type.toLowerCase().includes(q) ||
      r.target.toLowerCase().includes(q) ||
      (r.comment || '').toLowerCase().includes(q)
    )
  })
})

function applyStatus(data) {
  status.value = data
  settingsDraft.value = {
    enabled: !!data.enabled,
    takeover_outbound: !!data.takeover_outbound,
    controller_port: data.controller_port || 19090,
    settings: { ...data.settings }
  }
  yamlManualOverride.value = !!data.manual_override
}

async function loadStatus() {
  loading.value = true
  error.value = ''
  try {
    const data = await adminApi.mihomoStatus(controller.signal)
    if (!controller.signal.aborted) applyStatus(data)
  } catch (err) {
    if (!controller.signal.aborted) error.value = err.message
  } finally {
    loading.value = false
  }
}

async function loadGroups() {
  try {
    const res = await adminApi.mihomoGroups(controller.signal)
    if (controller.signal.aborted) return
    groups.value = res.groups || []
    delays.value = res.delays || {}
    allNodes.value = res.all_nodes || []
    groupsLive.value = !!res.live
    if (groups.value.length && Object.keys(expandedGroups).length === 0) {
      expandedGroups[groups.value[0].name] = true
    }
  } catch (err) {
    if (!controller.signal.aborted) error.value = err.message
  }
}

async function loadYaml(kind = yamlKind.value) {
  yamlKind.value = kind
  yamlBusy.value = true
  try {
    const res = await adminApi.mihomoYaml(kind, controller.signal)
    if (controller.signal.aborted) return
    yamlContent.value = res.content || ''
    yamlManualOverride.value = !!res.manual_override
  } catch (err) {
    if (!controller.signal.aborted) toast.error(err.message)
  } finally {
    yamlBusy.value = false
  }
}

async function switchTab(tabId) {
  activeTab.value = tabId
  if (tabId === 'groups') await loadGroups()
  if (tabId === 'yaml') await loadYaml(yamlKind.value)
}

async function runCoreAction(fn, okMsg) {
  busyAction.value = true
  error.value = ''
  try {
    const data = await fn()
    applyStatus(data)
    toast.success(okMsg)
    await loadGroups()
  } catch (err) {
    error.value = err.message
    toast.error(err.message)
  } finally {
    busyAction.value = false
  }
}

const startCore = () => runCoreAction(() => adminApi.startMihomo(), 'Mihomo 内核已启动')
const stopCore = () => runCoreAction(() => adminApi.stopMihomo(), 'Mihomo 内核已停止')
const restartCore = () => runCoreAction(() => adminApi.restartMihomo(), 'Mihomo 内核已重启')
const reloadCore = () => runCoreAction(() => adminApi.reloadMihomo(), '配置已热重载')

async function switchMode(mode) {
  if (currentMode.value === mode) return
  await runCoreAction(() => adminApi.setMihomoMode(mode), `已切换为${modes.find((m) => m.id === mode)?.label}模式`)
}

function toggleGroupExpand(name) {
  expandedGroups[name] = !expandedGroups[name]
}

async function selectNode(group, node) {
  try {
    await adminApi.selectMihomoProxy(group, node)
    const g = groups.value.find((item) => item.name === group)
    if (g) g.now = node
    toast.success(`已切换 ${group} → ${node}`)
  } catch (err) {
    toast.error(err.message)
  }
}

async function testGroupDelay(groupName) {
  testingGroup.value = groupName
  try {
    const res = await adminApi.testMihomoGroupDelay(groupName)
    delays.value = { ...delays.value, ...(res.delays || {}) }
  } catch (err) {
    toast.error(err.message)
  } finally {
    testingGroup.value = ''
  }
}

function openGroupModal() {
  modalError.value = ''
  groupForm.name = ''
  groupForm.type = 'select'
  groupForm.filter = ''
  groupForm.url = 'https://www.gstatic.com/generate_204'
  groupForm.interval = 300
  groupForm.proxies = []
  groupDialog.value?.showModal()
}

async function submitCustomGroup() {
  modalError.value = ''
  modalBusy.value = true
  try {
    const data = await adminApi.saveMihomoGroup({
      name: groupForm.name,
      type: groupForm.type,
      filter: groupForm.filter,
      url: groupForm.type === 'select' ? '' : groupForm.url,
      interval: groupForm.type === 'select' ? 0 : groupForm.interval,
      proxies: groupForm.proxies
    })
    applyStatus(data)
    groupDialog.value?.close()
    await loadGroups()
    expandedGroups[groupForm.name] = true
    toast.success('自定义策略组已保存')
  } catch (err) {
    modalError.value = err.message
  } finally {
    modalBusy.value = false
  }
}

async function deleteCustomGroup(group) {
  if (!confirm(`确定删除自定义策略组「${group.name}」？`)) return
  try {
    const data = await adminApi.deleteMihomoGroup(group.group_id)
    applyStatus(data)
    await loadGroups()
    toast.success('策略组已删除')
  } catch (err) {
    toast.error(err.message)
  }
}

function openRuleModal() {
  modalError.value = ''
  ruleForm.type = 'DOMAIN-SUFFIX'
  ruleForm.value = ''
  ruleForm.target = ruleTargetOptions.value[0] || 'DIRECT'
  ruleForm.no_resolve = false
  ruleForm.comment = ''
  ruleDialog.value?.showModal()
}

async function submitRule() {
  modalError.value = ''
  modalBusy.value = true
  try {
    const data = await adminApi.addMihomoRule({
      type: ruleForm.type,
      value: ruleForm.value,
      target: ruleForm.target,
      no_resolve: ruleForm.no_resolve,
      enabled: true,
      comment: ruleForm.comment
    })
    applyStatus(data)
    ruleDialog.value?.close()
    toast.success('分流规则已添加')
  } catch (err) {
    modalError.value = err.message
  } finally {
    modalBusy.value = false
  }
}

function isFirstRule(id) {
  const list = status.value?.custom_rules || []
  return list.length === 0 || list[0].id === id
}

function isLastRule(id) {
  const list = status.value?.custom_rules || []
  return list.length === 0 || list[list.length - 1].id === id
}

async function saveRulesList(nextRules) {
  savingRules.value = true
  try {
    const data = await adminApi.saveMihomoRules(nextRules)
    applyStatus(data)
  } catch (err) {
    toast.error(err.message)
  } finally {
    savingRules.value = false
  }
}

async function moveRule(id, delta) {
  const list = [...(status.value?.custom_rules || [])]
  const idx = list.findIndex((r) => r.id === id)
  const target = idx + delta
  if (idx < 0 || target < 0 || target >= list.length) return
  ;[list[idx], list[target]] = [list[target], list[idx]]
  await saveRulesList(list)
}

async function toggleRule(id) {
  const list = (status.value?.custom_rules || []).map((r) => (r.id === id ? { ...r, enabled: !r.enabled } : r))
  await saveRulesList(list)
}

async function removeRule(id) {
  savingRules.value = true
  try {
    const data = await adminApi.deleteMihomoRule(id)
    applyStatus(data)
    toast.success('规则已删除')
  } catch (err) {
    toast.error(err.message)
  } finally {
    savingRules.value = false
  }
}

async function saveCoreSettings() {
  if (!settingsDraft.value) return
  savingSettings.value = true
  error.value = ''
  try {
    const payload = {
      enabled: settingsDraft.value.enabled,
      takeover_outbound: settingsDraft.value.takeover_outbound,
      controller_port: Number(settingsDraft.value.controller_port) || 19090,
      settings: {
        ...settingsDraft.value.settings,
        mixed_port: Number(settingsDraft.value.settings.mixed_port) || 0,
        http_port: Number(settingsDraft.value.settings.http_port) || 0,
        socks_port: Number(settingsDraft.value.settings.socks_port) || 0
      }
    }
    const data = await adminApi.saveMihomoSettings(payload)
    applyStatus(data)
    await loadGroups()
    toast.success('端口与核心设置已保存并合成 runtime.yaml')
  } catch (err) {
    error.value = err.message
    toast.error(err.message)
  } finally {
    savingSettings.value = false
  }
}

function openSubModal() {
  modalError.value = ''
  subForm.name = ''
  subForm.url = ''
  subForm.yaml_content = ''
  subDialog.value?.showModal()
}

async function submitSubscription() {
  modalError.value = ''
  modalBusy.value = true
  try {
    const data = await adminApi.addMihomoSubscription({ ...subForm })
    applyStatus(data)
    subDialog.value?.close()
    await loadGroups()
    toast.success('订阅已添加')
  } catch (err) {
    modalError.value = err.message
  } finally {
    modalBusy.value = false
  }
}

async function activateSub(id) {
  busySub.value = id
  try {
    const data = await adminApi.activateMihomoSubscription(id)
    applyStatus(data)
    await loadGroups()
    toast.success('已切换启用订阅')
  } catch (err) {
    toast.error(err.message)
  } finally {
    busySub.value = ''
  }
}

async function refreshSub(id) {
  busySub.value = id
  try {
    const data = await adminApi.refreshMihomoSubscription(id)
    applyStatus(data)
    await loadGroups()
    toast.success('订阅已更新')
  } catch (err) {
    toast.error(err.message)
    await loadStatus()
  } finally {
    busySub.value = ''
  }
}

async function deleteSub(sub) {
  if (!confirm(`确定删除订阅「${sub.name}」？`)) return
  busySub.value = sub.id
  try {
    const data = await adminApi.deleteMihomoSubscription(sub.id)
    applyStatus(data)
    await loadGroups()
    toast.success('订阅已删除')
  } catch (err) {
    toast.error(err.message)
  } finally {
    busySub.value = ''
  }
}

async function downloadCore() {
  coreBusy.value = true
  try {
    const res = await adminApi.downloadMihomoCore()
    if (res.status) applyStatus(res.status)
    toast.success(`内核已就绪: ${shortVersion(res.version)}`)
  } catch (err) {
    toast.error(err.message)
  } finally {
    coreBusy.value = false
  }
}

function triggerUpload() {
  coreFileInput.value?.click()
}

async function handleFileUpload(event) {
  const file = event.target.files?.[0]
  event.target.value = ''
  if (!file) return
  coreBusy.value = true
  try {
    const res = await adminApi.uploadMihomoCore(file)
    if (res.status) applyStatus(res.status)
    toast.success(`内核已上传: ${shortVersion(res.version)}`)
  } catch (err) {
    toast.error(err.message)
  } finally {
    coreBusy.value = false
  }
}

async function saveYaml() {
  yamlBusy.value = true
  try {
    const res = await adminApi.saveMihomoYaml(yamlContent.value)
    yamlContent.value = res.content || yamlContent.value
    yamlManualOverride.value = !!res.manual_override
    await loadStatus()
    await loadGroups()
    toast.success('运行配置已保存并应用')
  } catch (err) {
    toast.error(err.message)
  } finally {
    yamlBusy.value = false
  }
}

async function resyncYaml() {
  yamlBusy.value = true
  try {
    const res = await adminApi.resyncMihomoYaml()
    yamlContent.value = res.content || ''
    yamlManualOverride.value = !!res.manual_override
    await loadStatus()
    await loadGroups()
    toast.success('已按流水线重新合成 runtime.yaml')
  } catch (err) {
    toast.error(err.message)
  } finally {
    yamlBusy.value = false
  }
}

function shortVersion(ver) {
  if (!ver) return ''
  const match = ver.match(/v?\d+\.\d+\.\d+\S*/i)
  return match ? match[0] : ver.slice(0, 24)
}

function formatDelay(ms) {
  if (ms === undefined || ms === null) return '—'
  if (ms === 0) return '超时'
  return `${ms} ms`
}

function delayClass(ms) {
  if (!ms) return 'muted'
  if (ms < 120) return 'ok'
  if (ms < 280) return 'warn'
  return 'bad'
}

function formatTime(iso) {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleString()
}

function formatQuota(sub) {
  const used = (sub.upload || 0) + (sub.download || 0)
  const gb = (n) => `${(n / 1024 / 1024 / 1024).toFixed(1)}G`
  return `${gb(used)} / ${gb(sub.total)}`
}

async function copyText(text, msg) {
  try {
    await navigator.clipboard.writeText(text)
    toast.success(msg)
  } catch {
    toast.info(text)
  }
}

onMounted(async () => {
  await loadStatus()
  await loadGroups()
})

onBeforeUnmount(() => {
  controller.abort()
})
</script>

<style scoped>
.mihomo-page {
  gap: 16px;
}

.header-controls {
  gap: 8px;
}

.mode-segment {
  display: inline-flex;
  padding: 3px;
  border-radius: var(--radius-sm);
  background: var(--strip-bg);
  border: 1px solid var(--line);
  gap: 2px;
  flex-wrap: wrap;
}

.mode-btn {
  padding: 5px 11px;
  font-size: 12px;
  font-weight: 500;
  border-radius: 6px;
  color: var(--muted);
  transition: var(--transition-smooth);
  white-space: nowrap;
}

.mode-btn:hover {
  color: var(--ink);
}

.mode-btn.active {
  background: var(--card-bg);
  color: var(--ink);
  box-shadow: 0 1px 3px rgba(15, 23, 42, 0.08);
  font-weight: 600;
}

.runtime-strip {
  padding: 12px 16px;
  min-width: 0;
  max-width: 100%;
}

.strip-items {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}

.strip-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 10px;
  border-radius: var(--radius-pill);
  font-size: 11.5px;
  background: var(--strip-bg);
  border: 1px solid var(--line);
  color: var(--ink-secondary);
  max-width: 100%;
  overflow-wrap: anywhere;
}

.port-copy-chip:hover {
  border-color: var(--brand);
  color: var(--ink);
}

.strip-chip.chip-active {
  border-color: rgba(16, 185, 129, 0.3);
  color: var(--ok);
}

.strip-chip.chip-brand {
  border-color: rgba(99, 102, 241, 0.35);
  color: var(--brand-light);
}

.mihomo-tabs-wrap {
  overflow-x: auto;
  padding-bottom: 2px;
  min-width: 0;
  max-width: 100%;
}

.mihomo-tabs {
  display: inline-flex;
  gap: 4px;
  border-bottom: 1px solid var(--line);
  min-width: 100%;
}

.tab-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 9px 14px;
  font-size: 13px;
  font-weight: 500;
  color: var(--muted);
  border-bottom: 2px solid transparent;
  margin-bottom: -1px;
  white-space: nowrap;
  transition: var(--transition-smooth);
}

.tab-btn:hover {
  color: var(--ink);
}

.tab-btn.active {
  color: var(--brand);
  border-bottom-color: var(--brand);
  font-weight: 600;
}

.tab-section {
  display: grid;
  gap: 14px;
  min-width: 0;
}

.section-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 10px;
}

.toolbar-meta {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}

.meta-pill {
  font-size: 12px;
  color: var(--muted);
}

.groups-stack {
  display: grid;
  gap: 12px;
}

.group-card {
  padding: 14px 16px;
}

.group-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 10px;
  cursor: pointer;
}

.group-title-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  min-width: 0;
}

.chevron-icon {
  color: var(--muted);
  transition: transform 0.2s ease;
  transform: rotate(-90deg);
}

.chevron-icon.rotated {
  transform: rotate(0deg);
}

.current-node {
  font-size: 12px;
  color: var(--brand);
  font-weight: 600;
}

.group-head-actions {
  display: flex;
  align-items: center;
  gap: 6px;
}

.nodes-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(175px, 1fr));
  gap: 8px;
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px solid var(--line);
}

.node-tile {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 9px 11px;
  border-radius: var(--radius-sm);
  background: var(--strip-bg);
  border: 1px solid var(--line);
  text-align: left;
  transition: var(--transition-smooth);
  min-width: 0;
}

.node-tile:not(:disabled):hover {
  border-color: var(--card-border-hover);
  background: var(--card-hover-bg);
}

.node-tile.selected {
  border-color: var(--brand);
  background: rgba(99, 102, 241, 0.1);
}

.node-name {
  font-size: 12px;
  color: var(--ink);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.node-delay {
  font-size: 11px;
  flex-shrink: 0;
}

.node-delay.ok { color: var(--ok); }
.node-delay.warn { color: var(--warn); }
.node-delay.bad { color: var(--bad); }
.node-delay.muted { color: var(--faint); }

.filter-inputs {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  flex: 1;
  min-width: 0;
}

.toolbar-input {
  max-width: 260px;
  min-width: 140px;
}

.toolbar-select {
  max-width: 190px;
  min-width: 130px;
}

.rules-panel {
  padding: 14px 16px;
}

.rules-list {
  display: grid;
  gap: 8px;
}

.rule-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 10px;
  padding: 10px 12px;
  border-radius: var(--radius-sm);
  background: var(--strip-bg);
  border: 1px solid var(--line);
}

.rule-row.disabled {
  opacity: 0.55;
}

.rule-main {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  min-width: 0;
  flex: 1;
}

.rule-value {
  font-size: 12.5px;
  color: var(--ink);
  overflow-wrap: anywhere;
}

.rule-arrow {
  color: var(--faint);
}

.rule-comment {
  font-size: 11.5px;
  color: var(--muted);
}

.rule-actions {
  display: flex;
  align-items: center;
  gap: 4px;
}

.settings-tab-grid {
  gap: 16px;
}

.panel-header-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 10px;
  margin-bottom: 14px;
}

.panel-header-row h2 {
  margin-bottom: 0;
}

.settings-form {
  display: grid;
  gap: 16px;
}

.ports-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 12px;
}

.selects-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 12px;
}

.form-field {
  display: grid;
  gap: 6px;
  font-size: 12px;
  color: var(--ink-secondary);
  min-width: 0;
}

.toggles-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 10px;
}

.toggle-card {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 10px 12px;
  border-radius: var(--radius-sm);
  background: var(--strip-bg);
  border: 1px solid var(--line);
  font-size: 12.5px;
  color: var(--ink-secondary);
  cursor: pointer;
}

.subs-list {
  display: grid;
  gap: 10px;
}

.sub-card {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 12px;
  padding: 12px 14px;
  border-radius: var(--radius-sm);
  background: var(--strip-bg);
  border: 1px solid var(--line);
}

.sub-card.active {
  border-color: var(--brand);
}

.sub-info {
  display: grid;
  gap: 4px;
  min-width: 0;
}

.sub-title-line {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}

.sub-meta {
  font-size: 11.5px;
  color: var(--muted);
  overflow-wrap: anywhere;
}

.sub-actions {
  display: flex;
  align-items: center;
  gap: 6px;
}

.core-meta {
  display: grid;
  gap: 10px;
}

.core-meta > div {
  display: grid;
  grid-template-columns: 84px minmax(0, 1fr);
  gap: 12px;
  font-size: 12.5px;
}

.core-meta dt {
  color: var(--muted);
}

.core-meta dd {
  margin: 0;
  overflow-wrap: anywhere;
}

.yaml-panel {
  padding: 16px;
}

.yaml-toolbar {
  margin-bottom: 12px;
}

.yaml-left {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}

.yaml-editor {
  width: 100%;
  min-height: 460px;
  padding: 12px 14px;
  font-size: 12px;
  line-height: 1.55;
  resize: vertical;
  tab-size: 2;
}

.mihomo-modal-form {
  display: grid;
  gap: 14px;
}

.node-picker {
  display: grid;
  gap: 8px;
}

.picker-label {
  font-size: 12px;
  color: var(--ink-secondary);
}

.picker-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  max-height: 180px;
  overflow-y: auto;
  padding: 8px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--line);
  background: var(--strip-bg);
}

.picker-chip {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 4px 8px;
  border-radius: 6px;
  border: 1px solid var(--line);
  background: var(--card-bg);
  font-size: 11.5px;
  cursor: pointer;
}

.modal-footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 4px;
}

.text-bad {
  color: var(--bad);
}

.mono-text {
  font-family: var(--mono);
}

@media (max-width: 640px) {
  .toolbar-input,
  .toolbar-select {
    max-width: none;
    width: 100%;
  }

  .ports-grid,
  .selects-grid,
  .toggles-grid,
  .nodes-grid {
    grid-template-columns: minmax(0, 1fr);
  }

  .core-meta > div {
    grid-template-columns: minmax(0, 1fr);
    gap: 2px;
  }
}
</style>
