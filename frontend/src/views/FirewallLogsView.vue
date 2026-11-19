<template>
  <div class="report-page firewall-logs-page animate-fade-in">
    <header class="report-heading">
      <h1>防火墙拦截记录</h1>
      <div class="topbar-actions">
        <router-link class="btn btn-secondary btn-sm" to="/ip-blocks">
          <ShieldAlert :size="15" />
          <span>IP 拦截名单</span>
        </router-link>
      </div>
    </header>
    <LogExplorer scope="firewall" />
  </div>
</template>

<script setup>
import { ShieldAlert } from 'lucide-vue-next'
import LogExplorer from '@/components/LogExplorer.vue'
</script>

<style scoped>
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

@media (max-width: 640px) {
  .topbar-actions {
    width: 100%;
  }
  .topbar-actions .btn {
    width: 100%;
    justify-content: center;
  }
}
</style>
