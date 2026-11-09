<template>
  <div class="report-page animate-fade-in">
    <div class="report-heading">
      <div>
        <h1>防火墙拦截记录</h1>
        <p>核对来源 IP、命中策略、检测引擎和处理动作，审计放行与拦截结果</p>
      </div>
      <router-link class="btn btn-secondary" to="/ip-blocks">IP 拦截名单</router-link>
    </div>
    <p class="report-note">
      包含 IP 策略、WAF、自定义规则、限速和冻结事件；仅记录模式的命中也会显示。IP 归属地使用本地数据库估算。
    </p>
    <LogExplorer scope="firewall" />
  </div>
</template>
<script setup>
import LogExplorer from '@/components/LogExplorer.vue'
</script>
