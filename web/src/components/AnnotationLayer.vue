<script setup>
// AnnotationLayer：把老师批注图形渲染成 SVG 覆盖层。
// 坐标是图片原始像素，viewBox 用原图宽高，容器按图片实际显示尺寸缩放即可。
const props = defineProps({
  shapes: { type: Array, default: () => [] },
  width: { type: Number, default: 1000 },
  height: { type: Number, default: 1000 }
})

function penPath(points) {
  if (!points || points.length < 4) return ''
  let d = `M ${points[0]} ${points[1]}`
  for (let i = 2; i + 1 < points.length; i += 2) d += ` L ${points[i]} ${points[i + 1]}`
  return d
}

function arrowHead(s) {
  const dx = s.x2 - s.x
  const dy = s.y2 - s.y
  const len = Math.hypot(dx, dy) || 1
  const ux = dx / len
  const uy = dy / len
  const size = Math.max(10, (s.width || 3) * 4.2)
  const bx = s.x2 - ux * size
  const by = s.y2 - uy * size
  const px = -uy
  const py = ux
  const w = size * 0.55
  return `${s.x2},${s.y2} ${bx + px * w},${by + py * w} ${bx - px * w},${by - py * w}`
}

function rectOf(s) {
  return {
    x: Math.min(s.x, s.x + s.w),
    y: Math.min(s.y, s.y + s.h),
    w: Math.abs(s.w),
    h: Math.abs(s.h)
  }
}
</script>

<template>
  <svg class="annotation-layer" :viewBox="`0 0 ${width} ${height}`" preserveAspectRatio="none">
    <template v-for="(s, i) in shapes" :key="i">
      <path v-if="s.tool === 'pen'" :d="penPath(s.points)" :stroke="s.color" :stroke-width="s.width || 3"
        fill="none" stroke-linecap="round" stroke-linejoin="round" vector-effect="non-scaling-stroke" />
      <ellipse v-else-if="s.tool === 'circle'" :cx="s.x + s.w / 2" :cy="s.y + s.h / 2"
        :rx="Math.abs(s.w) / 2" :ry="Math.abs(s.h) / 2" :stroke="s.color" :stroke-width="s.width || 3"
        fill="none" vector-effect="non-scaling-stroke" />
      <rect v-else-if="s.tool === 'rect'" v-bind="rectOf(s)" :width="rectOf(s).w" :height="rectOf(s).h"
        :stroke="s.color" :stroke-width="s.width || 3" fill="none" rx="4" vector-effect="non-scaling-stroke" />
      <g v-else-if="s.tool === 'arrow'">
        <line :x1="s.x" :y1="s.y" :x2="s.x2" :y2="s.y2" :stroke="s.color" :stroke-width="s.width || 3"
          vector-effect="non-scaling-stroke" />
        <polygon :points="arrowHead(s)" :fill="s.color" />
      </g>
      <text v-else-if="s.tool === 'text'" :x="s.x" :y="s.y" :fill="s.color" :font-size="s.width || 24"
        font-weight="600" dominant-baseline="hanging">{{ s.text }}</text>
    </template>
  </svg>
</template>

<style scoped>
.annotation-layer {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  pointer-events: none;
  overflow: visible;
}
</style>
