<template>
  <div class="toast-container" aria-live="polite" aria-relevant="additions">
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
  top: calc(20px + env(safe-area-inset-top, 0px));
  right: calc(24px + env(safe-area-inset-right, 0px));
  max-width: min(420px, calc(100vw - 48px));
  max-height: calc(100dvh - 40px - env(safe-area-inset-top, 0px) - env(safe-area-inset-bottom, 0px));
  overflow-y: auto;
  z-index: 9999;
  display: flex;
  flex-direction: column;
  gap: 10px;
  pointer-events: none;
}

.toast-text {
  min-width: 0;
  overflow-wrap: anywhere;
}

@media (max-width: 640px) {
  .toast-container {
    top: calc(12px + env(safe-area-inset-top, 0px));
    left: calc(12px + env(safe-area-inset-left, 0px));
    right: calc(12px + env(safe-area-inset-right, 0px));
    max-width: none;
    max-height: calc(100dvh - 24px - env(safe-area-inset-top, 0px) - env(safe-area-inset-bottom, 0px));
  }
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
