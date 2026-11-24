<template>
  <div
    class="yaml-code-shell"
    :class="[
      `preset-${heightPreset}`,
      { wrap, readonly, 'has-errors': allErrors.length > 0 }
    ]"
  >
    <div class="yaml-code-body" :style="stageVars">
      <!-- Left Gutter: Line Numbers + Active Accent + Error Badges -->
      <div
        ref="gutterRef"
        class="yaml-gutter mono-text"
        :style="{ width: gutterWidth }"
        aria-hidden="true"
      >
        <div class="yaml-gutter-track" :style="{ transform: `translate3d(0, ${-scrollTop}px, 0)` }">
          <button
            v-for="(lineNum, idx) in lineCount"
            :key="lineNum"
            type="button"
            tabindex="-1"
            class="yaml-gutter-num"
            :class="{
              active: activeLine === lineNum,
              error: errorMap.has(lineNum)
            }"
            :style="wrap && wrappedHeights[idx] ? { height: `${wrappedHeights[idx]}px` } : undefined"
            :title="errorMap.get(lineNum) || `跳转到第 ${lineNum} 行`"
            @mousedown.prevent="jumpToLine(lineNum)"
          >
            <span class="yaml-gutter-dot" :class="{ visible: errorMap.has(lineNum) }" />
            <span>{{ lineNum }}</span>
          </button>
        </div>
      </div>

      <!-- Main Stage: Highlighted Backdrop + Positioning Lines + Transparent Textarea Overlay -->
      <div class="yaml-stage">
        <div class="yaml-backdrop mono-text" aria-hidden="true">
          <div
            ref="linesTrackRef"
            class="yaml-lines-track"
            :class="{ wrap }"
            :style="linesTrackStyle"
          >
            <div
              v-for="(row, idx) in renderedRows"
              :key="idx"
              :ref="(el) => setRowEl(el, idx)"
              class="yaml-code-row"
              :class="{
                active: activeLine === row.num,
                error: errorMap.has(row.num)
              }"
            >
              <!-- Vertical indent positioning lines -->
              <span
                v-for="lvl in row.indentLevel"
                :key="lvl"
                class="yaml-indent-guide"
                :class="{ active: isActiveScopeGuide(idx, lvl) }"
                :style="{ left: `${(lvl - 1) * 2}ch` }"
              />
              <span class="yaml-row-text">
                <template v-for="(tok, tIdx) in row.tokens" :key="tIdx"
                  ><span :class="tok.cls">{{ tok.text }}</span></template
                ><template v-if="row.tokens.length === 0">&nbsp;</template>
              </span>
            </div>
          </div>
        </div>

        <textarea
          ref="textareaRef"
          :value="modelValue"
          :class="['yaml-code-input', 'mono-text', editorClass, { wrap }]"
          :aria-label="ariaLabel"
          :readonly="readonly"
          :wrap="wrap ? 'soft' : 'off'"
          spellcheck="false"
          autocapitalize="off"
          autocomplete="off"
          @input="handleInput"
          @scroll="syncScroll"
          @click="updateCursor"
          @keyup="updateCursor"
          @select="updateCursor"
          @focus="updateCursor"
          @keydown.tab.exact.prevent="handleTab(false)"
          @keydown.shift.tab.prevent="handleTab(true)"
          @keydown.enter.exact="handleEnter"
        />
      </div>
    </div>

    <!-- Compact Footer Ruler & Error Positioning Bar -->
    <div class="yaml-code-footer">
      <div class="yaml-cursor-meta mono-text">
        <span>行 {{ cursor.line }}:{{ cursor.col }}</span>
        <span class="yaml-meta-sep">·</span>
        <span>{{ lineCount }} 行</span>
        <span class="yaml-meta-sep">·</span>
        <span>缩进 {{ activeIndentLevel }} 级</span>
      </div>

      <div class="yaml-footer-actions">
        <button
          v-if="allErrors.length > 0"
          type="button"
          class="yaml-error-jump"
          :title="currentError?.message"
          @click="cycleAndJumpError"
        >
          <AlertTriangle :size="12" />
          <span>定位错误 · 第 {{ currentError?.line }} 行</span>
          <span v-if="allErrors.length > 1" class="yaml-error-count">({{ errorIndex + 1 }}/{{ allErrors.length }})</span>
        </button>
        <span v-else class="yaml-status-ok">
          <Check :size="12" /> YAML 语法辅助就绪
        </span>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { AlertTriangle, Check } from 'lucide-vue-next'
