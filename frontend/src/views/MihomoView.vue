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
          :disabled="zashboardBusy || loading"
          @click="openZashboard"
        >
          <ExternalLink :size="14" /> {{ zashboardBusy ? '下载面板中…' : 'Zashboard 面板' }}
        </button>
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

    <!-- Compact Runtime Strip (shown on secondary tabs; Overview has full telemetry) -->
    <SpotlightCard v-if="status && activeTab !== 'overview'" class="runtime-strip">
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

        <span v-if="status.settings?.auth_enabled" class="strip-chip chip-brand">
          代理认证 {{ status.settings.auth_username }}
        </span>
      </div>
    </SpotlightCard>

    <p v-if="status?.last_error" class="report-warning" role="status">{{ status.last_error }}</p>

    <!-- Main 5-Tab Bar -->
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

    <!-- Tab 0: Overview Dashboard -->
    <section v-show="activeTab === 'overview'" class="tab-section" role="tabpanel" aria-label="概览">
      <MihomoOverview
        v-if="status"
        :status="status"
        :active-port-entries="activePortEntries"
        :active="activeTab === 'overview'"
        :core-busy="coreBusy"
        :checking-core-update="checkingCoreUpdate"
        @navigate="handleOverviewNavigate"
        @updated="onCustomYamlSaved"
        @check-core-update="checkCoreUpdate"
        @upgrade-core="downloadCore"
      />
    </section>

    <!-- Tab 1: Proxy Groups & Nodes -->
    <section v-show="activeTab === 'groups'" class="tab-section" role="tabpanel" aria-label="策略组与节点">
      <div class="section-toolbar">
        <div class="toolbar-meta">
          <span class="meta-pill">{{ groups.length }} 个策略组</span>
          <span class="meta-pill">{{ allNodes.length }} 个节点</span>
          <span v-if="groupsLive" class="badge badge-ok">实时控制</span>
          <span v-else class="badge badge-warn">离线预览（请先启动内核）</span>
        </div>
        <div class="report-actions">
          <button
            type="button"
            class="btn btn-secondary btn-sm"
            :aria-pressed="groupsView === 'yaml'"
            @click="groupsView = groupsView === 'yaml' ? 'list' : 'yaml'"
          >
            <FileCode2 :size="14" /> {{ groupsView === 'yaml' ? '返回列表' : 'YAML 编辑' }}
          </button>
          <button v-if="groupsView === 'list'" type="button" class="btn btn-secondary btn-sm" @click="loadGroups()">刷新节点</button>
          <button v-if="groupsView === 'list'" type="button" class="btn btn-primary btn-sm" @click="openGroupModal()">
            <Plus :size="14" /> 新建策略组
          </button>
        </div>
      </div>

      <div v-if="groupsView === 'list' && groups.length" class="groups-stack">
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
                :title="status?.running ? '测试该策略组所有节点延迟' : '请先点击右上角「启动」运行 Mihomo 内核'"
                @click="testGroupDelay(group.name)"
              >
                <Zap :size="13" /> {{ testingGroup === group.name ? '测速中…' : '测速' }}
              </button>
              <button
                v-if="group.custom && group.group_id"
                type="button"
                class="btn btn-secondary btn-sm"
                :aria-label="`编辑策略组 ${group.name}`"
                @click="openGroupModal(group)"
              >
                <Pencil :size="13" />
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
            <div
              v-for="node in group.all"
              :key="node"
              class="node-tile"
              :class="{ selected: group.now === node }"
            >
              <button
                type="button"
                class="node-select-btn"
                :disabled="!status?.running || (group.type !== 'Selector' && group.type !== 'select')"
                @click="selectNode(group.name, node)"
              >
                <span class="node-name" :title="node">{{ node }}</span>
              </button>
              <button
                v-if="!builtinNodeNames.has(node)"
                type="button"
                class="node-delay node-delay-btn mono-text"
                :class="delayClass(delays[node])"
                :disabled="!status?.running || testingNode === node"
                :title="status?.running ? `单独测速 ${node}` : '请先启动 Mihomo 内核'"
                :aria-label="`单独测速 ${node}`"
                @click.stop="testSingleNodeDelay(node)"
              >
                {{ testingNode === node ? '…' : formatDelay(delays[node]) }}
              </button>
              <span v-else class="node-delay mono-text muted">—</span>
            </div>
          </div>
        </SpotlightCard>
      </div>
      <SpotlightCard v-else-if="groupsView === 'list'" class="report-panel">
        <p class="report-empty">暂无策略组</p>
      </SpotlightCard>
      <MihomoCustomYaml v-else kind="groups" @saved="onCustomYamlSaved" />
    </section>

    <!-- Tab 2: Custom Routing Rules -->
    <section v-show="activeTab === 'rules'" class="tab-section" role="tabpanel" aria-label="自定义分流">
      <div class="section-toolbar rules-toolbar">
        <div class="mode-segment rules-view-switch" role="group" aria-label="分流视图切换">
          <button type="button" class="mode-btn" :class="{ active: rulesView === 'list' }" @click="rulesView = 'list'">
            分流规则
          </button>
          <button
            type="button"
            class="mode-btn"
            :class="{ active: rulesView === 'rulesets' }"
            @click="rulesView = 'rulesets'"
          >
            规则集 ({{ status?.custom_rule_sets?.length || 0 }})
          </button>
          <button type="button" class="mode-btn" :class="{ active: rulesView === 'yaml' }" @click="rulesView = 'yaml'">
            YAML 编辑
          </button>
        </div>

        <div class="report-actions rules-toolbar-actions">
          <div v-if="rulesView === 'list'" class="filter-inputs">
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
            <button type="button" class="btn btn-primary btn-sm" @click="openRuleModal()">
              <Plus :size="14" /> 添加规则
            </button>
          </div>
          <template v-else-if="rulesView === 'rulesets'">
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              :disabled="ruleSetToolbar.updatingAll || !ruleSetToolbar.hasUpdatable"
              @click="ruleSetsRef?.updateAll()"
            >
              <RefreshCw :size="13" :class="{ spinning: ruleSetToolbar.updatingAll }" />
              {{ ruleSetToolbar.updatingAll ? '更新中…' : '批量更新' }}
            </button>
            <button type="button" class="btn btn-primary btn-sm" @click="ruleSetsRef?.openModal()">
              <Plus :size="14" /> 新建规则集
            </button>
          </template>
        </div>
      </div>

      <SpotlightCard v-if="rulesView === 'list'" class="report-panel rules-panel">
        <div v-if="filteredRules.length" class="rules-list">
          <div v-for="rule in filteredRules" :key="rule.id" class="rule-row" :class="{ disabled: !rule.enabled }">
            <div class="rule-main">
              <span class="badge badge-neutral mono-text">{{ rule.type }}</span>
              <span v-if="rule.type !== 'MATCH'" class="rule-value mono-text">{{ rule.value }}</span>
              <span v-else class="rule-value rule-value-muted">全部剩余流量（兜底）</span>
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
                :aria-label="`上移规则 ${rule.type === 'MATCH' ? `MATCH ${rule.target}` : rule.value}`"
                @click="moveRule(rule.id, -1)"
              >
                <ArrowUp :size="14" />
              </button>
              <button
                type="button"
                class="btn btn-ghost btn-sm"
                :disabled="savingRules || isLastRule(rule.id)"
                title="下移优先级"
                :aria-label="`下移规则 ${rule.type === 'MATCH' ? `MATCH ${rule.target}` : rule.value}`"
                @click="moveRule(rule.id, 1)"
              >
                <ArrowDown :size="14" />
              </button>
              <label class="switch" :aria-label="`启用规则 ${rule.type === 'MATCH' ? `MATCH ${rule.target}` : rule.value}`">
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
                :aria-label="`删除规则 ${rule.type === 'MATCH' ? `MATCH ${rule.target}` : rule.value}`"
                @click="removeRule(rule.id)"
              >
                <Trash2 :size="14" />
              </button>
            </div>
          </div>
        </div>
        <p v-else class="report-empty">{{ status?.custom_rules?.length ? '无匹配规则' : '暂无自定义分流规则' }}</p>
      </SpotlightCard>
      <MihomoRuleSets
        v-else-if="rulesView === 'rulesets'"
        ref="ruleSetsRef"
        :rule-sets="status?.custom_rule_sets || []"
        :presets="status?.rule_set_presets || []"
        :target-options="ruleTargetOptions"
        :core-running="!!status?.running"
        @updated="applyStatus"
        @toolbar-state="ruleSetToolbar = $event"
      />
      <MihomoCustomYaml v-else kind="rules" @saved="onCustomYamlSaved" />
    </section>

    <!-- Tab 3: Connections -->
    <section v-show="activeTab === 'connections'" class="tab-section" role="tabpanel" aria-label="连接">
      <MihomoConnections :active="activeTab === 'connections'" :running="!!status?.running" />
    </section>

    <!-- Tab 4: Subscriptions & Special-Line DNS -->
    <section v-show="activeTab === 'subscriptions'" class="tab-section settings-tab-grid" role="tabpanel" aria-label="订阅管理">
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
                <span v-if="sub.exclude_groups_and_rules" class="badge badge-warn">仅导入节点</span>
                <span v-if="sub.include_filter" class="badge badge-brand">仅保留: {{ sub.include_filter }}</span>
                <span v-if="sub.exclude_filter" class="badge badge-warn">排除: {{ sub.exclude_filter }}</span>
                <span v-if="sub.node_suffix" class="badge badge-neutral">后缀: {{ sub.node_suffix }}</span>
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
                type="button"
                class="btn btn-secondary btn-sm"
                :disabled="busySub === sub.id"
                :aria-label="`编辑订阅 ${sub.name}`"
                @click="openSubModal(sub)"
              >
                <Pencil :size="13" /> 编辑
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

      <!-- Incremental & Inherited DNS Policies Card -->
      <SpotlightCard v-if="status" class="report-panel">
        <div class="panel-header-row">
          <div class="dns-heading-group">
            <h2>专线 DNS 策略 (nameserver-policy)</h2>
            <button
              v-if="status.inherited_dns_policies?.length"
              type="button"
              class="badge badge-ok inherited-dns-toggle"
              :aria-expanded="showInheritedDNS"
              @click="showInheritedDNS = !showInheritedDNS"
            >
              已继承订阅 {{ status.inherited_dns_policies.length }} 条
              <ChevronDown :size="12" class="tiny-chevron" :class="{ rotated: showInheritedDNS }" aria-hidden="true" />
            </button>
            <span v-else class="badge badge-neutral">订阅无自带策略</span>
          </div>
          <button type="button" class="btn btn-primary btn-sm" @click="openDNSPolicyModal()">
            <Plus :size="14" /> 添加 DNS 策略
          </button>
        </div>

        <div v-if="showInheritedDNS && status.inherited_dns_policies?.length" class="inherited-dns-box">
          <div v-for="item in status.inherited_dns_policies" :key="item.domain" class="dns-policy-row readonly">
            <span class="rule-value mono-text">{{ item.domain }}</span>
            <ArrowRight :size="13" class="rule-arrow" aria-hidden="true" />
            <span class="dns-servers mono-text">{{ item.servers.join(', ') }}</span>
          </div>
        </div>

        <div v-if="status.custom_dns_policies?.length" class="rules-list">
          <div
            v-for="item in status.custom_dns_policies"
            :key="item.id"
            class="rule-row"
            :class="{ disabled: !item.enabled }"
          >
            <div class="rule-main">
              <span class="badge badge-brand">增量</span>
              <span class="rule-value mono-text">{{ item.domain }}</span>
              <ArrowRight :size="14" class="rule-arrow" aria-hidden="true" />
              <span class="dns-servers mono-text">{{ (item.servers || []).join(', ') }}</span>
            </div>
            <div class="rule-actions">
              <label class="switch" :aria-label="`启用 DNS 策略 ${item.domain}`">
                <input
                  type="checkbox"
                  :checked="item.enabled"
                  :disabled="savingDNSPolicies"
                  @change="toggleDNSPolicy(item.id)"
                />
                <span class="switch-slider" />
              </label>
              <button
                type="button"
                class="btn btn-ghost btn-sm text-bad"
                :disabled="savingDNSPolicies"
                :aria-label="`删除 DNS 策略 ${item.domain}`"
                @click="removeDNSPolicy(item.id)"
              >
                <Trash2 :size="14" />
              </button>
            </div>
          </div>
        </div>
        <p v-else-if="!status.inherited_dns_policies?.length" class="report-empty">暂无自定义 DNS 策略</p>
      </SpotlightCard>
    </section>

    <!-- Tab 4: Ports, Core Overrides, LAN Auth, Zashboard & Core Binary -->
    <section v-show="activeTab === 'settings'" class="tab-section settings-tab-grid" role="tabpanel" aria-label="端口与核心">
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
              <span>DNS 监听端口</span>
              <input
                v-model.number="settingsDraft.settings.dns_listen_port"
                name="dns_listen_port"
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

          <div class="auth-section">
            <label class="toggle-card">
              <span>启用局域网代理认证 (authentication)</span>
              <input v-model="settingsDraft.settings.auth_enabled" name="auth_enabled" type="checkbox" />
            </label>
            <div v-if="settingsDraft.settings.auth_enabled" class="auth-fields-grid">
              <label class="form-field">
                <span>代理账号</span>
                <input
                  v-model.trim="settingsDraft.settings.auth_username"
                  name="auth_username"
                  type="text"
                  autocomplete="off"
                  class="form-input mono-text"
                  placeholder="例如 proxyuser"
                  required
                />
              </label>
              <label class="form-field">
                <span>代理密码</span>
                <input
                  v-model="settingsDraft.settings.auth_password"
                  name="auth_password"
                  type="password"
                  autocomplete="new-password"
                  class="form-input mono-text"
                  :placeholder="status?.auth_password_configured ? '已配置，留空保持不变' : '请输入密码'"
                />
              </label>
            </div>
            <p v-if="settingsDraft.settings.auth_enabled" class="auth-hint">
              仅校验局域网接入；本机 127.0.0.1 出站联动自动免密
            </p>
          </div>

          <div class="selects-grid">
            <label class="form-field">
              <span>DNS 增强模式</span>
              <select v-model="settingsDraft.settings.dns_enhanced_mode" name="dns_enhanced_mode" class="form-input">
                <option value="keep">沿用订阅原配置 (keep)</option>
                <option value="fake-ip">fake-ip</option>
                <option value="redir-host">redir-host</option>
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

      <!-- Zashboard Dashboard Card -->
      <SpotlightCard v-if="status" class="report-panel">
        <div class="panel-header-row">
          <h2>Zashboard 面板</h2>
          <div class="report-actions">
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              :disabled="zashboardBusy"
              @click="downloadZashboard(false)"
            >
              <Download :size="14" /> {{ zashboardBusy ? '下载中…' : (status.zashboard_installed ? '更新面板' : '在线下载面板') }}
            </button>
            <button
              type="button"
              class="btn btn-primary btn-sm"
              :disabled="zashboardBusy"
              @click="openZashboard"
            >
              <ExternalLink :size="14" /> 一键打开面板
            </button>
          </div>
        </div>
        <dl class="core-meta">
          <div>
            <dt>面板状态</dt>
            <dd>{{ status.zashboard_installed ? '已就绪' : '未安装' }}</dd>
          </div>
          <div>
            <dt>当前版本</dt>
            <dd class="mono-text">{{ status.zashboard_version || '—' }}</dd>
          </div>
          <div>
            <dt>受保护入口</dt>
            <dd class="mono-text">/zashboard/ · /api/mihomo/ctrl</dd>
          </div>
        </dl>
      </SpotlightCard>

      <!-- Core Binary Management Card -->
      <SpotlightCard v-if="status" class="report-panel">
        <div class="panel-header-row">
          <h2>Mihomo 内核</h2>
          <div class="report-actions">
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              :disabled="checkingCoreUpdate || coreBusy"
              @click="checkCoreUpdate"
            >
              <RefreshCw :size="14" :class="{ spinning: checkingCoreUpdate }" />
              {{ checkingCoreUpdate ? '检查中…' : '检查更新' }}
            </button>
            <button type="button" class="btn btn-secondary btn-sm" :disabled="coreBusy" @click="triggerUpload">
              <Upload :size="14" /> 上传内核文件
            </button>
            <button type="button" class="btn btn-primary btn-sm" :disabled="coreBusy" @click="downloadCore">
              <Download :size="14" />
              {{ coreBusy ? '处理中…' : status.core_update?.update_available ? '立即更新内核' : '在线下载内核' }}
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
            <dt>最新版本</dt>
            <dd class="mono-text">
              <template v-if="status.core_update?.latest_version">
                {{ status.core_update.latest_version }}
                <span class="badge" :class="status.core_update.update_available ? 'badge-warn' : 'badge-ok'">
                  {{ status.core_update.update_available ? '有新版本' : '已是最新' }}
                </span>
              </template>
              <span v-else>未检查</span>
            </dd>
          </div>
          <div>
            <dt>执行路径</dt>
            <dd class="mono-text">{{ status.binary_path || '—' }}</dd>
          </div>
        </dl>
      </SpotlightCard>

      <!-- GeoIP (country.mmdb) Card -->
      <SpotlightCard v-if="status" class="report-panel">
        <div class="panel-header-row">
          <h2>GeoIP 数据库 (country.mmdb)</h2>
          <div class="report-actions">
            <button type="button" class="btn btn-secondary btn-sm" :disabled="mmdbBusy" @click="triggerMMDBUpload">
              <Upload :size="14" /> 上传 country.mmdb
            </button>
            <button type="button" class="btn btn-primary btn-sm" :disabled="mmdbBusy" @click="downloadMMDB">
              <Download :size="14" /> {{ mmdbBusy ? '下载中…' : '镜像在线下载' }}
            </button>
            <input
              ref="mmdbFileInput"
              type="file"
              accept=".mmdb,application/octet-stream"
              class="sr-only"
              @change="handleMMDBUpload"
            />
          </div>
        </div>
        <dl class="core-meta">
          <div>
            <dt>安装状态</dt>
            <dd>{{ status.mmdb_installed ? '已就绪' : '未安装（首次启动将自动从镜像下载）' }}</dd>
          </div>
          <div>
            <dt>文件大小</dt>
            <dd class="mono-text">{{ status.mmdb_size ? formatBytes(status.mmdb_size) : '—' }}</dd>
          </div>
          <div>
            <dt>更新时间</dt>
            <dd class="mono-text">{{ status.mmdb_updated_at ? formatTime(status.mmdb_updated_at) : '—' }}</dd>
          </div>
        </dl>
      </SpotlightCard>
    </section>

    <!-- Tab 5: Config YAML Dual View -->
    <section v-show="activeTab === 'yaml'" class="tab-section" role="tabpanel" aria-label="配置文件">
      <SpotlightCard
        class="report-panel yaml-panel"
        :class="{ fullscreen: yamlFullscreen }"
        @keydown.esc="yamlFullscreen && (yamlFullscreen = false)"
      >
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
                :class="{ active: yamlKind === 'custom' }"
                @click="loadYaml('custom')"
              >
                自定义配置 (custom.yaml)
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
            <span
              v-if="yamlKind === 'runtime'"
              class="badge"
              :class="yamlManualOverride ? 'badge-warn' : 'badge-ok'"
              :title="yamlManualOverride ? '当前为手动修改版本；后续刷新订阅或修改设置会按流水线重新合成并覆盖手改内容' : ''"
            >
              {{ yamlManualOverride ? '已手动编辑 · 刷新订阅将重合成' : '流水线自动合成' }}
            </span>
            <span v-else-if="yamlKind === 'custom'" class="badge badge-brand">
              合并前自定义总配置 · 订阅更新不覆盖
            </span>
            <span v-else class="badge badge-neutral">只读原件</span>
          </div>

          <div class="report-actions yaml-actions">
            <button
              type="button"
              class="btn btn-ghost btn-sm"
              :aria-pressed="yamlWrap"
              @click="yamlWrap = !yamlWrap"
            >
              <WrapText :size="13" /> {{ yamlWrap ? '不换行' : '自动换行' }}
            </button>
            <button
              v-if="yamlKind !== 'source'"
              type="button"
              class="btn btn-ghost btn-sm"
              :aria-pressed="yamlPreviewMode"
              @click="yamlPreviewMode = !yamlPreviewMode"
            >
              <Eye :size="13" /> {{ yamlPreviewMode ? '编辑代码' : '高亮预览' }}
            </button>
            <button
              type="button"
              class="btn btn-ghost btn-sm"
              :aria-pressed="yamlFullscreen"
              @click="yamlFullscreen = !yamlFullscreen"
            >
              <component :is="yamlFullscreen ? Minimize2 : Maximize2" :size="13" />
              {{ yamlFullscreen ? '退出全屏' : '全屏' }}
            </button>
            <template v-if="yamlKind === 'runtime'">
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                :disabled="yamlBusy"
                @click="formatYaml"
              >
                <WandSparkles :size="13" /> 格式化
              </button>
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                :disabled="yamlBusy"
                @click="validateYaml"
              >
                <CheckCircle2 :size="13" /> 校验配置
              </button>
              <button type="button" class="btn btn-secondary btn-sm" :disabled="yamlBusy" @click="resyncYaml">
                <RotateCw :size="13" /> 重新合成
              </button>
              <button
                type="button"
                class="btn btn-primary btn-sm"
                :disabled="yamlBusy || (yamlDirty && !yamlValidated)"
                :title="yamlDirty && !yamlValidated ? '请先点击「校验配置」通过后再保存' : ''"
                @click="saveYaml"
              >
                <Save :size="13" /> {{ yamlBusy ? '保存中…' : '保存并热重载' }}
              </button>
            </template>
            <template v-else-if="yamlKind === 'custom'">
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                :disabled="yamlBusy"
                @click="loadYaml('custom')"
              >
                <RefreshCw :size="13" /> 重新载入
              </button>
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                :disabled="yamlBusy"
                @click="validateYaml"
              >
                <CheckCircle2 :size="13" /> 校验配置
              </button>
              <button
                type="button"
                class="btn btn-primary btn-sm"
                :disabled="yamlBusy || !yamlDirty"
                @click="saveYaml"
              >
                <Save :size="13" /> {{ yamlBusy ? '保存中…' : '保存并生效' }}
              </button>
            </template>
          </div>
        </div>

        <div
          v-if="yamlKind !== 'source' && yamlValidation"
          class="yaml-validation-banner"
          :class="yamlValidation.valid ? 'valid' : 'invalid'"
          role="status"
        >
          <template v-if="yamlValidation.valid">
            <span v-if="yamlValidation.summary">
              校验通过{{ yamlValidation.core_tested ? '（已通过 mihomo -t 内核校验）' : '' }} ·
              {{ yamlValidation.summary?.proxies || 0 }} 节点 /
              {{ yamlValidation.summary?.groups || 0 }} 策略组 /
              {{ yamlValidation.summary?.rules || 0 }} 规则
            </span>
            <span v-else>校验与预合并通过，点击「保存并生效」即可写入并同步到各管理标签页</span>
            <span v-if="yamlValidation.warnings?.length" class="yaml-warnings">
              · {{ yamlValidation.warnings.join('；') }}
            </span>
          </template>
          <template v-else>
            <span class="yaml-error-banner-text">{{ yamlValidation.error }}</span>
            <button
              v-if="yamlErrorLine"
              type="button"
              class="yaml-locate-btn"
              @click="yamlCodeEditorRef?.jumpToLine(yamlErrorLine)"
            >
              定位第 {{ yamlErrorLine }} 行
            </button>
          </template>
        </div>
        <div v-else-if="yamlKind === 'runtime' && yamlDirty" class="yaml-validation-banner pending">
          配置已修改，请先点击「校验配置」通过后再保存
        </div>

        <div v-if="yamlKind === 'source' || yamlPreviewMode" class="yaml-viewer mono-text" :class="{ wrap: yamlWrap }" aria-readonly="true">
          <div class="yaml-code-table">
            <div v-for="row in highlightedYamlRows" :key="row.num" class="yaml-code-line">
              <span class="yaml-line-num">{{ row.num }}</span>
              <span class="yaml-line-code">
                <template v-for="(tok, idx) in row.tokens" :key="idx"><span :class="tok.cls">{{ tok.text }}</span></template>
                <template v-if="row.tokens.length === 0">&nbsp;</template>
              </span>
            </div>
          </div>
        </div>

        <div v-else class="yaml-editor-shell">
          <YamlCodeEditor
            ref="yamlCodeEditorRef"
            v-model="yamlContent"
            editor-class="yaml-editor"
            height-preset="tall"
            aria-label="Mihomo YAML 配置内容"
            :wrap="yamlWrap"
            :error-message="yamlValidation && !yamlValidation.valid ? yamlValidation.error : ''"
            @input="onYamlInput"
          />
        </div>
      </SpotlightCard>
    </section>

    <!-- Tab 6: Mihomo Runtime Logs -->
    <section v-show="activeTab === 'logs'" class="tab-section" role="tabpanel" aria-label="运行日志">
      <div class="section-toolbar">
        <div class="toolbar-meta">
          <span class="badge" :class="logsRunning ? 'badge-ok' : 'badge-neutral'">
            {{ logsRunning ? '运行中' : '已停止' }}
          </span>
          <span class="meta-pill">{{ filteredLogLines.length }} 条日志</span>
          <input
            v-model.trim="logSearch"
            type="search"
            class="form-input toolbar-input"
            aria-label="筛选运行日志"
            placeholder="筛选日志关键字…"
          />
        </div>
        <div class="report-actions">
          <label class="log-live-toggle">
            <span>实时刷新</span>
            <span class="switch">
              <input v-model="logsLive" type="checkbox" aria-label="实时刷新运行日志" @change="syncLogPolling" />
              <span class="switch-slider" />
            </span>
          </label>
          <button type="button" class="btn btn-secondary btn-sm" :disabled="logsLoading" @click="loadLogs(false)">
            <RefreshCw :size="13" /> 刷新
          </button>
          <button type="button" class="btn btn-secondary btn-sm text-bad" :disabled="logsLoading" @click="clearLogs">
            <Trash2 :size="13" /> 清理
          </button>
        </div>
      </div>

      <SpotlightCard class="report-panel log-panel">
        <p v-if="logsError" class="report-warning" role="status">{{ logsError }}</p>
        <div ref="logViewerEl" class="log-viewer mono-text" role="log" aria-live="off">
          <template v-if="filteredLogLines.length">
            <div v-for="entry in filteredLogLines" :key="entry.num" class="log-line" :class="entry.levelClass">
              <span class="log-num">{{ entry.num }}</span>
              <span class="log-text">{{ entry.text }}</span>
            </div>
          </template>
          <p v-else class="report-empty">{{ logLines.length ? '无匹配日志' : '暂无 Mihomo 运行日志' }}</p>
        </div>
      </SpotlightCard>
    </section>

    <!-- Dialog: Create or Edit Custom Proxy Group -->
    <dialog ref="groupDialog" class="modal-dialog" aria-labelledby="mihomo-group-dialog-title">
      <div class="modal-header">
        <h2 id="mihomo-group-dialog-title" class="modal-title">{{ groupForm.id ? '编辑自定义策略组' : '新建自定义策略组' }}</h2>
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
              <option value="smart">智能优选 (smart)</option>
              <option value="relay">链式代理 (relay)</option>
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
              <option value="RULE-SET">RULE-SET（规则集）</option>
              <option value="MATCH">MATCH（兜底规则）</option>
            </select>
          </label>
          <label class="form-field">
            <span>目标策略组</span>
            <select v-model="ruleForm.target" name="rule_target" class="form-input" required>
              <option v-for="t in ruleTargetOptions" :key="t" :value="t">{{ t }}</option>
            </select>
          </label>
        </div>
        <label v-if="ruleForm.type !== 'MATCH'" class="form-field">
          <span>{{ ruleForm.type === 'RULE-SET' ? '规则集名称' : '匹配值' }}</span>
          <input
            v-model.trim="ruleForm.value"
            name="rule_value"
            class="form-input mono-text"
            :list="ruleForm.type === 'RULE-SET' ? 'mihomo-ruleset-names' : undefined"
            required
            :placeholder="ruleForm.type === 'RULE-SET' ? '选择或输入已配置的规则集名称' : '例如：openai.com 或 192.168.1.0/24'"
          />
          <datalist id="mihomo-ruleset-names">
            <option v-for="rs in status?.custom_rule_sets || []" :key="rs.id" :value="rs.name" />
          </datalist>
        </label>
        <p v-else class="form-hint">MATCH 规则将匹配所有未命中上方规则的剩余流量，并自动添加至自定义分流列表底部。</p>
        <label class="form-field">
          <span>备注 (可选)</span>
          <input v-model.trim="ruleForm.comment" name="rule_comment" class="form-input" maxlength="256" />
        </label>
        <label v-if="['IP-CIDR', 'IP-CIDR6', 'SRC-IP-CIDR', 'GEOIP', 'RULE-SET'].includes(ruleForm.type)" class="toggle-card">
          <span>跳过 DNS 解析 (no-resolve)</span>
          <input v-model="ruleForm.no_resolve" type="checkbox" />
        </label>
      </form>
      <div class="modal-footer">
        <button type="button" class="btn btn-secondary" @click="ruleDialog?.close()">取消</button>
        <button type="submit" form="mihomo-rule-form" class="btn btn-primary" :disabled="modalBusy">
          {{ ruleForm.type === 'MATCH' ? '添加至底部' : '添加至顶部' }}
        </button>
      </div>
    </dialog>

    <!-- Dialog: Add or Edit Subscription -->
    <dialog ref="subDialog" class="modal-dialog" aria-labelledby="mihomo-sub-dialog-title">
      <div class="modal-header">
        <h2 id="mihomo-sub-dialog-title" class="modal-title">{{ subForm.id ? '编辑订阅与节点筛选' : '添加代理订阅' }}</h2>
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
          <input
            v-model.trim="subForm.url"
            name="sub_url"
            type="url"
            class="form-input mono-text"
            :placeholder="subForm.id ? '留空保留当前链接' : 'https://example.com/sub?...'"
          />
        </label>
        <label v-if="!subForm.id" class="form-field">
          <span>或直接粘贴 Clash YAML 配置</span>
          <textarea v-model="subForm.yaml_content" name="sub_yaml" class="form-input mono-text" rows="4" placeholder="留空则从上方 URL 下载" />
        </label>
        <div class="ports-grid">
          <label class="form-field">
            <span>保留节点正则 (仅保留匹配，可选)</span>
            <input
              v-model.trim="subForm.include_filter"
              name="sub_include_filter"
              class="form-input mono-text"
              placeholder="例如：HK|JP|SG|专线"
            />
          </label>
          <label class="form-field">
            <span>排除节点正则 (排除匹配，可选)</span>
            <input
              v-model.trim="subForm.exclude_filter"
              name="sub_exclude_filter"
              class="form-input mono-text"
              placeholder="例如：剩余流量|套餐|到期|官网"
            />
          </label>
        </div>
        <label class="form-field">
          <span>节点名称统一后缀 (可选)</span>
          <input
            v-model="subForm.node_suffix"
            name="sub_node_suffix"
            class="form-input mono-text"
            maxlength="64"
            placeholder="例如： [主力]（注意可保留前导空格）"
          />
        </label>
        <label class="toggle-card">
          <div class="toggle-card-copy">
            <span>完全排除机场策略组与规则配置</span>
            <small>开启后仅导入机场节点（proxies），忽略订阅自带的 proxy-groups、rules 与 rule-providers，完全使用自定义配置。</small>
          </div>
          <input v-model="subForm.exclude_groups_and_rules" name="sub_exclude_groups_and_rules" type="checkbox" />
        </label>
      </form>
      <div class="modal-footer">
        <button type="button" class="btn btn-secondary" @click="subDialog?.close()">取消</button>
        <button type="submit" form="mihomo-sub-form" class="btn btn-primary" :disabled="modalBusy">
          {{ modalBusy ? '处理中…' : subForm.id ? '保存修改' : '保存订阅' }}
        </button>
      </div>
    </dialog>

    <!-- Dialog: Add Custom DNS Policy -->
    <dialog ref="dnsDialog" class="modal-dialog" aria-labelledby="mihomo-dns-dialog-title">
      <div class="modal-header">
        <h2 id="mihomo-dns-dialog-title" class="modal-title">添加增量 DNS 策略</h2>
        <button type="button" class="close-modal-btn" aria-label="关闭" @click="dnsDialog?.close()">
          <X :size="18" />
        </button>
      </div>
      <form id="mihomo-dns-form" class="modal-body mihomo-modal-form" @submit.prevent="submitDNSPolicy">
        <p v-if="modalError" class="report-error" role="alert">{{ modalError }}</p>
        <label class="form-field">
          <span>匹配域名 / 通配符 / Geosite</span>
          <input
            v-model.trim="dnsForm.domain"
            name="dns_domain"
            class="form-input mono-text"
            required
            placeholder="例如：+.corp.internal 或 geosite:cn"
          />
        </label>
        <label class="form-field">
          <span>指定 DNS 服务器（每行一个或逗号分隔）</span>
          <textarea
            v-model="dnsForm.serversText"
            name="dns_servers"
            class="form-input mono-text"
            rows="3"
            required
            placeholder="10.0.0.53&#10;https://dns.example.com/dns-query#专线"
          />
        </label>
      </form>
      <div class="modal-footer">
        <button type="button" class="btn btn-secondary" @click="dnsDialog?.close()">取消</button>
        <button type="submit" form="mihomo-dns-form" class="btn btn-primary" :disabled="modalBusy">保存 DNS 策略</button>
      </div>
    </dialog>
  </div>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import {
  Activity,
  ArrowDown,
  ArrowRight,
  ArrowUp,
  CheckCircle2,
  ChevronDown,
  Copy,
  Download,
  ExternalLink,
  Eye,
  FileCode2,
  GitBranch,
  Layers,
  LayoutDashboard,
  Maximize2,
  Minimize2,
  Pencil,
  Play,
  Plus,
  RefreshCw,
  RotateCw,
  Save,
  ScrollText,
  Sliders,
  Square,
  Trash2,
  Upload,
  WandSparkles,
  WrapText,
  X,
  Zap
} from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import YamlCodeEditor from '@/components/YamlCodeEditor.vue'
import MihomoCustomYaml from '@/components/MihomoCustomYaml.vue'
import MihomoConnections from '@/components/MihomoConnections.vue'
import MihomoRuleSets from '@/components/MihomoRuleSets.vue'
import MihomoOverview from '@/components/MihomoOverview.vue'
import { adminApi } from '@/api/admin'
import { copyText as copyToClipboard } from '@/api/clipboard'
import { formatBytes } from '@/api/reports'
import { parseYamlErrorLocations, tokenizeYamlLine } from '@/api/yamlEditor'
import { useToast } from '@/composables/useToast'
import '@/styles/reports.css'

