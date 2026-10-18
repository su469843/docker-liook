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

// 默认封面配色（与后端 coverColors 白名单一致）：老师选个颜色，
// 前端把「渐变底色 + 课程标题 + 手绘图标」渲染成封面图，不用再找图。
export const COVER_COLORS = [
  { id: 'green', name: '薄荷绿', from: '#0b8a67', to: '#38d9a9' },
  { id: 'blue', name: '晴空蓝', from: '#1864ab', to: '#4dabf7' },
  { id: 'indigo', name: '暮光紫', from: '#4c2f9e', to: '#9775fa' },
  { id: 'orange', name: '落日橙', from: '#c2410c', to: '#ff922b' },
  { id: 'pink', name: '莓果粉', from: '#a61e4d', to: '#f06595' },
  { id: 'teal', name: '深海青', from: '#0b5f6b', to: '#22b8cf' },
  { id: 'slate', name: '石墨灰', from: '#2b3137', to: '#868e96' },
  { id: 'gold', name: '琥珀金', from: '#8a5a00', to: '#f0b429' },
]

export function coverPalette(colorOrId) {
  if (typeof colorOrId === 'string' && colorOrId) {
    const hit = COVER_COLORS.find((c) => c.id === colorOrId)
    if (hit) return hit
  }
  const n = Math.abs(Number(colorOrId) || 0) % COVER_COLORS.length
  return COVER_COLORS[n]
}

export function coverStyle(colorOrId) {
  const p = coverPalette(colorOrId)
  return { background: `linear-gradient(135deg, ${p.from} 0%, ${p.to} 100%)` }
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