import {
  getCursorLineCol,
  getLineIndentLevels,
  getLineOffset,
  lintYamlLines,
  parseYamlErrorLocations,
  tokenizeYamlLine
} from '@/api/yamlEditor'

const props = defineProps({
  modelValue: { type: String, default: '' },
  editorClass: { type: String, default: 'yaml-editor' },
  ariaLabel: { type: String, default: 'YAML 配置编辑器' },
  wrap: { type: Boolean, default: false },
  readonly: { type: Boolean, default: false },
  errorMessage: { type: String, default: '' },
  heightPreset: { type: String, default: 'tall' }
})

const emit = defineEmits(['update:modelValue', 'input'])

const textareaRef = ref(null)
const gutterRef = ref(null)
const linesTrackRef = ref(null)
const rowEls = []

const scrollTop = ref(0)
const scrollLeft = ref(0)
const textareaWidth = ref(0)
const wrappedHeights = ref([])
const cursor = ref({ line: 1, col: 1 })
const errorIndex = ref(0)

const LINE_HEIGHT_PX = 20
const PAD_Y_PX = 12
const PAD_X_PX = 14

const lines = computed(() => String(props.modelValue ?? '').split('\n'))
const lineCount = computed(() => lines.value.length)
const indentLevels = computed(() => getLineIndentLevels(lines.value))

const activeLine = computed(() => Math.min(cursor.value.line, lineCount.value))
const activeIndentLevel = computed(() => indentLevels.value[activeLine.value - 1] ?? 0)

// Compute continuous vertical block range sharing >= activeIndentLevel so scope guide lights up cleanly.
const activeScopeRange = computed(() => {
  const lvl = activeIndentLevel.value
  if (lvl <= 0) return { start: -1, end: -1 }
  const idx = activeLine.value - 1
  const lvls = indentLevels.value
  let start = idx
  while (start > 0 && lvls[start - 1] >= lvl) start--
  let end = idx
  while (end + 1 < lvls.length && lvls[end + 1] >= lvl) end++
  return { start, end }
})

function isActiveScopeGuide(rowIdx, lvl) {
  if (activeIndentLevel.value <= 0 || lvl !== activeIndentLevel.value) return false
  return rowIdx >= activeScopeRange.value.start && rowIdx <= activeScopeRange.value.end
}

const renderedRows = computed(() =>
  lines.value.map((line, index) => ({
    num: index + 1,
    indentLevel: indentLevels.value[index] ?? 0,
    tokens: tokenizeYamlLine(line)
  }))
)

const clientLintErrors = computed(() => lintYamlLines(props.modelValue))
const serverErrors = computed(() => parseYamlErrorLocations(props.errorMessage, props.modelValue))

const allErrors = computed(() => {
  const merged = new Map()
  for (const err of serverErrors.value) {
    if (!merged.has(err.line)) merged.set(err.line, err)
  }
  for (const err of clientLintErrors.value) {
    if (!merged.has(err.line)) merged.set(err.line, err)
  }
  return Array.from(merged.values()).sort((a, b) => a.line - b.line)
})

const errorMap = computed(() => {
  const map = new Map()
  for (const err of allErrors.value) {
    map.set(err.line, err.message)
  }
  return map
})

const currentError = computed(() => {
  if (allErrors.value.length === 0) return null
  return allErrors.value[errorIndex.value % allErrors.value.length]
})

const gutterWidth = computed(() => {
  const digits = Math.max(3, String(lineCount.value).length)
  return `calc(${digits}ch + 24px)`
})

