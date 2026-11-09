<template>
  <fieldset class="mcp-permissions">
    <legend>{{ title }}</legend>
    <div class="permission-options">
      <label v-for="item in permissions" :key="item.key"
        ><input v-model="model[item.key]" :name="item.key" type="checkbox" />{{ item.label }}</label
      >
    </div>
    <label
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
  margin-bottom: 12px;
}
.permission-options {
  display: flex;
  gap: 14px;
  flex-wrap: wrap;
}
.permission-options label {
  display: flex;
  align-items: center;
  gap: 6px;
}
label {
  display: grid;
  gap: 7px;
  font-size: 12px;
  color: var(--ink-secondary);
  min-width: 0;
}
</style>
