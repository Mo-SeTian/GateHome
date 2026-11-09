<template>
  <div 
    ref="cardRef"
    class="spotlight-card"
    @mousemove="handleMouseMove"
    @mouseleave="handleMouseLeave"
    :style="spotlightStyle"
  >
    <div class="spotlight-overlay" />
    <div class="card-inner">
      <slot />
    </div>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'

const cardRef = ref(null)
const mouseX = ref(0)
const mouseY = ref(0)
const isHovered = ref(false)

const handleMouseMove = (e) => {
  if (!cardRef.value) return
  const rect = cardRef.value.getBoundingClientRect()
  mouseX.value = e.clientX - rect.left
  mouseY.value = e.clientY - rect.top
  isHovered.value = true
}

const handleMouseLeave = () => {
  isHovered.value = false
}

const spotlightStyle = computed(() => {
  return {
    '--spotlight-x': `${mouseX.value}px`,
    '--spotlight-y': `${mouseY.value}px`,
    '--spotlight-opacity': isHovered.value ? '1' : '0'
  }
})
</script>

<style scoped>
.spotlight-card {
  position: relative;
  background: var(--card-bg);
  border-radius: var(--radius-card);
  border: 1px solid var(--card-border);
  box-shadow: var(--shadow-card), var(--inner-highlight);
  backdrop-filter: blur(20px);
  -webkit-backdrop-filter: blur(20px);
  overflow: hidden;
  transition: border-color 0.3s ease, box-shadow 0.3s ease, transform 0.25s cubic-bezier(0.16, 1, 0.3, 1);
}

.spotlight-card:hover {
  border-color: var(--card-border-hover);
  box-shadow: var(--shadow-hover);
}

.spotlight-overlay {
  position: absolute;
  inset: 0;
  pointer-events: none;
  opacity: var(--spotlight-opacity, 0);
  background: radial-gradient(
    450px circle at var(--spotlight-x, 0px) var(--spotlight-y, 0px),
    var(--spotlight-color),
    transparent 70%
  );
  transition: opacity 0.3s ease;
  z-index: 1;
}

.card-inner {
  position: relative;
  z-index: 2;
  height: 100%;
}
</style>
