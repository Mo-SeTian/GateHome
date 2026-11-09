<template>
  <div class="report-page firewall-logs-page animate-fade-in">
    <div class="page-topbar">
      <div class="title-wrap">
        <h1 class="page-title">防火墙拦截记录</h1>
        <p class="page-desc">
          核对来源 IP、命中策略、检测引擎与拦截结果（包含 WAF、自定义规则、限速与冻结事件；IP 归属地基于本地数据库估算）
        </p>
      </div>
      <div class="topbar-actions">
        <router-link class="btn btn-secondary btn-sm" to="/ip-blocks">
          <ShieldAlert :size="15" />
          <span>IP 拦截名单</span>
        </router-link>
      </div>
    </div>
    <LogExplorer scope="firewall" />
  </div>
</template>

<script setup>
import { ShieldAlert } from 'lucide-vue-next'
import LogExplorer from '@/components/LogExplorer.vue'
</script>

<style scoped>
.page-topbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 16px;
}
.title-wrap {
  display: flex;
  flex-direction: column;
}
.page-title {
  font-size: 20px;
  font-weight: 700;
  color: var(--ink);
  margin: 0;
}
.page-desc {
  font-size: 12.5px;
  color: var(--muted);
  margin: 4px 0 0;
}
.topbar-actions {
  display: flex;
  align-items: center;
  gap: 10px;
}
.topbar-actions .btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
</style>