const stageVars = computed(() => ({
  '--yaml-lh': `${LINE_HEIGHT_PX}px`,
  '--yaml-pad-y': `${PAD_Y_PX}px`,
  '--yaml-pad-x': `${PAD_X_PX}px`
}))

const linesTrackStyle = computed(() => {
  const style = {
    transform: `translate3d(${props.wrap ? 0 : -scrollLeft.value}px, ${-scrollTop.value}px, 0)`
  }
  if (props.wrap && textareaWidth.value > 0) {
    style.width = `${textareaWidth.value}px`
  }
  return style
})

function setRowEl(el, idx) {
  rowEls[idx] = el || null
}

function measureWrappedHeights() {
  if (!props.wrap) {
    if (wrappedHeights.value.length) wrappedHeights.value = []
    return
  }
  nextTick(() => {
    const next = new Array(lines.value.length)
    for (let i = 0; i < lines.value.length; i++) {
      next[i] = rowEls[i]?.offsetHeight || LINE_HEIGHT_PX
    }
    wrappedHeights.value = next
  })
}

function syncMetrics() {
  const el = textareaRef.value
  if (!el) return
  textareaWidth.value = el.clientWidth
  scrollTop.value = el.scrollTop
  scrollLeft.value = el.scrollLeft
}

function syncScroll() {
  const el = textareaRef.value
  if (!el) return
  scrollTop.value = el.scrollTop
  scrollLeft.value = el.scrollLeft
}

function updateCursor() {
  const el = textareaRef.value
  if (!el) return
  cursor.value = getCursorLineCol(el.value, el.selectionStart)
}

function handleInput(event) {
  const val = event.target.value
  emit('update:modelValue', val)
  emit('input', val)
  nextTick(() => {
    updateCursor()
    syncMetrics()
    measureWrappedHeights()
  })
}

function handleTab(outdent) {
  if (props.readonly) return
  const el = textareaRef.value
  if (!el) return
  const val = props.modelValue ?? ''
  const start = el.selectionStart
  const end = el.selectionEnd

  if (!outdent && start === end) {
    const nextVal = val.slice(0, start) + '  ' + val.slice(end)
    emit('update:modelValue', nextVal)
    emit('input', nextVal)
    nextTick(() => {
      el.selectionStart = el.selectionEnd = start + 2
      updateCursor()
      syncScroll()
    })
    return
  }

  const lineStart = val.lastIndexOf('\n', Math.max(0, start - 1)) + 1
  let lineEnd = val.indexOf('\n', end)
  if (lineEnd < 0) lineEnd = val.length
  const block = val.slice(lineStart, lineEnd)
  const blockLines = block.split('\n')
  let firstDelta = 0
  let totalDelta = 0

  const updated = blockLines.map((line, i) => {
    if (!outdent) {
      totalDelta += 2
      if (i === 0) firstDelta = 2
      return '  ' + line
    }
    const rm = line.startsWith('  ') ? 2 : line.startsWith(' ') || line.startsWith('\t') ? 1 : 0
    totalDelta -= rm
    if (i === 0) firstDelta = -rm
    return line.slice(rm)
  })

  const nextVal = val.slice(0, lineStart) + updated.join('\n') + val.slice(lineEnd)
  emit('update:modelValue', nextVal)
  emit('input', nextVal)
  nextTick(() => {
    el.selectionStart = Math.max(lineStart, start + firstDelta)
    el.selectionEnd = Math.max(lineStart, end + totalDelta)
    updateCursor()
    syncScroll()
  })
}

