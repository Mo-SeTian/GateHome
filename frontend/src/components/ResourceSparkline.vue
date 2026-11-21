<template>
  <div v-if="geometry.count" class="sparkline-wrap" :class="toneClass">
    <svg
      class="sparkline-svg"
      viewBox="0 0 180 38"
      preserveAspectRatio="none"
      :role="compact ? undefined : 'img'"
      :aria-hidden="compact || undefined"
      :aria-label="compact ? undefined : ariaSummary"
    >
      <defs>
        <linearGradient :id="gradientId" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stop-color="currentColor" stop-opacity="0.28" />
          <stop offset="100%" stop-color="currentColor" stop-opacity="0.01" />
        </linearGradient>
      </defs>
      <line v-if="!compact"
        x1="2"
        y1="19"
        x2="178"
        y2="19"
        class="sparkline-guide"
        vector-effect="non-scaling-stroke"
      />
      <path :d="geometry.areaPath" :fill="`url(#${gradientId})`" />
      <path
        :d="geometry.linePath"
        class="sparkline-line"
        fill="none"
        vector-effect="non-scaling-stroke"
      />
      <circle
        v-if="geometry.lastPoint && !compact"
        :cx="geometry.lastPoint.x"
        :cy="geometry.lastPoint.y"
        r="2.5"
        class="sparkline-dot"
      />
    </svg>
    <div v-if="!compact" class="sparkline-meta mono">
      <span>近 {{ geometry.count }}/{{ maxSamples }} 次</span>
      <span>峰 {{ geometry.max.toFixed(1) }}% · 均 {{ geometry.avg.toFixed(1) }}%</span>
    </div>
  </div>
</template>

<script setup>
import { computed, useId } from 'vue'
import { buildSparklineGeometry, MAX_SPARKLINE_SAMPLES } from '@/api/sparkline'

const props = defineProps({
  compact: Boolean,
  samples: { type: Array, default: () => [] },
  label: { type: String, required: true },
  tone: { type: String, default: 'brand' },
  maxSamples: { type: Number, default: MAX_SPARKLINE_SAMPLES }
})

const gradientId = `sparkline-grad-${useId()}`

const geometry = computed(() =>
  buildSparklineGeometry(props.samples, { width: 180, height: 38, padX: 2, padY: 4 })
)

const toneClass = computed(() => {
  if (props.tone === 'bad') return 'tone-bad'
  if (props.tone === 'warn') return 'tone-warn'
  if (props.tone === 'ok') return 'tone-ok'
  if (props.tone === 'cyan') return 'tone-cyan'
  return 'tone-brand'
})

const ariaSummary = computed(() => {
  if (!geometry.value.count) return `${props.label}趋势图：暂无采样`
  return `${props.label}最近 ${geometry.value.count} 次采样趋势，峰值 ${geometry.value.max.toFixed(1)}%，均值 ${geometry.value.avg.toFixed(1)}%`
})
</script>

<style scoped>
.sparkline-wrap {
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin-top: 4px;
  min-width: 0;
  width: 100%;
  overflow: hidden;
}

.tone-ok { color: var(--ok); }

.tone-brand {
  color: var(--brand-light, #818cf8);
}
.theme-light .tone-ok { color: var(--ok); }

.tone-brand {
  color: var(--brand, #4f46e5);
}
.tone-cyan {
  color: var(--brand-cyan, #22d3ee);
}
.theme-light .tone-cyan {
  color: #0891b2;
}
.tone-warn {
  color: var(--warn, #f59e0b);
}
.tone-bad {
  color: var(--bad, #f43f5e);
}

.sparkline-svg {
  display: block;
  width: 100%;
  height: 36px;
  overflow: visible;
}

.sparkline-guide {
  stroke: var(--line);
  stroke-width: 1;
  stroke-dasharray: 2 3;
  opacity: 0.7;
}

.sparkline-line {
  stroke: currentColor;
  stroke-width: 1.75;
  stroke-linecap: round;
  stroke-linejoin: round;
}

.sparkline-dot {
  fill: currentColor;
}

.sparkline-meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
  font-size: 10.5px;
  color: var(--faint);
  line-height: 1.2;
  white-space: nowrap;
  overflow: hidden;
}

.sparkline-meta span:last-child {
  overflow: hidden;
  text-overflow: ellipsis;
}

.mono {
  font-family: var(--mono);
  font-variant-numeric: tabular-nums;
}
</style>
