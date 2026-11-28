<template>
  <div class="report-page logs-hub animate-fade-in">
    <header class="hub-header">
      <div class="hub-heading">
        <h1>日志与拦截</h1>
        <p>集中检索反代访问日志、防火墙拦截审计、实时 IP 冻结名单与系统运行日志</p>
      </div>
      <nav class="hub-tabs hub-tabs-4" role="tablist" aria-label="日志与拦截子页面">
        <router-link
          to="/logs"
          role="tab"
          :aria-selected="activeTab === 'access'"
          class="hub-tab"
          :class="{ active: activeTab === 'access' }"
        >
          反代访问
        </router-link>
        <router-link
          to="/logs/firewall"
          role="tab"
          :aria-selected="activeTab === 'firewall'"
          class="hub-tab"
          :class="{ active: activeTab === 'firewall' }"
        >
          防火墙拦截
        </router-link>
        <router-link
          to="/logs/ip-blocks"
          role="tab"
          :aria-selected="activeTab === 'ip-blocks'"
          class="hub-tab"
          :class="{ active: activeTab === 'ip-blocks' }"
        >
          <span>IP 拦截名单</span>
          <span v-if="activeFreezeCount > 0" class="hub-tab-badge">{{ activeFreezeCount }}</span>
        </router-link>
        <router-link
          to="/logs/system"
          role="tab"
          :aria-selected="activeTab === 'system'"
          class="hub-tab"
          :class="{ active: activeTab === 'system' }"
        >
          系统日志
        </router-link>
      </nav>
    </header>

    <IpBlocksView
      v-if="activeTab === 'ip-blocks'"
      embedded
      @count-change="activeFreezeCount = $event"
    />
    <LogExplorer v-else :key="logScope" :scope="logScope" />
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import LogExplorer from '@/components/LogExplorer.vue'
import IpBlocksView from '@/views/IpBlocksView.vue'
import { adminApi } from '@/api/admin'
import '@/styles/reports.css'

const route = useRoute()
const activeTab = computed(() => {
  const tab = route.meta.logTab
  if (tab === 'firewall' || tab === 'ip-blocks' || tab === 'system') return tab
  return 'access'
})
const logScope = computed(() => {
  if (activeTab.value === 'firewall') return 'firewall'
  if (activeTab.value === 'system') return 'project'
  return 'access'
})

const activeFreezeCount = ref(0)
const controller = new AbortController()

async function refreshFreezeBadge() {
  try {
    const result = await adminApi.ipBlocks(controller.signal)
    const now = Date.parse(result?.now) || Date.now()
    activeFreezeCount.value = (result?.entries ?? []).filter((item) => Date.parse(item.until) > now).length
  } catch {
    // Non-blocking badge summary; IpBlocksView displays explicit errors when opened.
  }
}

watch(activeTab, (nextTab) => {
  if (nextTab !== 'ip-blocks') refreshFreezeBadge()
})

onMounted(() => {
  if (activeTab.value !== 'ip-blocks') refreshFreezeBadge()
})

onUnmounted(() => controller.abort())
</script>
