<script setup>
// FormulaText：把文本里的 $...$（行内）和 $$...$$（块级）用 KaTeX 渲染成公式。
// 老师出题时直接写 LaTeX，比如：$x^2-5x+6=0$ 的解是？
import { computed } from 'vue'
import katex from 'katex'

const props = defineProps({
  text: { type: String, default: '' },
  block: { type: Boolean, default: false }
})

function esc(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]))
}

function render(tex, display) {
  try {
    return katex.renderToString(tex, { displayMode: display, throwOnError: false, strict: 'ignore', trust: false })
  } catch {
    return esc(tex)
  }
}

const parts = computed(() => {
  const src = props.text || ''
  const out = []
  let buf = ''
  let i = 0
  while (i < src.length) {
    if (src.startsWith('$$', i)) {
      const end = src.indexOf('$$', i + 2)
      if (end > i + 2) {
        if (buf) { out.push({ type: 'text', value: buf }); buf = '' }
        out.push({ type: 'tex', display: true, html: render(src.slice(i + 2, end), true) })
        i = end + 2
        continue
      }
    }
    if (src[i] === '$') {
      const end = src.indexOf('$', i + 1)
      if (end > i + 1 && src[i + 1] !== '$') {
        if (buf) { out.push({ type: 'text', value: buf }); buf = '' }
        out.push({ type: 'tex', display: false, html: render(src.slice(i + 1, end), false) })
        i = end + 1
        continue
      }
    }
    buf += src[i]
    i++
  }
  if (buf) out.push({ type: 'text', value: buf })
  return out
})
</script>

<template>
  <span class="formula-text" :class="{ block }">
    <template v-for="(p, i) in parts" :key="i">
      <span v-if="p.type === 'text'">{{ p.value }}</span>
      <span v-else class="formula-tex" v-html="p.html"></span>
    </template>
  </span>
</template>

<style scoped>
.formula-text { white-space: pre-wrap; }
.formula-text.block { display: block; }
.formula-tex :deep(.katex) { font-size: 1.05em; }
.formula-tex :deep(.katex-display) { margin: 0.35em 0; }
</style>