function handleEnter(event) {
  if (props.readonly) return
  const el = textareaRef.value
  if (!el) return
  const val = props.modelValue ?? ''
  const start = el.selectionStart
  const end = el.selectionEnd
  if (start !== end) return

  const lineStart = val.lastIndexOf('\n', Math.max(0, start - 1)) + 1
  const currentBefore = val.slice(lineStart, start)
  const indentMatch = currentBefore.match(/^( *)/)
  let nextIndent = indentMatch ? indentMatch[1] : ''
  const trimmed = currentBefore.trimEnd()
  if (trimmed.endsWith(':') && !trimmed.startsWith('#')) {
    nextIndent += '  '
  }
  if (!nextIndent) return

  event.preventDefault()
  const insert = '\n' + nextIndent
  const nextVal = val.slice(0, start) + insert + val.slice(end)
  emit('update:modelValue', nextVal)
  emit('input', nextVal)
  nextTick(() => {
    el.selectionStart = el.selectionEnd = start + insert.length
    updateCursor()
    syncMetrics()
  })
}

function jumpToLine(lineNumber) {
  const el = textareaRef.value
  if (!el) return
  const target = Math.max(1, Math.min(Number(lineNumber) || 1, lineCount.value))
  const offset = getLineOffset(props.modelValue ?? '', target)

  el.focus({ preventScroll: true })
  el.setSelectionRange(offset, offset)
  updateCursor()

  let topPx = (target - 1) * LINE_HEIGHT_PX
  if (props.wrap && rowEls[target - 1]) {
    topPx = rowEls[target - 1].offsetTop - PAD_Y_PX
  }
  const maxScroll = Math.max(0, el.scrollHeight - el.clientHeight)
  const targetScroll = Math.max(0, Math.min(maxScroll, topPx - Math.floor(el.clientHeight / 3)))
  el.scrollTop = targetScroll
  el.scrollLeft = 0
  syncScroll()
}

function cycleAndJumpError() {
  if (allErrors.value.length === 0) return
  const idx = errorIndex.value % allErrors.value.length
  jumpToLine(allErrors.value[idx].line)
  errorIndex.value = (idx + 1) % allErrors.value.length
}

watch(
  () => [props.modelValue, props.wrap],
  () => {
    rowEls.length = lines.value.length
    nextTick(() => {
      syncMetrics()
      measureWrappedHeights()
    })
  }
)

watch(
  () => props.errorMessage,
  (nextMsg) => {
    errorIndex.value = 0
    if (!nextMsg) return
    const parsed = parseYamlErrorLocations(nextMsg, props.modelValue)
    if (parsed.length > 0) {
      nextTick(() => jumpToLine(parsed[0].line))
    }
  }
)

let resizeObserver = null
onMounted(() => {
  syncMetrics()
  measureWrappedHeights()
  if (typeof ResizeObserver !== 'undefined' && textareaRef.value) {
    resizeObserver = new ResizeObserver(() => {
      syncMetrics()
      measureWrappedHeights()
    })
    resizeObserver.observe(textareaRef.value)
  }
})

onBeforeUnmount(() => {
  resizeObserver?.disconnect()
})

defineExpose({ jumpToLine })
</script>

<style scoped>
.yaml-code-shell {
  width: 100%;
  min-width: 0;
  max-width: 100%;
  display: flex;
  flex-direction: column;
  border-radius: var(--radius-sm);
  border: 1px solid var(--line);
  background: var(--input-bg);
  overflow: hidden;
  transition: border-color 0.2s ease, box-shadow 0.2s ease;
}

.yaml-code-shell:hover {
  border-color: var(--card-border-hover);
}

.yaml-code-shell:focus-within {
  border-color: var(--brand);
  box-shadow: 0 0 0 3px rgba(99, 102, 241, 0.2), 0 0 14px rgba(99, 102, 241, 0.2);
}

.yaml-code-shell.has-errors {
  border-color: rgba(244, 63, 94, 0.45);
}

.yaml-code-shell.preset-tall {
  height: clamp(480px, 64dvh, 780px);
  min-height: 440px;
}

.yaml-code-shell.preset-compact {
  height: clamp(360px, 56dvh, 680px);
  min-height: 320px;
}

.yaml-code-body {
  position: relative;
  flex: 1 1 auto;
  min-height: 0;
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  overflow: hidden;
}