const toast = useToast()
const controller = new AbortController()
let yamlRequestSeq = 0
let yamlAbort = null

const modes = [
  { id: 'rule', label: '规则' },
  { id: 'global', label: '全局' },
  { id: 'direct', label: '直连' }
]

const tabs = [
  { id: 'overview', label: '概览', icon: LayoutDashboard },
  { id: 'groups', label: '策略组与节点', icon: Layers },
  { id: 'rules', label: '自定义分流', icon: GitBranch },
  { id: 'connections', label: '连接', icon: Activity },
  { id: 'subscriptions', label: '订阅管理', icon: Download },
  { id: 'settings', label: '端口与核心', icon: Sliders },
  { id: 'yaml', label: '配置文件', icon: FileCode2 },
  { id: 'logs', label: '运行日志', icon: ScrollText }
]

const status = ref(null)
const settingsDraft = ref(null)
const loading = ref(false)
const busyAction = ref(false)
const savingSettings = ref(false)
const savingRules = ref(false)
const savingDNSPolicies = ref(false)
const showInheritedDNS = ref(false)
const busySub = ref('')
const coreBusy = ref(false)
const checkingCoreUpdate = ref(false)
const mmdbBusy = ref(false)
const zashboardBusy = ref(false)
const testingGroup = ref('')
const testingNode = ref('')
const error = ref('')
const builtinNodeNames = new Set(['DIRECT', 'REJECT', 'PASS', 'COMPATIBLE'])

