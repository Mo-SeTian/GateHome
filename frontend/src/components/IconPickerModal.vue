<template>
  <Teleport to="body">
    <dialog
      ref="dialog" class="icon-picker-dialog" :class="{ 'icon-picker-expanded': expanded }"
      aria-labelledby="icon-picker-title" @cancel.stop @close.stop="emit('close')" @click.self="close"
    >
      <div class="icon-picker-body">
        <div class="icon-picker-handle" aria-hidden="true" @pointerdown="startDrag" @pointerup="finishDrag">
          <span />
        </div>
        <header class="icon-dialog-header">
          <h2 id="icon-picker-title">从图标库选择</h2>
          <button type="button" class="icon-close" aria-label="关闭图标选择器" @click="close"><X :size="18" /></button>
        </header>
        <div class="icon-picker-controls">
          <label class="icon-search">
            <Search :size="16" aria-hidden="true" />
            <input
              ref="search" v-model="filters.keyword" type="search" maxlength="80" aria-label="搜索图标"
              placeholder="搜索英文名、中文名或拼音" role="combobox" aria-expanded="true"
              aria-controls="icon-picker-options" :aria-activedescendant="selected ? 'picker-icon-' + active : undefined"
              @keydown="onKeydown"
            />
          </label>
          <div class="icon-filters" aria-label="分类筛选" role="group">
            <button
              v-for="[value, label] in categories" :key="value" type="button" class="icon-chip"
              :aria-pressed="filters.category === value" @click="filters.category = value"
            >{{ label }}</button>
          </div>
        </div>

        <div class="icon-picker-results" :aria-busy="loading">
          <p v-if="error" class="icon-error" role="alert">{{ error }}</p>
          <p v-else-if="loading && !items.length" class="icon-empty" role="status">正在读取图标…</p>
          <p v-else-if="!items.length" class="icon-empty" role="status">没有符合条件的图标</p>
          <div id="icon-picker-options" ref="grid" class="icon-picker-grid" role="listbox" aria-label="可选图标">
            <button
              v-for="(icon, i) in items" :id="'picker-icon-' + i" :key="icon.id" type="button"
              class="icon-tile" role="option" :aria-selected="active === i" tabindex="-1"
              :title="[icon.name, icon.cnName].filter(Boolean).join(' · ')" @click="active = i"
            >
              <img :src="icon.url" width="32" height="32" alt="" loading="lazy" />
              <span>{{ icon.name }}</span>
            </button>
          </div>
        </div>

        <div v-if="pageCount > 1" class="icon-picker-pages">
          <button type="button" class="btn btn-secondary" :disabled="page <= 1 || loading" @click="page--">上一页</button>
          <span>{{ page }} / {{ pageCount }}</span>
          <button type="button" class="btn btn-secondary" :disabled="page >= pageCount || loading" @click="page++">下一页</button>
        </div>
        <footer class="icon-picker-footer">
          <div class="icon-picker-selection">
            <img v-if="selected" :src="selected.url" width="32" height="32" alt="" />
            <div v-if="selected">
              <strong>{{ selected.name }}</strong>
              <code class="icon-path">{{ iconReference(selected) }}</code>
            </div>
            <span v-else>未选择图标</span>
          </div>
          <button type="button" class="btn btn-primary" :disabled="!selected || loading" @click="apply">应用图标</button>
        </footer>
      </div>
    </dialog>
  </Teleport>
</template>

<script setup>
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { Search, X } from 'lucide-vue-next'
import { iconCategories } from '@/api/icons'
import { useIconSearch } from '@/composables/useIconSearch'
import '@/styles/icons.css'

const props = defineProps({ modelValue: { type: String, default: '' } })
const emit = defineEmits(['select', 'close'])
const dialog = ref(null)
const search = ref(null)
const grid = ref(null)
const active = ref(0)
const expanded = ref(false)
const { filters, items, page, pageCount, loading, error } = useIconSearch(72)
const categories = [['', '全部分类'], ...Object.entries(iconCategories)]
const selected = computed(() => items.value[active.value])
const iconReference = icon => '/icons/' + icon.relPath
let dragStart = 0

