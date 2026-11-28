<template>
  <div class="report-page statistics-hub animate-fade-in">
    <header class="hub-header">
      <div class="hub-heading">
        <h1>数据统计</h1>
        <p>统一查看反代请求趋势、安全命中与反代 / 端口转发双向流量明细</p>
      </div>
      <nav class="hub-tabs hub-tabs-2" role="tablist" aria-label="数据统计子页面">
        <router-link
          to="/statistics"
          role="tab"
          :aria-selected="activeTab === 'access'"
          class="hub-tab"
          :class="{ active: activeTab === 'access' }"
        >
          访问统计
        </router-link>
        <router-link
          to="/statistics/traffic"
          role="tab"
          :aria-selected="activeTab === 'traffic'"
          class="hub-tab"
          :class="{ active: activeTab === 'traffic' }"
        >
          流量统计
        </router-link>
      </nav>
    </header>

    <StatisticsView v-if="activeTab === 'access'" embedded />
    <TrafficView v-else embedded />
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import StatisticsView from '@/views/StatisticsView.vue'
import TrafficView from '@/views/TrafficView.vue'
import '@/styles/reports.css'

const route = useRoute()
const activeTab = computed(() => (route.meta.statTab === 'traffic' ? 'traffic' : 'access'))
</script>
