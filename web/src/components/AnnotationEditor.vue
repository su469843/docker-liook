<script setup>
// AnnotationEditor：老师圈画批注编辑器（画笔 / 圈 / 方框 / 箭头 / 文字）。
// 所有坐标存图片原始像素，保存到后端 annotations[附件key] 里，学员端按原图缩放显示。
import { computed, ref } from 'vue'
import Modal from './Modal.vue'
import Icon from './Icon.vue'
import AnnotationLayer from './AnnotationLayer.vue'

const props = defineProps({
  src: { type: String, required: true },
  shapes: { type: Array, default: () => [] },
  title: { type: String, default: '圈画批注' }
})
const emit = defineEmits(['close', 'save'])

const COLORS = ['#e03131', '#f08c00', '#2f9e44', '#1971c2', '#7048e8', '#111111']
const WIDTHS = [2, 4, 7, 11]
const TOOLS = [
  { id: 'pen', name: '画笔' },
  { id: 'circle', name: '圈' },
  { id: 'rect', name: '方框' },
  { id: 'arrow', name: '箭头' },
  { id: 'text', name: '文字' }
]

const natW = ref(1000)
const natH = ref(1000)
const tool = ref('pen')
const color = ref(COLORS[0])
const width = ref(4)
const textInput = ref('')
const shapes = ref((props.shapes || []).map((s) => ({ ...s })))
const draft = ref(null)
const svgEl = ref(null)

const rendered = computed(() => (draft.value ? [...shapes.value, draft.value] : shapes.value))

function onImgLoad(e) {
  const img = e.target
  if (img?.naturalWidth) {
    natW.value = img.naturalWidth
    natH.value = img.naturalHeight
  }
}

function clamp(v, lo, hi) {
  return Math.min(hi, Math.max(lo, v))
}

function toImg(ev) {
  const el = svgEl.value
  if (!el) return { x: 0, y: 0 }
  const rect = el.getBoundingClientRect()
  return {
    x: clamp(((ev.clientX - rect.left) / rect.width) * natW.value, 0, natW.value),
    y: clamp(((ev.clientY - rect.top) / rect.height) * natH.value, 0, natH.value)
  }
}

let drag = null

function onDown(ev) {
  ev.preventDefault()
  const p = toImg(ev)
  if (tool.value === 'text') {
    const text = textInput.value.trim()
    if (!text) return
    shapes.value.push({ tool: 'text', color: color.value, width: Math.round(width.value * 6), x: p.x, y: p.y, text })
    textInput.value = ''
    return
  }
  drag = { start: p, points: [p.x, p.y] }
  draft.value = tool.value === 'pen'
    ? { tool: 'pen', color: color.value, width: width.value, points: [p.x, p.y] }
    : { tool: tool.value, color: color.value, width: width.value, x: p.x, y: p.y, w: 0, h: 0, x2: p.x, y2: p.y }
  try { svgEl.value?.setPointerCapture(ev.pointerId) } catch { /* ignore */ }
}

function onMove(ev) {
  if (!drag || !draft.value) return
  const p = toImg(ev)
  if (tool.value === 'pen') {
    const pts = draft.value.points
    const lx = pts[pts.length - 2]
    const ly = pts[pts.length - 1]
    if (Math.hypot(p.x - lx, p.y - ly) < 1.5) return
    pts.push(p.x, p.y)
    draft.value = { ...draft.value, points: pts }
  } else if (tool.value === 'arrow') {
    draft.value = { ...draft.value, x2: p.x, y2: p.y }
  } else {
    draft.value = {
      ...draft.value,
      x: Math.min(drag.start.x, p.x),
      y: Math.min(drag.start.y, p.y),
      w: Math.abs(p.x - drag.start.x),
      h: Math.abs(p.y - drag.start.y)
    }
  }
}

function onUp() {
  if (!drag) return
  drag = null
  const d = draft.value
  draft.value = null
  if (!d) return
  if (d.tool === 'pen') {
    if (d.points.length >= 4) shapes.value.push(d)
    return
  }
  if (d.tool === 'arrow') {
    if (Math.hypot(d.x2 - d.x, d.y2 - d.y) > 6) shapes.value.push(d)
    return
  }
  if (d.w > 6 && d.h > 6) shapes.value.push(d)
}

function undo() {
  shapes.value.pop()
}

function clearAll() {
  shapes.value = []
}

function save() {
  emit('save', shapes.value.map((s) => ({ ...s })))
}
</script>

<template>
  <Modal :title="title" wide :close-on-mask="false" @close="emit('close')">
    <div class="anno-toolbar">
      <div class="row" style="gap:6px">
        <button v-for="t in TOOLS" :key="t.id" class="btn small" :class="{ primary: tool === t.id }"
          @click="tool = t.id">{{ t.name }}</button>
      </div>
      <div class="row" style="gap:6px">
        <button v-for="c in COLORS" :key="c" class="anno-color" :class="{ active: color === c }"
          :style="{ background: c }" title="颜色" @click="color = c"></button>
      </div>
      <div class="row" style="gap:6px">
        <button v-for="w in WIDTHS" :key="w" class="btn small" :class="{ primary: width === w }"
          @click="width = w"><span class="anno-width-dot" :style="{ width: Math.min(w + 3, 14) + 'px', height: Math.min(w + 3, 14) + 'px', background: color }"></span></button>
      </div>
      <input v-if="tool === 'text'" v-model="textInput" class="input" style="width:180px;height:32px"
        placeholder="输入文字后点图片放置" maxlength="80" />
      <div class="row" style="gap:6px;margin-left:auto">
        <button class="btn small" :disabled="!shapes.length" @click="undo"><Icon name="arrow-left" :size="13" /> 撤销</button>
        <button class="btn small danger" :disabled="!shapes.length" @click="clearAll">清空</button>
      </div>
    </div>

    <div class="anno-stage">
      <div class="anno-canvas" :style="{ aspectRatio: natW + ' / ' + natH }">
        <img :src="src" alt="作业图片" @load="onImgLoad" draggable="false" />
        <AnnotationLayer :shapes="rendered" :width="natW" :height="natH" />
        <svg ref="svgEl" class="annotation-layer anno-input-layer" :viewBox="`0 0 ${natW} ${natH}`"
          preserveAspectRatio="none" @pointerdown="onDown" @pointermove="onMove" @pointerup="onUp"
          @pointercancel="onUp" @pointerleave="onUp"></svg>
      </div>
    </div>
    <p class="muted small" style="margin:8px 0 0">坐标按原图保存，学员端会按同样位置显示；点「保存批注」后记得再点「保存批改」。</p>

    <template #foot>
      <button class="btn" @click="emit('close')">取消</button>
      <button class="btn primary" @click="save"><Icon name="check" :size="15" /> 保存批注</button>
    </template>
  </Modal>
</template>