watch(items, list => {
  active.value = Math.max(0, list.findIndex(icon => iconReference(icon) === props.modelValue))
})
onMounted(() => {
  dialog.value.showModal()
  search.value.focus()
})

function close() {
  dialog.value.close()
}

function apply() {
  if (!selected.value || loading.value) return
  emit('select', iconReference(selected.value))
  close()
}

function onKeydown(event) {
  if (event.isComposing || !items.value.length || loading.value) return
  if (event.key === 'Enter') {
    event.preventDefault()
    apply()
    return
  }
  const columns = getComputedStyle(grid.value).gridTemplateColumns.split(' ').length
  const offsets = { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -columns, ArrowDown: columns }
  const offset = offsets[event.key]
  if (!offset) return
  event.preventDefault()
  active.value = Math.min(items.value.length - 1, Math.max(0, active.value + offset))
  nextTick(() => grid.value?.children[active.value]?.scrollIntoView({ block: 'nearest' }))
}

function startDrag(event) {
  event.currentTarget.setPointerCapture(event.pointerId)
  dragStart = event.clientY
}

function finishDrag(event) {
  const distance = event.clientY - dragStart
  if (distance < -36) expanded.value = true
  if (distance > 52) {
    if (expanded.value) expanded.value = false
    else close()
  }
}
</script>

<style scoped>
.icon-picker-dialog {
  color: var(--ink);
  background: var(--popover-bg);
  border: 1px solid var(--line);
  border-radius: 16px;
  width: min(700px, calc(100vw - 32px));
  margin: auto;
  max-height: 85dvh;
  overflow: hidden;
  box-shadow: 0 20px 56px rgb(0 0 0 / 28%);
}
.icon-picker-body { display: flex; flex-direction: column; max-height: inherit; min-height: 0; }
.icon-picker-handle { display: none; }
.icon-picker-controls { padding: 12px 16px 8px; display: grid; gap: 8px; }
.icon-picker-results { overflow: auto; min-height: 104px; padding: 5px 16px 12px; }
.icon-picker-grid { display: grid; grid-template-columns: repeat(9, minmax(0, 1fr)); gap: 4px; }
.icon-picker-grid .icon-tile { min-height: 52px; padding: 4px 2px; gap: 2px; }
.icon-picker-pages {
  display: flex; align-items: center; justify-content: center; gap: 12px;
  padding: 6px 16px; color: var(--muted); font-variant-numeric: tabular-nums;
}
.icon-picker-dialog .icon-picker-footer {
  display: flex; align-items: center; justify-content: space-between; gap: 10px;
  padding: 10px 16px !important; border-top: 1px solid var(--line);
}
.icon-picker-selection { min-width: 0; display: flex; align-items: center; gap: 8px; color: var(--muted); }
.icon-picker-selection > div { display: grid; min-width: 0; gap: 2px; }
.icon-picker-selection strong { color: var(--ink); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.icon-picker-selection img { object-fit: contain; flex-shrink: 0; }
.icon-picker-selection .icon-path { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

@media (max-width: 680px) {
  .icon-picker-dialog {
    inset: auto 0 0;
    width: 100% !important;
    max-width: none;
    margin: 0 !important;
    border-radius: 18px 18px 0 0 !important;
    max-height: 76dvh !important;
  }
  .icon-picker-dialog.icon-picker-expanded { max-height: 94dvh !important; }
  .icon-picker-handle { display: grid; place-items: center; min-height: 22px; touch-action: none; flex-shrink: 0; }
  .icon-picker-handle span { width: 36px; height: 4px; border-radius: 4px; background: var(--faint); }
  .icon-picker-grid { grid-template-columns: repeat(5, minmax(0, 1fr)); }
  .icon-picker-dialog .icon-picker-footer { padding-bottom: max(10px, env(safe-area-inset-bottom)) !important; }
  .icon-picker-footer .btn, .icon-picker-pages .btn { min-height: 44px; }
}
</style>