const activeTab = ref('overview')
const groupsView = ref('list')
const rulesView = ref('list')
const ruleSetsRef = ref(null)
const ruleSetToolbar = ref({ updatingAll: false, hasUpdatable: false })
const groups = ref([])
const delays = ref({})
const allNodes = ref([])
const groupsLive = ref(false)
const expandedGroups = reactive({})

const ruleSearch = ref('')
const ruleTargetFilter = ref('')

const yamlCodeEditorRef = ref(null)
const yamlKind = ref('runtime')
const yamlContent = ref('')
const yamlManualOverride = ref(false)
const yamlBusy = ref(false)
const yamlDirty = ref(false)
const yamlValidated = ref(false)
const yamlValidation = ref(null)
const yamlWrap = ref(false)
const yamlFullscreen = ref(false)
const yamlPreviewMode = ref(false)
const yamlErrorLine = computed(() => {
  if (!yamlValidation.value || yamlValidation.value.valid || !yamlValidation.value.error) return null
  return parseYamlErrorLocations(yamlValidation.value.error, yamlContent.value)[0]?.line ?? null
})

const logLines = ref([])
const logsRunning = ref(false)
const logsError = ref('')
const logsLoading = ref(false)
const logsLive = ref(true)
const logSearch = ref('')
const logViewerEl = ref(null)
let logPollTimer = null

