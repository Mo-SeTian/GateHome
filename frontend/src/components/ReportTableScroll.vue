<template>
  <div class="report-table-frame">
    <p v-if="overflowing" :id="hintId" class="report-table-hint">
      <MoveHorizontal :size="15" aria-hidden="true" />
      向左滑动查看右侧列
    </p>
    <div ref="viewport" class="report-table-wrap" role="region" :aria-label="label"
      :aria-describedby="overflowing ? hintId : undefined" :tabindex="overflowing ? 0 : undefined">
      <slot />
    </div>
  </div>
</template>

<script setup>
import { onBeforeUnmount, onMounted, ref, useId } from 'vue'
import { MoveHorizontal } from 'lucide-vue-next'

defineProps({ label: { type: String, required: true } })

const hintId = useId()
const viewport = ref(null)
const overflowing = ref(false)
let observer

onMounted(() => {
  const element = viewport.value
  observer = new ResizeObserver(() => {
    overflowing.value = element.scrollWidth > element.clientWidth
  })
  observer.observe(element)
  if (element?.firstElementChild) observer.observe(element.firstElementChild)
})

onBeforeUnmount(() => observer?.disconnect())
</script>

<style scoped>
.report-table-frame { min-width: 0; }
.report-table-hint {
  display: flex;
  align-items: center;
  gap: 6px;
  margin: 0 0 8px;
  color: var(--muted);
  font-size: 12px;
}
</style>
