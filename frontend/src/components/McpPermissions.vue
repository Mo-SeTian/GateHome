<template>
  <fieldset class="mcp-permissions">
    <legend>{{ title }}</legend>
    <div class="permission-grid">
      <label
        v-for="item in permissions"
        :key="item.key"
        class="permission-card"
        :class="{ checked: Boolean(model[item.key]) }"
      >
        <input
          v-model="model[item.key]"
          :name="item.key"
          type="checkbox"
        />
        <span class="permission-label">{{ item.label }}</span>
      </label>
    </div>
    <label class="sunpanel-select-label"
      >SunPanel 账号<select
        v-model.number="model.sunpanel_user_id"
        name="sunpanel_user_id"
        class="form-select"
      >
        <option :value="0">不授权 SunPanel</option>
        <option v-for="user in users" :key="user.id" :value="user.id">
          {{ user.name || user.username }}（{{ user.username }}）
        </option>
        <option
          v-if="model.sunpanel_user_id && !users.some((user) => user.id === model.sunpanel_user_id)"
          :value="model.sunpanel_user_id"
        >
          账号 #{{ model.sunpanel_user_id }} · 请刷新确认
        </option>
      </select></label
    >
    <p class="report-note">绑定后仅管理该账号的桌面。未绑定账号的 Token 无法访问 SunPanel 内容。</p>
  </fieldset>
</template>
<script setup>
const model = defineModel({ required: true })
defineProps({ users: { type: Array, default: () => [] }, title: { type: String, default: '权限范围' } })
const permissions = [
  { key: 'enabled', label: '启用' },
  { key: 'read', label: '查询' },
  { key: 'write', label: '配置修改' },
  { key: 'maintenance', label: '维护操作' }
]
</script>
<style scoped>
.mcp-permissions {
  display: grid;
  gap: 14px;
  border: 0;
  padding: 0;
  margin: 0;
  min-width: 0;
}
legend {
  font-size: 12px;
  color: var(--muted);
  margin-bottom: 4px;
  font-weight: 500;
}
.permission-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 8px;
  width: 100%;
}
@media (max-width: 640px) {
  .permission-grid {
    grid-template-columns: repeat(2, 1fr);
    gap: 8px;
  }
}
.permission-card {
  display: flex !important;
  flex-direction: row !important;
  align-items: center !important;
  justify-content: flex-start !important;
  gap: 8px !important;
  height: 38px !important;
  min-height: 38px !important;
  padding: 0 10px !important;
  margin: 0 !important;
  background: rgba(255, 255, 255, 0.03) !important;
  border: 1px solid var(--line) !important;
  border-radius: var(--radius-sm) !important;
  cursor: pointer !important;
  user-select: none !important;
  box-sizing: border-box !important;
  transition: all 0.2s cubic-bezier(0.16, 1, 0.3, 1) !important;
}
.permission-card:hover {
  background: rgba(255, 255, 255, 0.06) !important;
  border-color: var(--card-border-hover) !important;
}
.permission-card.checked {
  background: rgba(99, 102, 241, 0.12) !important;
  border-color: rgba(99, 102, 241, 0.4) !important;
}
.permission-card:has(input:focus-visible) {
  outline: 2px solid var(--brand);
  outline-offset: 3px;
}
.permission-label {
  font-size: 12.5px !important;
  font-weight: 500 !important;
  color: var(--ink-secondary) !important;
  white-space: nowrap !important;
  overflow: hidden;
  text-overflow: ellipsis;
}
.permission-card.checked .permission-label {
  color: var(--ink) !important;
  font-weight: 600 !important;
}
.sunpanel-select-label {
  display: flex !important;
  flex-direction: column !important;
  gap: 7px !important;
  font-size: 12px !important;
  font-weight: 600 !important;
  color: var(--ink-secondary) !important;
  min-width: 0 !important;
}
</style>
