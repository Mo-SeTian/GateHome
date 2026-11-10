<template>
  <div class="toast-container" aria-live="polite" aria-relevant="additions">
    <transition-group name="toast">
      <div 
        v-for="toast in toasts" 
        :key="toast.id" 
        class="toast-item"
        :class="[`toast-${toast.type}`, `cat-${toast.category || 'system'}`]"
        @mouseenter="pauseToast(toast.id)"
        @mouseleave="resumeToast(toast.id)"
        @touchstart.passive="pauseToast(toast.id)"
        @touchend.passive="resumeToast(toast.id)"
      >
        <!-- Desktop Single-line Compact View -->
        <div class="toast-desktop-row" @click="handleToastClick(toast)">
          <span class="toast-dot" />
          <span class="toast-category">{{ categoryLabel(toast.category) }}</span>
          <span class="toast-text">{{ toast.message }}</span>
          <button 
            class="toast-close-btn" 
            title="关闭通知" 
            aria-label="关闭此通知"
            @click.stop="removeToast(toast.id)"
          >
            <X :size="11" />
          </button>
        </div>

        <!-- Mobile Two-line Compact Tile View (Right Upper) -->
        <div class="toast-mobile-tile" @click="handleToastClick(toast)">
          <div class="tile-header">
            <div class="tile-meta">
              <span class="toast-dot" />
              <span class="toast-category">{{ categoryLabel(toast.category) }}</span>
              <span class="toast-time">· 刚刚</span>
            </div>
            <button 
              class="toast-close-btn" 
              title="关闭通知" 
              aria-label="关闭此通知"
              @click.stop="removeToast(toast.id)"
            >
              <X :size="10" />
            </button>
          </div>
          <div class="tile-message">{{ toast.message }}</div>
        </div>
      </div>
    </transition-group>
  </div>
</template>

<script setup>
import { useRouter } from 'vue-router'
import { X } from 'lucide-vue-next'
import { useToast } from '@/composables/useToast'

const router = useRouter()
const { toasts, removeToast, pauseToast, resumeToast } = useToast()

function categoryLabel(cat) {
  switch (cat) {
    case 'waf': return 'WAF 防护'
    case 'ddns': return 'DDNS'
    case 'backup': return '系统备份'
    case 'cert': return 'SSL 证书'
    case 'mcp': return 'AI 管理'
    default: return '系统'
  }
}

function handleToastClick(toast) {
  if (toast.path) {
    router.push(toast.path)
    removeToast(toast.id)
  }
}
</script>

<style scoped>
/* Container Positioning */
.toast-container {
  position: fixed;
  top: calc(18px + env(safe-area-inset-top, 0px));
  right: calc(20px + env(safe-area-inset-right, 0px));
  z-index: 9999;
  display: flex;
  flex-direction: column;
  gap: 8px;
  pointer-events: none;
  max-width: 440px;
}

/* Base Toast Item */
.toast-item {
  pointer-events: auto;
  background: var(--card-bg);
  backdrop-filter: blur(28px) saturate(180%);
  -webkit-backdrop-filter: blur(28px) saturate(180%);
  border: 1px solid var(--card-border);
  box-shadow: 0 10px 28px rgba(0, 0, 0, 0.45), var(--inner-highlight);
  color: var(--ink);
  cursor: pointer;
  transition: all 0.22s cubic-bezier(0.16, 1, 0.3, 1);
  overflow: hidden;
}

.toast-item:hover {
  transform: translateY(-1.5px);
  border-color: var(--card-border-hover);
  box-shadow: 0 14px 34px rgba(0, 0, 0, 0.55), 0 0 18px rgba(99, 102, 241, 0.15);
}

/* Glowing Indicator Dots */
.toast-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  flex-shrink: 0;
}
.toast-success .toast-dot {
  background: var(--ok);
  box-shadow: 0 0 8px var(--ok);
}
.toast-error .toast-dot {
  background: var(--bad);
  box-shadow: 0 0 8px var(--bad);
}
.toast-warn .toast-dot {
  background: var(--warn);
  box-shadow: 0 0 8px var(--warn);
}
.toast-info .toast-dot {
  background: var(--brand-cyan, #06b6d4);
  box-shadow: 0 0 8px rgba(6, 182, 212, 0.6);
}

/* Close Button (Apple macOS Circular Style) */
.toast-close-btn {
  width: 20px;
  height: 20px;
  border-radius: 50%;
  background: rgba(255, 255, 255, 0.08);
  border: 1px solid rgba(255, 255, 255, 0.12);
  color: var(--muted);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  flex-shrink: 0;
  transition: all 0.15s ease;
  padding: 0;
}
.toast-close-btn:hover {
  background: var(--bad);
  border-color: var(--bad);
  color: #fff;
  transform: scale(1.1);
}

/* ---------------- DESKTOP LAYOUT (Default) ---------------- */
.toast-desktop-row {
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 7px 12px 7px 14px;
  min-height: 38px;
  border-radius: 20px;
}
.toast-item {
  border-radius: 20px;
}
.toast-mobile-tile {
  display: none;
}

.toast-category {
  font-size: 11px;
  font-weight: 700;
  color: var(--muted);
  text-transform: uppercase;
  letter-spacing: 0.4px;
  padding-right: 8px;
  border-right: 1px solid var(--line);
  flex-shrink: 0;
  line-height: 1;
}

.toast-text {
  font-size: 12.5px;
  font-weight: 500;
  color: var(--ink);
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  line-height: 1.4;
  padding-right: 4px;
}

/* ---------------- MOBILE LAYOUT (< 640px) ---------------- */
@media (max-width: 640px) {
  .toast-container {
    top: calc(56px + env(safe-area-inset-top, 0px));
    left: auto;
    right: calc(12px + env(safe-area-inset-right, 0px));
    max-width: 215px;
    width: 215px;
  }

  .toast-desktop-row {
    display: none;
  }

  .toast-item {
    border-radius: 14px;
  }

  .toast-mobile-tile {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 8px 10px;
  }

  .tile-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .tile-meta {
    display: flex;
    align-items: center;
    gap: 5px;
  }

  .tile-header .toast-category {
    font-size: 10.5px;
    padding-right: 0;
    border-right: none;
  }

  .toast-time {
    font-size: 9.5px;
    color: var(--muted);
  }

  .tile-header .toast-close-btn {
    width: 18px;
    height: 18px;
  }

  .tile-message {
    font-size: 11px;
    color: var(--ink);
    line-height: 1.35;
    overflow: hidden;
    text-overflow: ellipsis;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
  }
}

/* Smooth Transitions */
.toast-enter-active,
.toast-leave-active {
  transition: all 0.28s cubic-bezier(0.16, 1, 0.3, 1);
}
.toast-enter-from {
  opacity: 0;
  transform: translateY(-12px) scale(0.92);
}
.toast-leave-to {
  opacity: 0;
  transform: translateX(28px) scale(0.92);
}
</style>
