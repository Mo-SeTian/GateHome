<template>
  <div class="toast-container">
    <transition-group name="toast">
      <div 
        v-for="toast in toasts" 
        :key="toast.id" 
        class="toast-item"
        :class="`toast-${toast.type}`"
        @click="removeToast(toast.id)"
      >
        <span class="toast-dot" />
        <span class="toast-text">{{ toast.message }}</span>
      </div>
    </transition-group>
  </div>
</template>

<script setup>
import { useToast } from '@/composables/useToast'
const { toasts, removeToast } = useToast()
</script>

<style scoped>
.toast-container {
  position: fixed;
  top: 20px;
  right: 24px;
  z-index: 9999;
  display: flex;
  flex-direction: column;
  gap: 10px;
  pointer-events: none;
}

.toast-item {
  pointer-events: auto;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 16px;
  border-radius: var(--radius-sm);
  background: var(--card-bg);
  backdrop-filter: blur(24px);
  -webkit-backdrop-filter: blur(24px);
  border: 1px solid var(--card-border);
  box-shadow: 0 10px 30px rgba(0, 0, 0, 0.4), var(--inner-highlight);
  color: var(--ink);
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.2s cubic-bezier(0.16, 1, 0.3, 1);
}
.toast-item:hover {
  transform: translateY(-2px);
}

.toast-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}

.toast-success .toast-dot {
  background: var(--ok);
  box-shadow: var(--ok-glow);
}
.toast-error .toast-dot {
  background: var(--bad);
  box-shadow: var(--bad-glow);
}
.toast-warn .toast-dot {
  background: var(--warn);
  box-shadow: 0 0 10px rgba(245, 158, 11, 0.4);
}
.toast-info .toast-dot {
  background: var(--brand-cyan);
  box-shadow: 0 0 10px rgba(6, 182, 212, 0.4);
}

/* Animations */
.toast-enter-active,
.toast-leave-active {
  transition: all 0.3s cubic-bezier(0.16, 1, 0.3, 1);
}
.toast-enter-from {
  opacity: 0;
  transform: translateY(-16px) scale(0.95);
}
.toast-leave-to {
  opacity: 0;
  transform: translateX(40px) scale(0.95);
}
</style>
