// 时间 / 数字 / 封面等展示工具
export function fmtTime(iso) {
  if (!iso) return ''
  const d = new Date(iso)
  if (isNaN(d.getTime())) return ''
  return d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false })
}

export function fmtDate(iso) {
  if (!iso) return ''
  const d = new Date(iso)
  if (isNaN(d.getTime())) return ''
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

export function fmtRel(iso) {
  if (!iso) return ''
  const d = new Date(iso)
  if (isNaN(d.getTime())) return ''
  const now = new Date()
  const diff = (now - d) / 1000
  if (diff < 60) return '刚刚'
  if (diff < 3600) return `${Math.floor(diff / 60)} 分钟前`
  const sameDay = d.toDateString() === now.toDateString()
  if (sameDay) return fmtTime(iso)
  const yesterday = new Date(now.getTime() - 86400000)
  if (d.toDateString() === yesterday.toDateString()) return '昨天'
  if (d.getFullYear() === now.getFullYear()) return `${d.getMonth() + 1}月${d.getDate()}日`
  return fmtDate(iso)
}

export function fmtDur(sec) {
  if (!sec || sec <= 0) return ''
  sec = Math.floor(sec)
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  const s = sec % 60
  if (h > 0) return `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
  return `${m}:${String(s).padStart(2, '0')}`
}

export function fmtSize(n) {
  if (!n || n <= 0) return ''
  if (n < 1024) return n + ' B'
  if (n < 1024 * 1024) return (n / 1024).toFixed(0) + ' KB'
  if (n < 1024 * 1024 * 1024) return (n / 1024 / 1024).toFixed(1) + ' MB'
  return (n / 1024 / 1024 / 1024).toFixed(2) + ' GB'
}

const COVER_COLORS = [
  ['#e6fcf5', '#b2f2d5'], ['#e7f5ff', '#a5d8ff'], ['#fff4e6', '#ffd8a8'],
  ['#f3f0ff', '#d0bfff'], ['#fff0f6', '#fcc2d7'], ['#e6fcf5', '#96f2d7']
]

export function coverGradient(id) {
  const c = COVER_COLORS[Math.abs(Number(id) || 0) % COVER_COLORS.length]
  return `linear-gradient(135deg, ${c[0]}, ${c[1]})`
}

// 封面兜底用的手绘图标名（按课程 ID 稳定分配）
const COURSE_ART = ['play', 'book', 'target', 'layers', 'spark', 'class', 'chart', 'plant']

export function courseArt(id) {
  return COURSE_ART[Math.abs(Number(id) || 0) % COURSE_ART.length]
}

export function kindLabel(kind) {
  return { video: '视频', rich: '图文', doc: '文档' }[kind] || '视频'
}

export function kindIconName(kind) {
  return { video: 'video-file', rich: 'text', doc: 'doc' }[kind] || 'video-file'
}

export function quoteText(m) {
  if (!m || !m.ref_lesson_id) return ''
  const t = m.ref_time ? fmtDur(m.ref_time) : ''
  return [m.ref_title || '课时', t].filter(Boolean).join(' · ')
}
