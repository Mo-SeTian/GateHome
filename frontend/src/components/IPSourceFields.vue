<template>
  <label
    >来源网卡<select v-model="settings.interface" name="interface" class="form-select">
      <option value="">自动路由</option>
      <option v-for="item in interfaces" :key="item.name" :value="item.name">
        {{ item.name }} · {{ item.up ? '已连接' : '未连接' }} · {{ item.addresses.join(', ') }}
      </option>
      <option
        v-if="settings.interface && !interfaces.some((item) => item.name === settings.interface)"
        :value="settings.interface"
      >
        {{ settings.interface }}（当前不可用）
      </option>
    </select></label
  >
  <template v-for="family in ['ipv4', 'ipv6']" :key="family">
    <label
      >{{ family === 'ipv4' ? 'IPv4' : 'IPv6' }} 获取方式<select
        v-model="settings[family + '_source']"
        :name="family + '_source'"
        class="form-select"
      >
        <option value="url">通过接口获取</option>
        <option value="interface">读取网卡公网 IP</option>
      </select></label
    >
    <label v-if="endpoints"
      >{{ family === 'ipv4' ? 'IPv4' : 'IPv6' }} 接口列表<textarea
        v-model="settings[family + 'Text']"
        :name="family + '_urls'"
        class="form-input mono-text"
        rows="3"
        required
      />
    </label>
  </template>
  <p class="report-note">
    接口查询保持直连，可绑定网卡；每个接口最多等待 5 秒，按顺序尝试。直接读取只接受公网地址。Docker bridge
    只显示容器内网卡。
  </p>
</template>
<script setup>
defineProps({
  settings: { type: Object, required: true },
  interfaces: { type: Array, default: () => [] },
  endpoints: Boolean
})
</script>