/* Gutter */
.yaml-gutter {
  position: relative;
  overflow: hidden;
  border-right: 1px solid var(--line);
  background: rgba(15, 23, 42, 0.45);
  user-select: none;
  flex-shrink: 0;
}

.yaml-gutter-track {
  padding: var(--yaml-pad-y) 0;
  will-change: transform;
}

.yaml-gutter-num {
  width: 100%;
  height: var(--yaml-lh);
  padding: 0 8px 0 6px;
  display: flex;
  align-items: flex-start;
  justify-content: flex-end;
  gap: 4px;
  font-size: 11.5px;
  line-height: var(--yaml-lh);
  color: var(--faint);
  border-left: 2px solid transparent;
  cursor: pointer;
  transition: background-color 0.12s ease, color 0.12s ease;
}

.yaml-gutter-num:hover {
  color: var(--ink-secondary);
  background: rgba(99, 102, 241, 0.08);
}

.yaml-gutter-num.active {
  color: var(--brand-light);
  background: rgba(99, 102, 241, 0.12);
  border-left-color: var(--brand);
  font-weight: 600;
}

.yaml-gutter-num.error {
  color: var(--bad);
  background: rgba(244, 63, 94, 0.16);
  border-left-color: var(--bad);
  font-weight: 600;
}

.yaml-gutter-dot {
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: transparent;
  margin-top: 7.5px;
  flex-shrink: 0;
}

.yaml-gutter-dot.visible {
  background: var(--bad);
  box-shadow: 0 0 6px rgba(244, 63, 94, 0.7);
}

/* Editor Stage */
.yaml-stage {
  position: relative;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
}

.yaml-backdrop {
  position: absolute;
  inset: 0;
  overflow: hidden;
  pointer-events: none;
  z-index: 1;
}

.yaml-lines-track {
  box-sizing: border-box;
  width: max-content;
  min-width: 100%;
  padding: var(--yaml-pad-y) var(--yaml-pad-x);
  font-size: 12.5px;
  line-height: var(--yaml-lh);
  font-weight: 400;
  letter-spacing: 0;
  word-spacing: 0;
  font-variant-ligatures: none;
  tab-size: 2;
  white-space: pre;
  will-change: transform;
}

.yaml-lines-track.wrap {
  width: 100%;
  white-space: pre-wrap;
  overflow-wrap: break-word;
}

.yaml-code-row {
  position: relative;
  min-height: var(--yaml-lh);
  line-height: var(--yaml-lh);
}

.yaml-code-row.active {
  background: rgba(99, 102, 241, 0.11);
  box-shadow: inset 0 1px 0 rgba(99, 102, 241, 0.2), inset 0 -1px 0 rgba(99, 102, 241, 0.2);
}

.yaml-code-row.error {
  background: rgba(244, 63, 94, 0.15);
  box-shadow: inset 2px 0 0 var(--bad), inset 0 1px 0 rgba(244, 63, 94, 0.24), inset 0 -1px 0 rgba(244, 63, 94, 0.24);
}

/* Vertical Indent Guide Positioning Lines */
.yaml-indent-guide {
  position: absolute;
  top: 0;
  bottom: 0;
  width: 1px;
  background: rgba(148, 163, 184, 0.2);
  pointer-events: none;
}

.yaml-indent-guide.active {
  background: rgba(129, 140, 248, 0.72);
  box-shadow: 0 0 6px rgba(99, 102, 241, 0.35);
}

.yaml-row-text {
  position: relative;
  z-index: 1;
}

/* Transparent Textarea Overlay — exact typographic match with .yaml-lines-track */
.yaml-code-input {
  position: relative;
  z-index: 2;
  display: block;
  box-sizing: border-box;
  width: 100%;
  height: 100%;
  min-height: 0;
  margin: 0;
  padding: var(--yaml-pad-y) var(--yaml-pad-x);
  border: 0;
  background: transparent;
  color: transparent;
  caret-color: var(--ink);
  font-size: 12.5px;
  line-height: var(--yaml-lh);
  font-weight: 400;
  letter-spacing: 0;
  word-spacing: 0;
  font-variant-ligatures: none;
  tab-size: 2;
  white-space: pre;
  overflow: auto;
  resize: none;
}

