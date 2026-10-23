// 朗读（TTS）工具：优先走自建的 Worker TTS，音频直接从 Worker 流到学员设备，
// CourseHub 服务器不代理、不落存储；没配置或 Worker 失败时退回浏览器内置
// speechSynthesis 发音，同样零存储。
//
// Worker 地址构建时用 web/.env 配置（已 gitignore，不进仓库），支持两种接口：
//
// 1. OpenAI 兼容接口（linshenkx/edge-tts-openai-cf-worker 等，
//    URL 以 /v1/audio/speech 结尾时自动识别）：
//      VITE_TTS_URL=https://tts.example.xyz/v1/audio/speech
//      VITE_TTS_KEY=your-api-key        （Worker 未设 API_KEY 时可省略）
//      VITE_TTS_VOICE=en-US-AriaNeural  （不填默认 en-US-AriaNeural）
//      VITE_TTS_SPEED=0.9               （不填默认 0.9，跟读稍慢一点更清楚）
//    以 POST JSON + Bearer 调用（该类 Worker 已放开 CORS），返回 MP3 存进内存
//    缓存直接播放；preload 会提前拉好，点喇叭秒出声。
//
// 2. 自定义 GET 模板（URL 用 {text} 占位，没有则自动追加 ?text=）：
//      VITE_TTS_URL=https://tts.example.workers.dev/?text={text}&voice=en-US-AriaNeural
//    走 <audio src> 直连播放，不需要 Worker 开 CORS。

const ENDPOINT = (import.meta.env.VITE_TTS_URL || '').trim()
const API_KEY = (import.meta.env.VITE_TTS_KEY || '').trim()
const VOICE = (import.meta.env.VITE_TTS_VOICE || 'en-US-AriaNeural').trim()
const SPEED = Number(import.meta.env.VITE_TTS_SPEED) || 0.9
const OPENAI_MODE = /\/v1\/audio\/speech\/?$/.test(ENDPOINT)

// text -> objectURL。几十 KB 一个单词的 MP3，几百个词也就几 MB，刷新页面即清。
const cache = new Map()
const inflight = new Set()
let currentAudio = null

// englishIn 从中英混排文本里提取要朗读的英文（剥掉 $...$ LaTeX 公式和中文，只留英文词）
export function englishIn(text) {
  const clean = String(text || '').replace(/\$[^$]*\$/g, ' ')
  const parts = clean.match(/[A-Za-z][A-Za-z'’-]*/g)
  return parts ? parts.join(' ') : ''
}

export function hasEnglish(text) {
  return !!englishIn(text)
}

// ---------------- GET 模板模式 ----------------

function workerUrl(text) {
  if (!ENDPOINT || OPENAI_MODE || !text) return ''
  const enc = encodeURIComponent(text)
  if (ENDPOINT.includes('{text}')) return ENDPOINT.replace('{text}', enc)
  return ENDPOINT + (ENDPOINT.includes('?') ? '&' : '?') + 'text=' + enc
}

// ---------------- OpenAI 兼容模式 ----------------

async function fetchOpenAIAudio(text) {
  const headers = { 'Content-Type': 'application/json' }
  if (API_KEY) headers.Authorization = 'Bearer ' + API_KEY
  const res = await fetch(ENDPOINT, {
    method: 'POST',
    headers,
    body: JSON.stringify({ model: 'tts-1', input: text, voice: VOICE, response_format: 'mp3', speed: SPEED })
  })
  if (!res.ok) throw new Error('TTS HTTP ' + res.status)
  if ((res.headers.get('content-type') || '').includes('application/json')) {
    throw new Error('TTS 返回了错误信息')
  }
  const url = URL.createObjectURL(await res.blob())
  if (cache.size >= 300) {
    for (const [k, v] of cache) {
      URL.revokeObjectURL(v)
      cache.delete(k)
      if (cache.size < 150) break
    }
  }
  cache.set(text, url)
  return url
}

function playUrl(url) {
  if (currentAudio) currentAudio.pause()
  currentAudio = new Audio(url)
  currentAudio.play().catch(() => { /* 自动播放被拦时忽略（点按钮的场景不会发生） */ })
}

// ---------------- 浏览器内置兜底 ----------------

function speakByBrowser(text) {
  if (!('speechSynthesis' in window)) return
  try {
    window.speechSynthesis.cancel()
    const u = new SpeechSynthesisUtterance(text)
    u.lang = 'en-US'
    u.rate = 0.9
    const v = window.speechSynthesis.getVoices().find((x) => /^en([-_]|$)/i.test(x.lang))
    if (v) u.voice = v
    window.speechSynthesis.speak(u)
  } catch { /* 没有可用语音就算了，不影响答题 */ }
}

// ---------------- 对外接口 ----------------

export async function speak(text) {
  const t = englishIn(text)
  if (!t) return
  if (window.speechSynthesis) window.speechSynthesis.cancel()
  if (OPENAI_MODE) {
    try {
      playUrl(cache.get(t) || (await fetchOpenAIAudio(t)))
      return
    } catch { /* Worker 挂了 / key 不对 → 浏览器兜底 */ }
  } else {
    const url = workerUrl(t)
    if (url) {
      playUrl(url)
      return
    }
  }
  speakByBrowser(t)
}

// preload 提前把音频拉进缓存，学员点喇叭时基本秒出声
export function preload(text) {
  const t = englishIn(text)
  if (!t || cache.has(t) || inflight.has(t)) return
  if (OPENAI_MODE) {
    inflight.add(t)
    fetchOpenAIAudio(t).catch(() => {}).finally(() => inflight.delete(t))
    return
  }
  const url = workerUrl(t)
  if (!url) return
  try {
    const a = new Audio()
    a.preload = 'auto'
    a.src = url
  } catch { /* ignore */ }
}
