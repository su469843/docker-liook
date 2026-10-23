// 作业附件工具：图片压缩（超过 3MB 自动压到 3MB 内）、上传、录音格式选择
import { postForm } from './api'

export const IMAGE_MAX_BYTES = 3 * 1024 * 1024
export const AUDIO_MAX_SECONDS = 300
export const MAX_PHOTOS = 9
export const MAX_AUDIOS = 3

export function fmtBytes(n) {
  if (!n || n <= 0) return ''
  if (n < 1024) return n + ' B'
  if (n < 1024 * 1024) return (n / 1024).toFixed(0) + ' KB'
  return (n / 1024 / 1024).toFixed(1) + ' MB'
}

function loadImage(src) {
  return new Promise((resolve, reject) => {
    const img = new Image()
    img.onload = () => resolve(img)
    img.onerror = reject
    img.src = src
  })
}

function toJpeg(canvas, quality) {
  return new Promise((resolve) => {
    try { canvas.toBlob(resolve, 'image/jpeg', quality) } catch { resolve(null) }
  })
}

// compressImage 把手机拍的大图压到 maxBytes 以内：先按最长边 2200 缩放，
// 再逐步降质量 / 缩尺寸，保证交作业不被 10MB 上限卡住。
export async function compressImage(file, maxBytes = IMAGE_MAX_BYTES) {
  if (!file || !file.type || !file.type.startsWith('image/')) return file
  let url = ''
  try {
    url = URL.createObjectURL(file)
    const img = await loadImage(url)
    const nw = img.naturalWidth || 1600
    const nh = img.naturalHeight || 1200
    const scale = Math.min(1, 2200 / Math.max(nw, nh))
    let w = Math.max(1, Math.round(nw * scale))
    let h = Math.max(1, Math.round(nh * scale))
    const canvas = document.createElement('canvas')
    const ctx = canvas.getContext('2d')
    if (!ctx) return file
    let quality = 0.9
    let blob = null
    for (let i = 0; i < 10; i++) {
      canvas.width = w
      canvas.height = h
      ctx.fillStyle = '#ffffff'
      ctx.fillRect(0, 0, w, h)
      ctx.drawImage(img, 0, 0, w, h)
      blob = await toJpeg(canvas, quality)
      if (!blob) break
      if (blob.size <= maxBytes) break
      if (quality > 0.5) {
        quality -= 0.12
      } else {
        w = Math.max(480, Math.round(w * 0.82))
        h = Math.max(360, Math.round(h * 0.82))
      }
    }
    if (!blob) return file
    if (file.size <= maxBytes && file.size <= blob.size) return file
    const base = (file.name || 'photo').replace(/\.[^.]+$/, '')
    return new File([blob], base + '.jpg', { type: 'image/jpeg', lastModified: Date.now() })
  } catch {
    return file
  } finally {
    if (url) URL.revokeObjectURL(url)
  }
}

export function practiceFileUrl(key) {
  return key ? '/api/practice-files/' + key : ''
}

// uploadPracticeFile 上传单个作业附件，返回 {key, kind, name, size}
export function uploadPracticeFile(practiceId, file, kind) {
  const fd = new FormData()
  fd.append('file', file)
  fd.append('kind', kind)
  return postForm(`/api/practices/${practiceId}/files`, fd)
}

// audioMime 选浏览器支持的录音格式（Chrome 是 webm/opus，Safari 是 mp4）
export function audioMime() {
  const list = ['audio/webm;codecs=opus', 'audio/webm', 'audio/mp4', 'audio/ogg;codecs=opus', 'audio/ogg']
  for (const m of list) {
    try {
      if (window.MediaRecorder && MediaRecorder.isTypeSupported(m)) return m
    } catch { /* ignore */ }
  }
  return ''
}

export function audioExt(mime) {
  if (!mime) return 'webm'
  if (mime.includes('mp4') || mime.includes('m4a')) return 'm4a'
  if (mime.includes('ogg')) return 'ogg'
  return 'webm'
}

export function audioSupported() {
  return !!(navigator.mediaDevices && navigator.mediaDevices.getUserMedia && window.MediaRecorder)
}

// downloadCSV 老师端导出名单 / 成绩
export function downloadCSV(filename, rows) {
  const esc = (v) => {
    const s = v === null || v === undefined ? '' : String(v)
    return /[",\n]/.test(s) ? '"' + s.replace(/"/g, '""') + '"' : s
  }
  const text = '\ufeff' + rows.map((r) => r.map(esc).join(',')).join('\r\n')
  const blob = new Blob([text], { type: 'text/csv;charset=utf-8' })
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob)
  a.download = filename
  a.click()
  setTimeout(() => URL.revokeObjectURL(a.href), 2000)
}
