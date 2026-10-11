<script setup lang="ts">
import { computed } from 'vue'
import { NEllipsis } from 'naive-ui'
import { ItemIcon } from '@/components/common'
import type { HealthResult } from '@/api/panel/serviceHealth'
import { PanelPanelConfigStyleEnum } from '@/enums'

interface Prop {
  health?: HealthResult
  itemInfo?: Panel.ItemInfo
  size?: number // 默认70
  forceBackground?: string // 强制背景色
  iconTextColor?: string
  iconTextInfoHideDescription: boolean
  iconTextIconHideTitle: boolean
  style: PanelPanelConfigStyleEnum
}

const props = withDefaults(defineProps<Prop>(), {
  size: 70,
})

const emit = defineEmits<{ (e: 'healthDetails'): void }>()

const defaultBackground = '#2a2a2a6b'

const calculateLuminance = (color: string) => {
  const hex = color.replace(/^#/, '')
  const r = parseInt(hex.substring(0, 2), 16)
  const g = parseInt(hex.substring(2, 4), 16)
  const b = parseInt(hex.substring(4, 6), 16)
  return (0.299 * r + 0.587 * g + 0.114 * b) / 255
}

const textColor = computed(() => {
  const luminance = calculateLuminance(props.itemInfo?.icon?.backgroundColor || defaultBackground)
  return luminance > 0.5 ? 'black' : 'white'
})
</script>

<template>
  <div class="app-icon w-full relative">
    <button v-if="health" type="button" class="service-health-badge" :class="[{ 'service-health-compact': style === PanelPanelConfigStyleEnum.icon }, health.state === 'auth_required' ? 'service-health-auth' : health.healthy ? 'service-health-ok' : 'service-health-error']" :title="`${health.message} · ${health.duration_ms} ms`" :aria-label="`${itemInfo?.title}：${health.label}，查看诊断`" @click.stop="emit('healthDetails')">
      <span aria-hidden="true" /><span class="health-status-label">{{ health.label }}</span>
    </button>
    <!-- 详情图标 -->
    <div
      v-if="style === PanelPanelConfigStyleEnum.info"
      class="app-icon-info w-full rounded-2xl  transition-all duration-200 hover:shadow-[0_0_20px_10px_rgba(0,0,0,0.2)] flex"
      :style="{ background: itemInfo?.icon?.backgroundColor || defaultBackground }"
    >
      <!-- 图标 -->
      <div class="app-icon-info-icon w-[70px] h-[70px]">
        <div class="w-[70px] h-full flex items-center justify-center ">
          <ItemIcon :item-icon="itemInfo?.icon" force-background="transparent" :size="50" class="overflow-hidden rounded-xl" />
        </div>
      </div>

      <!-- 文字 -->
      <!-- 如果为纯白色，将自动根据背景的明暗计算字体的黑白色 -->
      <div class="text-white flex items-center" :style="{ color: (iconTextColor === '#ffffff') ? textColor : iconTextColor, maxWidth: 'calc(100% - 80px)' }">
        <div class="app-icon-info-text-box w-full">
          <div class="app-icon-info-text-box-title font-semibold w-full">
            <NEllipsis>
              {{ itemInfo?.title }}
            </NEllipsis>
          </div>
          <div v-if="!iconTextInfoHideDescription" class="app-icon-info-text-box-description">
            <NEllipsis :line-clamp="2" class="text-xs">
              {{ itemInfo?.description }}
            </NEllipsis>
          </div>
        </div>
      </div>
    </div>

    <!-- 极简(小)图标（APP） -->
    <div v-if="style === PanelPanelConfigStyleEnum.icon" class="app-icon-small">
      <div
        class="app-icon-small-icon overflow-hidden rounded-2xl sunpanel w-[70px] h-[70px] mx-auto rounded-2xl transition-all duration-200 hover:shadow-[0_0_20px_10px_rgba(0,0,0,0.2)]"
        :title="itemInfo?.description"
      >
        <ItemIcon :item-icon="itemInfo?.icon" />
      </div>
      <div
        v-if="!iconTextIconHideTitle"
        class="app-icon-small-title text-center app-icon-text-shadow cursor-pointer mt-[2px]"
        :style="{ color: iconTextColor }"
      >
        <span>{{ itemInfo?.title }}</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.service-health-badge {position:absolute;right:5px;top:5px;z-index:1;display:flex;align-items:center;gap:4px;max-width:calc(100% - 10px);font-size:12px;line-height:18px;padding:1px 5px;border-radius:6px;background:rgba(255,255,255,.94);color:#176a44;cursor:pointer;border:1px solid rgba(24,160,88,.35)}
.service-health-badge>span:first-child{width:5px;height:5px;border-radius:50%;background:currentColor;flex:none}
.service-health-auth{color:#946200;border-color:rgba(148,98,0,.35)}
.service-health-error{color:#b42318;border-color:rgba(180,35,24,.35)}
.service-health-badge:focus-visible{outline:2px solid currentColor;outline-offset:2px}
.service-health-compact{width:18px;height:18px;justify-content:center;padding:0;border-radius:50%}
.service-health-compact .health-status-label{display:none}
.service-health-compact:before{content:'';position:absolute;inset:-13px;border-radius:50%}
@media(max-width:639px){
  .service-health-badge{width:18px;height:18px;justify-content:center;padding:0;border-radius:50%}
  .service-health-badge .health-status-label{display:none}
  .service-health-badge:before{content:'';position:absolute;inset:-13px;border-radius:50%}
}
</style>