const groupDialog = ref(null)
const ruleDialog = ref(null)
const subDialog = ref(null)
const dnsDialog = ref(null)
const coreFileInput = ref(null)
const mmdbFileInput = ref(null)
const modalError = ref('')
const modalBusy = ref(false)

const groupForm = reactive({
  id: '',
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
  id: '',
  name: '',
  url: '',
  yaml_content: '',
  include_filter: '',
  exclude_filter: '',
  node_suffix: '',
  exclude_groups_and_rules: false
})

const dnsForm = reactive({
  domain: '',
  serversText: ''
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
  if (s.dns_listen_port > 0) entries.push({ label: 'DNS', address: `${host}:${s.dns_listen_port}` })
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
      (r.value || '').toLowerCase().includes(q) ||
      r.type.toLowerCase().includes(q) ||
      r.target.toLowerCase().includes(q) ||
      (r.type === 'MATCH' && '全部剩余流量（兜底）'.toLowerCase().includes(q)) ||
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
    settings: {
      ...data.settings,
      auth_password: ''
    }
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
  if (kind !== yamlKind.value) {
    yamlContent.value = ''
  }
  yamlKind.value = kind
  yamlDirty.value = false
  yamlValidated.value = false
  yamlValidation.value = null
  if (kind === 'source') {
    yamlPreviewMode.value = false
  }
  const seq = ++yamlRequestSeq
  if (yamlAbort) {
    yamlAbort.abort()
  }
  const currentAbort = new AbortController()
  yamlAbort = currentAbort
  yamlBusy.value = true
  try {
    const res = kind === 'custom'
      ? await adminApi.mihomoCustomYaml('all', currentAbort.signal)
      : await adminApi.mihomoYaml(kind, currentAbort.signal)
    if (controller.signal.aborted || seq !== yamlRequestSeq) return
    yamlContent.value = res.content || ''
    yamlManualOverride.value = !!res.manual_override
  } catch (err) {
    if (!controller.signal.aborted && seq === yamlRequestSeq && err?.name !== 'AbortError') {
      toast.error(err.message)
    }
  } finally {
    if (seq === yamlRequestSeq) {
      yamlBusy.value = false
    }
  }
}

async function onCustomYamlSaved(nextStatus) {
  if (nextStatus) applyStatus(nextStatus)
  await loadGroups()
}

async function switchTab(tabId) {
  activeTab.value = tabId
  if (tabId === 'groups') await loadGroups()
  if (tabId === 'yaml') await loadYaml(yamlKind.value)
  if (tabId === 'logs') {
    await loadLogs(false)
    syncLogPolling()
  } else {
    stopLogPolling()
  }
}

async function handleOverviewNavigate(targetTab) {
  if (targetTab === 'yaml') {
    yamlKind.value = 'runtime'
  }
  await switchTab(targetTab)
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
  expandedGroups[groupName] = true
  try {
    const res = await adminApi.testMihomoGroupDelay(groupName)
    const nextDelays = res.delays || {}
    delays.value = { ...delays.value, ...nextDelays }
    const entries = Object.values(nextDelays)
    const okCount = entries.filter((v) => v > 0).length
    const timeoutCount = entries.filter((v) => v === 0).length
    if (entries.length === 0) {
      toast.info(`「${groupName}」无可测速节点`)
    } else if (okCount === 0) {
      toast.warn(`测速完成：${timeoutCount} 个节点均超时`)
    } else {
      toast.success(`测速完成：${okCount} 个可用${timeoutCount ? `，${timeoutCount} 个超时` : ''}`)
    }
  } catch (err) {
    toast.error(err.message)
  } finally {
    testingGroup.value = ''
  }
}

async function testSingleNodeDelay(nodeName) {
  testingNode.value = nodeName
  try {
    const res = await adminApi.testMihomoNodeDelay(nodeName)
    const delay = Number(res.delay) || 0
    delays.value = { ...delays.value, [nodeName]: delay }
    if (delay > 0) {
      toast.success(`${nodeName}: ${delay} ms`)
    } else {
      toast.warn(`${nodeName}: 测速超时`)
    }
  } catch (err) {
    toast.error(err.message)
  } finally {
    testingNode.value = ''
  }
}

function openGroupModal(group = null) {
  modalError.value = ''
  const custom = group?.group_id
    ? (status.value?.custom_groups || []).find((item) => item.id === group.group_id)
    : null
  groupForm.id = custom?.id || ''
  groupForm.name = custom?.name || ''
  groupForm.type = custom?.type || 'select'
  groupForm.filter = custom?.filter || ''
  groupForm.url = custom?.url || 'https://www.gstatic.com/generate_204'
  groupForm.interval = custom?.interval || 300
  groupForm.proxies = custom?.proxies ? [...custom.proxies] : []
  groupDialog.value?.showModal()
}

async function submitCustomGroup() {
  modalError.value = ''
  modalBusy.value = true
  try {
    const data = await adminApi.saveMihomoGroup({
      id: groupForm.id || undefined,
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
    toast.success(groupForm.id ? '自定义策略组已更新' : '自定义策略组已保存')
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
      value: ruleForm.type === 'MATCH' ? '' : ruleForm.value,
      target: ruleForm.target,
      no_resolve: ruleForm.type === 'MATCH' ? false : ruleForm.no_resolve,
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
        socks_port: Number(settingsDraft.value.settings.socks_port) || 0,
        dns_listen_port: Number(settingsDraft.value.settings.dns_listen_port) || 0,
        auth_enabled: !!settingsDraft.value.settings.auth_enabled,
        auth_username: (settingsDraft.value.settings.auth_username || '').trim(),
        auth_password: settingsDraft.value.settings.auth_password || ''
      }
    }
    const data = await adminApi.saveMihomoSettings(payload)
    applyStatus(data)
    await loadGroups()
    toast.success('代理端口与核心设置已保存并合成 runtime.yaml')
  } catch (err) {
    error.value = err.message
    toast.error(err.message)
  } finally {
    savingSettings.value = false
  }
}

function buildZashboardURL() {
  const isHttps = window.location.protocol === 'https:'
  const protocol = isHttps ? 'https' : 'http'
  const hostname = window.location.hostname
  const port = window.location.port || (isHttps ? '443' : '80')
  const params = new URLSearchParams({
    protocol,
    hostname,
    port,
    secondaryPath: '/api/mihomo/ctrl',
    secret: '',
    label: 'GateHome',
    disableUpgradeCore: '1',
    disableTunMode: '1'
  })
  return `/zashboard/#/setup?${params.toString()}`
}

async function downloadZashboard(openAfter = false) {
  zashboardBusy.value = true
  try {
    const res = await adminApi.downloadMihomoZashboard()
    if (res.status) applyStatus(res.status)
    toast.success(`Zashboard 面板已就绪${res.version ? `: ${res.version}` : ''}`)
    if (openAfter) {
      const win = window.open(buildZashboardURL(), '_blank', 'noopener,noreferrer')
      if (!win) {
        toast.info('Zashboard 已就绪，请再次点击「Zashboard 面板」打开')
      }
    }
  } catch (err) {
    toast.error(err.message)
  } finally {
    zashboardBusy.value = false
  }
}

async function openZashboard() {
  if (status.value?.zashboard_installed) {
    window.open(buildZashboardURL(), '_blank', 'noopener,noreferrer')
    return
  }
  await downloadZashboard(true)
}

function openSubModal(sub = null) {
  modalError.value = ''
  subForm.id = sub?.id || ''
  subForm.name = sub?.name || ''
  subForm.url = ''
  subForm.yaml_content = ''
  subForm.include_filter = sub?.include_filter || ''
  subForm.exclude_filter = sub?.exclude_filter || ''
  subForm.node_suffix = sub?.node_suffix || ''
  subForm.exclude_groups_and_rules = !!sub?.exclude_groups_and_rules
  subDialog.value?.showModal()
}

async function submitSubscription() {
  modalError.value = ''
  modalBusy.value = true
  try {
    const isEdit = !!subForm.id
    const data = isEdit
      ? await adminApi.updateMihomoSubscription(subForm.id, {
          name: subForm.name,
          url: subForm.url,
          include_filter: subForm.include_filter,
          exclude_filter: subForm.exclude_filter,
          node_suffix: subForm.node_suffix,
          exclude_groups_and_rules: subForm.exclude_groups_and_rules
        })
      : await adminApi.addMihomoSubscription({
          name: subForm.name,
          url: subForm.url,
          yaml_content: subForm.yaml_content,
          include_filter: subForm.include_filter,
          exclude_filter: subForm.exclude_filter,
          node_suffix: subForm.node_suffix,
          exclude_groups_and_rules: subForm.exclude_groups_and_rules
        })
    applyStatus(data)
    subDialog.value?.close()
    await loadGroups()
    toast.success(isEdit ? '订阅设置已更新' : '订阅已添加')
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

function openDNSPolicyModal() {
  modalError.value = ''
  dnsForm.domain = ''
  dnsForm.serversText = ''
  dnsDialog.value?.showModal()
}

async function submitDNSPolicy() {
  modalError.value = ''
  const servers = dnsForm.serversText
    .split(/[\n,]+/)
    .map((s) => s.trim())
    .filter(Boolean)
  modalBusy.value = true
  try {
    const data = await adminApi.addMihomoDNSPolicy({
      domain: dnsForm.domain,
      servers,
      enabled: true
    })
    applyStatus(data)
    dnsDialog.value?.close()
    toast.success('增量 DNS 策略已添加')
  } catch (err) {
    modalError.value = err.message
  } finally {
    modalBusy.value = false
  }
}

async function toggleDNSPolicy(id) {
  savingDNSPolicies.value = true
  try {
    const next = (status.value?.custom_dns_policies || []).map((p) =>
      p.id === id ? { ...p, enabled: !p.enabled } : p
    )
    const data = await adminApi.saveMihomoDNSPolicies(next)
    applyStatus(data)
  } catch (err) {
    toast.error(err.message)
  } finally {
    savingDNSPolicies.value = false
  }
}

async function removeDNSPolicy(id) {
  savingDNSPolicies.value = true
  try {
    const data = await adminApi.deleteMihomoDNSPolicy(id)
    applyStatus(data)
    toast.success('DNS 策略已删除')
  } catch (err) {
    toast.error(err.message)
  } finally {
    savingDNSPolicies.value = false
  }
}

async function checkCoreUpdate() {
  checkingCoreUpdate.value = true
  try {
    const res = await adminApi.checkMihomoCoreUpdate()
    if (res.status) applyStatus(res.status)
    const info = res.core_update
    if (info?.update_available) {
      toast.info(`检测到新版本 ${info.latest_version}`)
    } else if (info?.latest_version) {
      toast.success(`当前内核已是最新版本 (${info.latest_version})`)
    } else {
      toast.info('已完成内核版本检查')
    }
  } catch (err) {
    toast.error(err.message)
  } finally {
    checkingCoreUpdate.value = false
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

async function downloadMMDB() {
  mmdbBusy.value = true
  try {
    const res = await adminApi.downloadMihomoMMDB()
    if (res.status) applyStatus(res.status)
    toast.success(`country.mmdb 已从 ${res.source || '镜像源'} 下载就绪`)
  } catch (err) {
    toast.error(err.message)
  } finally {
    mmdbBusy.value = false
  }
}

function triggerMMDBUpload() {
  mmdbFileInput.value?.click()
}

async function handleMMDBUpload(event) {
  const file = event.target.files?.[0]
  event.target.value = ''
  if (!file) return
  mmdbBusy.value = true
  try {
    const res = await adminApi.uploadMihomoMMDB(file)
    if (res.status) applyStatus(res.status)
    toast.success(`country.mmdb 已上传 (${formatBytes(res.size || file.size)})`)
  } catch (err) {
    toast.error(err.message)
  } finally {
    mmdbBusy.value = false
  }
}

function onYamlInput() {
  yamlDirty.value = true
  yamlValidated.value = false
  yamlValidation.value = null
}

const highlightedYamlRows = computed(() =>
  (yamlContent.value || '').split('\n').map((line, index) => ({
    num: index + 1,
    tokens: tokenizeYamlLine(line)
  }))
)

async function validateYaml() {
  yamlBusy.value = true
  try {
    if (yamlKind.value === 'custom') {
      await adminApi.saveMihomoCustomYaml('all', yamlContent.value, true)
      yamlValidation.value = { valid: true }
      yamlValidated.value = true
      toast.success('自定义总配置校验与预合并通过')
      return
    }
    const res = await adminApi.validateMihomoYaml(yamlContent.value)
    yamlValidation.value = res
    yamlValidated.value = !!res.valid
    if (res.valid) {
      toast.success(res.core_tested ? 'YAML 与内核校验均通过' : 'YAML 语法与引用校验通过')
    } else {
      toast.error(res.error || '配置校验未通过')
    }
  } catch (err) {
    yamlValidated.value = false
    yamlValidation.value = { valid: false, error: err.message }
    toast.error(err.message)
  } finally {
    yamlBusy.value = false
  }
}

async function formatYaml() {
  yamlBusy.value = true
  try {
    const res = await adminApi.validateMihomoYaml(yamlContent.value)
    yamlValidation.value = res
    if (res.valid && res.formatted_yaml) {
      yamlContent.value = res.formatted_yaml
      yamlDirty.value = true
      yamlValidated.value = true
      toast.success('已完成 YAML 格式化并校验通过')
    } else {
      yamlValidated.value = false
      toast.error(res.error || 'YAML 无法格式化，请先修复语法错误')
    }
  } catch (err) {
    yamlValidated.value = false
    yamlValidation.value = { valid: false, error: err.message }
    toast.error(err.message)
  } finally {
    yamlBusy.value = false
  }
}

async function saveYaml() {
  if (yamlKind.value === 'custom') {
    yamlBusy.value = true
    try {
      const res = await adminApi.saveMihomoCustomYaml('all', yamlContent.value, false)
      yamlContent.value = res.content || yamlContent.value
      yamlDirty.value = false
      yamlValidated.value = true
      yamlValidation.value = null
      if (res.status) applyStatus(res.status)
      await loadGroups()
      toast.success('自定义配置已保存并合并到运行配置')
    } catch (err) {
      yamlValidated.value = false
      yamlValidation.value = { valid: false, error: err.message }
      toast.error(err.message)
    } finally {
      yamlBusy.value = false
    }
    return
  }
  if (yamlDirty.value && !yamlValidated.value) {
    await validateYaml()
    if (!yamlValidated.value) return
  }
  yamlBusy.value = true
  try {
    const res = await adminApi.saveMihomoYaml(yamlContent.value)
    yamlContent.value = res.content || yamlContent.value
    yamlManualOverride.value = !!res.manual_override
    yamlDirty.value = false
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
    yamlDirty.value = false
    yamlValidated.value = false
    yamlValidation.value = null
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
    await copyToClipboard(text)
    toast.success(msg)
  } catch {
    toast.info(text)
  }
}

function classifyLogLevel(line) {
  const lower = line.toLowerCase()
  if (lower.includes('level=error') || lower.includes('level=fatal')) return 'log-err'
  if (lower.includes('level=warning') || lower.includes('level=warn')) return 'log-warn'
  if (lower.includes('level=debug')) return 'log-debug'
  return 'log-info'
}

const filteredLogLines = computed(() => {
  const q = logSearch.value.toLowerCase()
  const out = []
  for (let i = 0; i < logLines.value.length; i++) {
    const text = logLines.value[i]
    if (q && !text.toLowerCase().includes(q)) continue
    out.push({ num: i + 1, text, levelClass: classifyLogLevel(text) })
  }
  return out
})

let logsInFlight = false

async function loadLogs(silent = false) {
  if (silent && logsInFlight) return
  logsInFlight = true
  if (!silent) logsLoading.value = true
  try {
    const el = logViewerEl.value
    const wasNearBottom = !el || (el.scrollHeight - el.scrollTop - el.clientHeight < 60)
    const res = await adminApi.mihomoLogs(controller.signal)
    if (controller.signal.aborted) return
    logLines.value = res.lines || []
    logsRunning.value = !!res.running
    logsError.value = res.last_error || ''
    if (wasNearBottom) {
      await nextTick()
      if (logViewerEl.value) {
        logViewerEl.value.scrollTop = logViewerEl.value.scrollHeight
      }
    }
  } catch (err) {
    if (!controller.signal.aborted && !silent) toast.error(err.message)
  } finally {
    logsInFlight = false
    if (!silent) logsLoading.value = false
  }
}

async function clearLogs() {
  logsLoading.value = true
  try {
    await adminApi.clearMihomoLogs()
    logLines.value = []
    logsError.value = ''
    toast.success('运行日志已清理')
  } catch (err) {
    toast.error(err.message)
  } finally {
    logsLoading.value = false
  }
}

function stopLogPolling() {
  if (logPollTimer) {
    clearInterval(logPollTimer)
    logPollTimer = null
  }
}

function syncLogPolling() {
  stopLogPolling()
  if (activeTab.value === 'logs' && logsLive.value) {
    logPollTimer = setInterval(() => {
      if (activeTab.value === 'logs' && logsLive.value && !document.hidden) {
        loadLogs(true)
      }
    }, 2000)
  }
}

onMounted(async () => {
  await loadStatus()
  await loadGroups()
})

onBeforeUnmount(() => {
  stopLogPolling()
  if (yamlAbort) yamlAbort.abort()
  controller.abort()
})
</script>

<style scoped>
.mihomo-page {
  gap: 16px;
  grid-template-columns: minmax(0, 1fr);
  width: 100%;
  max-width: 100%;
}

.header-controls {
  gap: 8px;
  min-width: 0;
  max-width: 100%;
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
  width: 100%;
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
  grid-template-columns: minmax(0, 1fr);
  gap: 14px;
  width: 100%;
  min-width: 0;
  max-width: 100%;
}

.section-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 10px;
}

.section-toolbar > .report-actions {
  margin-left: auto;
}

.toolbar-meta {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  min-width: 0;
}

.toolbar-meta .form-input.toolbar-input {
  width: 220px !important;
  flex: 0 1 220px;
}

.meta-pill {
  font-size: 12px;
  color: var(--muted);
}

.groups-stack {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 12px;
  min-width: 0;
}

.group-card {
  padding: 14px 16px;
  min-width: 0;
  max-width: 100%;
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
  flex-wrap: wrap;
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
  gap: 6px;
  padding: 4px 6px 4px 10px;
  border-radius: var(--radius-sm);
  background: var(--strip-bg);
  border: 1px solid var(--line);
  transition: var(--transition-smooth);
  min-width: 0;
}

.node-tile:hover {
  border-color: var(--card-border-hover);
  background: var(--card-hover-bg);
}

.node-tile.selected {
  border-color: var(--brand);
  background: rgba(99, 102, 241, 0.1);
}

.node-select-btn {
  flex: 1 1 auto;
  min-width: 0;
  display: flex;
  align-items: center;
  padding: 5px 4px 5px 0;
  text-align: left;
}

.node-select-btn:disabled {
  cursor: default;
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

.node-delay-btn {
  padding: 4px 7px;
  border-radius: 6px;
  border: 1px solid transparent;
  transition: var(--transition-smooth);
}

.node-delay-btn:not(:disabled):hover {
  border-color: var(--line);
  background: var(--card-bg);
}

.node-delay.ok { color: var(--ok); }
.node-delay.warn { color: var(--warn); }
.node-delay.bad { color: var(--bad); }
.node-delay.muted { color: var(--faint); }

.filter-inputs {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  min-width: 0;
}

.rules-toolbar {
  min-height: 40px;
}

.rules-view-switch {
  flex-shrink: 0;
}

.rules-toolbar-actions {
  margin-left: auto;
  flex: 1 1 280px;
  justify-content: flex-end;
  min-width: 0;
}

.rules-toolbar-actions .filter-inputs {
  flex: 1 1 auto;
  justify-content: flex-end;
}

.spinning {
  animation: mihomo-spin 0.85s linear infinite;
}

@keyframes mihomo-spin {
  to {
    transform: rotate(360deg);
  }
}

.form-input.toolbar-input {
  width: 220px !important;
  max-width: 260px;
  min-width: 140px;
  flex: 0 1 220px;
}

.form-input.toolbar-select {
  width: 165px !important;
  max-width: 190px;
  min-width: 130px;
  flex: 0 1 165px;
}

.rules-panel {
  padding: 14px 16px;
}

.rules-list {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 8px;
  min-width: 0;
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
  min-width: 0;
  max-width: 100%;
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
  flex: 1 1 240px;
}

.rule-value {
  font-size: 12.5px;
  color: var(--ink);
  overflow-wrap: anywhere;
}

.rule-value-muted {
  color: var(--muted);
  font-style: italic;
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
  flex-wrap: wrap;
  gap: 4px;
  flex-shrink: 0;
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
  grid-template-columns: minmax(0, 1fr);
  gap: 16px;
  min-width: 0;
}

.ports-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 12px;
}

.auth-section {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 10px;
  min-width: 0;
}

.auth-fields-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 12px;
}

.auth-hint {
  margin: 0;
  font-size: 11.5px;
  color: var(--muted);
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
  min-width: 0;
  max-width: 100%;
}

.toggle-card-copy {
  display: flex;
  flex-direction: column;
  gap: 3px;
  min-width: 0;
}

.toggle-card-copy small {
  font-size: 11.5px;
  color: var(--muted);
  line-height: 1.45;
}

.form-hint {
  margin: 0;
  font-size: 12px;
  color: var(--muted);
  line-height: 1.5;
}

.subs-list {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 10px;
  min-width: 0;
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
  min-width: 0;
  max-width: 100%;
}

.sub-card.active {
  border-color: var(--brand);
}

.sub-info {
  display: grid;
  gap: 4px;
  min-width: 0;
  flex: 1 1 240px;
}

.sub-title-line {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}

.sub-title-line .badge {
  overflow: hidden;
  text-overflow: ellipsis;
}

.sub-meta {
  font-size: 11.5px;
  color: var(--muted);
  overflow-wrap: anywhere;
}

.sub-actions {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  flex-shrink: 0;
}

.dns-heading-group {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}

.inherited-dns-toggle {
  cursor: pointer;
}

.tiny-chevron {
  transition: transform 0.2s ease;
  transform: rotate(-90deg);
}

.tiny-chevron.rotated {
  transform: rotate(0deg);
}

.inherited-dns-box {
  display: grid;
  gap: 6px;
  margin-bottom: 12px;
  padding: 10px 12px;
  border-radius: var(--radius-sm);
  background: var(--strip-bg);
  border: 1px dashed var(--line);
}

.dns-policy-row.readonly {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  font-size: 12px;
}

.dns-servers {
  font-size: 12px;
  color: var(--ink-secondary);
  overflow-wrap: anywhere;
}

.core-meta {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 10px;
  min-width: 0;
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
  min-width: 0;
  max-width: 100%;
}

.yaml-panel :deep(.card-inner) {
  min-width: 0;
  max-width: 100%;
}

.yaml-panel.fullscreen {
  position: fixed;
  inset: 0;
  z-index: 1200;
  border-radius: 0;
  padding: 16px;
  background: var(--canvas);
  overflow: auto;
}

.yaml-panel.fullscreen :deep(.card-inner) {
  display: flex;
  flex-direction: column;
  height: 100%;
  gap: 10px;
}

.yaml-toolbar {
  margin-bottom: 12px;
}

.yaml-panel.fullscreen .yaml-toolbar {
  margin-bottom: 0;
}

.yaml-left {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  min-width: 0;
}

.yaml-actions {
  gap: 6px;
  margin-left: auto;
  justify-content: flex-end;
}

.yaml-validation-banner {
  padding: 8px 12px;
  margin-bottom: 10px;
  border-radius: var(--radius-sm);
  font-size: 12px;
  line-height: 1.5;
  border: 1px solid var(--line);
  overflow-wrap: anywhere;
}

.yaml-validation-banner.valid {
  background: rgba(16, 185, 129, 0.1);
  border-color: rgba(16, 185, 129, 0.35);
  color: var(--ok);
}

.yaml-validation-banner.invalid {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 8px;
  background: rgba(244, 63, 94, 0.1);
  border-color: rgba(244, 63, 94, 0.35);
  color: var(--bad);
}

.yaml-error-banner-text {
  flex: 1 1 200px;
  min-width: 0;
  white-space: pre-wrap;
}

.yaml-locate-btn {
  padding: 2px 9px;
  border-radius: var(--radius-pill);
  border: 1px solid rgba(244, 63, 94, 0.45);
  background: rgba(244, 63, 94, 0.14);
  color: inherit;
  font-size: 11.5px;
  font-weight: 600;
  white-space: nowrap;
  flex-shrink: 0;
}

.yaml-locate-btn:hover {
  background: rgba(244, 63, 94, 0.24);
}

.yaml-validation-banner.pending {
  background: rgba(245, 158, 11, 0.1);
  border-color: rgba(245, 158, 11, 0.35);
  color: var(--warn);
}

.yaml-warnings {
  color: var(--warn);
}

.yaml-editor-shell {
  width: 100%;
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.yaml-viewer {
  width: 100%;
  height: clamp(480px, 64dvh, 780px);
  min-height: 440px;
  overflow: auto;
  border-radius: var(--radius-sm);
  border: 1px solid var(--line);
  background: var(--input-bg);
  font-size: 12.5px;
  line-height: 1.6;
}

.yaml-panel.fullscreen .yaml-editor-shell,
.yaml-panel.fullscreen .yaml-viewer {
  flex: 1 1 auto;
  height: 100% !important;
  min-height: 0 !important;
}

.yaml-panel.fullscreen .yaml-editor-shell :deep(.yaml-code-shell) {
  flex: 1 1 auto;
  height: 100% !important;
  min-height: 0 !important;
}

.yaml-code-table {
  display: table;
  min-width: 100%;
  padding: 10px 0;
}

.yaml-code-line {
  display: table-row;
}

.yaml-code-line:hover {
  background: rgba(99, 102, 241, 0.06);
}

.yaml-line-num {
  display: table-cell;
  width: 1%;
  min-width: 40px;
  padding: 0 10px 0 8px;
  text-align: right;
  color: var(--faint);
  user-select: none;
  border-right: 1px solid var(--line);
}

.yaml-line-code {
  display: table-cell;
  padding: 0 14px;
  white-space: pre;
}

.yaml-viewer.wrap .yaml-line-code {
  white-space: pre-wrap;
  word-break: break-all;
}

.tok-key { color: var(--brand); font-weight: 600; }
.tok-punct { color: var(--muted); }
.tok-str { color: var(--ok); }
.tok-num { color: var(--warn); }
.tok-bool { color: var(--brand-cyan); font-weight: 600; }
.tok-anchor { color: #c084fc; font-weight: 600; }
.tok-rule { color: #38bdf8; font-weight: 600; }
.tok-target { color: #f472b6; font-weight: 600; }
.tok-val { color: var(--ink); }
.tok-comment { color: var(--faint); font-style: italic; }

.log-live-toggle {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: 12.5px;
  color: var(--ink-secondary);
  cursor: pointer;
}

.log-panel {
  padding: 12px;
  min-width: 0;
  max-width: 100%;
}

.log-viewer {
  width: 100%;
  height: clamp(380px, 58dvh, 720px);
  min-height: 320px;
  overflow: auto;
  border-radius: var(--radius-sm);
  border: 1px solid var(--line);
  background: var(--input-bg);
  padding: 10px 12px;
  font-size: 12px;
  line-height: 1.55;
  min-width: 0;
  max-width: 100%;
}

.log-line {
  display: grid;
  grid-template-columns: 34px minmax(0, 1fr);
  gap: 8px;
  padding: 2px 0;
  border-bottom: 1px solid rgba(148, 163, 184, 0.08);
  overflow-wrap: anywhere;
}

.log-num {
  color: var(--faint);
  text-align: right;
  user-select: none;
}

.log-text {
  white-space: pre-wrap;
  word-break: break-word;
}

.log-info .log-text { color: var(--ink-secondary); }
.log-warn .log-text { color: var(--warn); }
.log-err .log-text { color: var(--bad); }
.log-debug .log-text { color: var(--faint); }

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

.spinning {
  animation: mihomo-spin 0.85s linear infinite;
}

@keyframes mihomo-spin {
  to {
    transform: rotate(360deg);
  }
}

@media (max-width: 640px) {
  .report-panel {
    padding: 12px;
  }

  .mode-btn {
    padding: 4px 8px;
    white-space: normal;
  }

  .yaml-panel {
    padding: 10px;
  }

  .yaml-viewer {
    height: clamp(400px, 60dvh, 640px) !important;
    min-height: 360px !important;
    font-size: 12px;
  }

  .filter-inputs {
    flex: 1 1 100%;
    width: 100%;
  }

  .rules-toolbar .rules-view-switch {
    width: 100%;
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }

  .rules-toolbar .rules-view-switch .mode-btn {
    text-align: center;
    font-size: 11.5px;
    padding: 6px 4px;
  }

  .rules-toolbar-actions {
    flex: 1 1 100%;
    justify-content: flex-start;
  }

  .rules-toolbar-actions .filter-inputs {
    justify-content: flex-start;
  }

  .section-toolbar > .report-actions {
    margin-left: 0;
    width: 100%;
  }

  .toolbar-meta {
    width: 100%;
  }

  .form-input.toolbar-input,
  .form-input.toolbar-select,
  .toolbar-meta .form-input.toolbar-input {
    max-width: none;
    width: 100% !important;
    flex: 1 1 100%;
  }

  .yaml-left {
    width: 100%;
  }

  .yaml-left .mode-segment {
    width: 100%;
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }

  .yaml-left .mode-btn {
    text-align: center;
    font-size: 11.5px;
    padding: 6px 4px;
  }

  .yaml-actions {
    width: 100%;
    margin-left: 0;
    justify-content: flex-start;
  }

  .panel-header-row > .btn {
    margin-left: auto;
  }

  .ports-grid,
  .auth-fields-grid,
  .selects-grid,
  .toggles-grid,
  .nodes-grid {
    grid-template-columns: minmax(0, 1fr);
  }

  .core-meta > div {
    grid-template-columns: minmax(0, 1fr);
    gap: 2px;
  }

  .rule-row,
  .sub-card {
    flex-direction: column;
    align-items: stretch;
    gap: 10px;
    padding: 12px;
  }

  .rule-main,
  .sub-info {
    flex: none;
    width: 100%;
  }

  .rule-actions,
  .sub-actions {
    width: 100%;
    justify-content: flex-end;
    padding-top: 8px;
    border-top: 1px solid var(--line);
  }
}
</style>
