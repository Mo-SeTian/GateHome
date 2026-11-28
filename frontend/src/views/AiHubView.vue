<template>
  <div class="report-page ai-hub animate-fade-in">
    <header class="hub-header">
      <div class="hub-heading">
        <h1>AI 与集成</h1>
        <p>统一管理内置网关助手对话、大模型接入点与 MCP 外部客户端授权</p>
      </div>
      <nav class="hub-tabs hub-tabs-3" role="tablist" aria-label="AI 与集成子页面">
        <router-link
          to="/assistant"
          role="tab"
          :aria-selected="activeTab === 'chat'"
          class="hub-tab"
          :class="{ active: activeTab === 'chat' }"
        >
          网关助手
        </router-link>
        <router-link
          to="/assistant/settings"
          role="tab"
          :aria-selected="activeTab === 'settings'"
          class="hub-tab"
          :class="{ active: activeTab === 'settings' }"
        >
          模型接入点
        </router-link>
        <router-link
          to="/assistant/mcp"
          role="tab"
          :aria-selected="activeTab === 'mcp'"
          class="hub-tab"
          :class="{ active: activeTab === 'mcp' }"
        >
          MCP 授权
        </router-link>
      </nav>
    </header>

    <AssistantChat v-if="activeTab === 'chat'" full embedded />
    <AssistantSettingsView v-else-if="activeTab === 'settings'" embedded />
    <McpView v-else embedded />
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import AssistantChat from '@/components/AssistantChat.vue'
import AssistantSettingsView from '@/views/AssistantSettingsView.vue'
import McpView from '@/views/McpView.vue'
import '@/styles/reports.css'

const route = useRoute()
const activeTab = computed(() => {
  const tab = route.meta.aiTab
  if (tab === 'settings' || tab === 'mcp') return tab
  return 'chat'
})
</script>