.yaml-code-input.wrap {
  white-space: pre-wrap;
  overflow-wrap: break-word;
  overflow-x: hidden;
}

.yaml-code-input::selection {
  background: rgba(99, 102, 241, 0.28);
  color: transparent;
}

/* Syntax Token Colors (strictly weight 400 to guarantee zero glyph width drift vs textarea) */
.tok-key { color: #818cf8; font-weight: 400; }
.tok-punct { color: #94a3b8; font-weight: 400; }
.tok-str { color: #34d399; font-weight: 400; }
.tok-num { color: #fbbf24; font-weight: 400; }
.tok-bool { color: #22d3ee; font-weight: 400; }
.tok-anchor { color: #c084fc; font-weight: 400; }
.tok-rule { color: #38bdf8; font-weight: 400; }
.tok-target { color: #f472b6; font-weight: 400; }
.tok-val { color: var(--ink); font-weight: 400; }
.tok-ws { color: transparent; }
.tok-comment { color: var(--faint); font-style: italic; font-weight: 400; }

/* Bottom Status & Error Jump Ruler */
.yaml-code-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 8px;
  padding: 5px 12px;
  border-top: 1px solid var(--line);
  background: rgba(15, 23, 42, 0.55);
  font-size: 11.5px;
  color: var(--muted);
}

.yaml-cursor-meta {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.yaml-meta-sep {
  opacity: 0.45;
}

.yaml-footer-actions {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  margin-left: auto;
}

.yaml-error-jump {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 2px 8px;
  border-radius: var(--radius-pill);
  border: 1px solid rgba(244, 63, 94, 0.4);
  background: rgba(244, 63, 94, 0.14);
  color: #fb7185;
  font-size: 11.5px;
  font-weight: 500;
  cursor: pointer;
  transition: background-color 0.15s ease;
}

.yaml-error-jump:hover {
  background: rgba(244, 63, 94, 0.24);
}

.yaml-error-count {
  opacity: 0.85;
}

.yaml-status-ok {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: var(--ok);
  opacity: 0.9;
}

/* VisionOS Light Theme Palette */
:global(body.theme-light) .yaml-gutter {
  background: #f8fafc;
}

:global(body.theme-light) .yaml-code-footer {
  background: #f8fafc;
}

:global(body.theme-light) .yaml-gutter-num.active {
  color: var(--brand);
  background: rgba(99, 102, 241, 0.08);
}

:global(body.theme-light) .yaml-code-row.active {
  background: rgba(99, 102, 241, 0.07);
  box-shadow: inset 0 1px 0 rgba(99, 102, 241, 0.14), inset 0 -1px 0 rgba(99, 102, 241, 0.14);
}

:global(body.theme-light) .yaml-indent-guide {
  background: rgba(148, 163, 184, 0.34);
}

:global(body.theme-light) .yaml-indent-guide.active {
  background: rgba(99, 102, 241, 0.72);
  box-shadow: none;
}

:global(body.theme-light) .tok-key { color: #4f46e5; }
:global(body.theme-light) .tok-punct { color: #64748b; }
:global(body.theme-light) .tok-str { color: #059669; }
:global(body.theme-light) .tok-num { color: #d97706; }
:global(body.theme-light) .tok-bool { color: #0891b2; }
:global(body.theme-light) .tok-anchor { color: #9333ea; }
:global(body.theme-light) .tok-rule { color: #0284c7; }
:global(body.theme-light) .tok-target { color: #db2777; }
:global(body.theme-light) .yaml-error-jump { color: #e11d48; }

@media (max-width: 768px) {
  .yaml-code-shell.preset-tall {
    height: clamp(420px, 60dvh, 640px);
    min-height: 410px;
  }

  .yaml-code-shell.preset-compact {
    height: clamp(320px, 52dvh, 560px);
    min-height: 300px;
  }
}
</style>
