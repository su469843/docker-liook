// 朗读（TTS）工具。三级降级，全部不占 CourseHub 存储空间：
//
// 1. 服务器代理（推荐）：后端配了 TTS_URL/TTS_KEY 环境变量时，/api/config 会返回
//    tts:true，前端只调同源的 GET /api/tts?text=...（带登录态），真正的 API key
//    只存在服务器上，不进前端产物；MP3 流回浏览器存进内存缓存直接播。
// 2. Worker 直连（备选）：构建时用 web/.env 的 VITE_TTS_URL/VITE_TTS_KEY 配置，
//    浏览器直接请求自建 Worker（OpenAI 兼容 POST 或自定义 GET 模板均可，见 .env.example）。
// 3. 浏览器内置发音：以上都没配置或失败时，用 speechSynthesis 兜底，零请求。
//
// 预热（preload）会把整份题目的音频提前拉进内存缓存，点喇叭基本秒出声；
// 几十 KB 一个单词的 MP3，几百个词也就几 MB，刷新页面即清。

import { api } from './api'

const ENDPOINT = (import.meta.env.VITE_TTS_URL || '').trim()
const API_KEY = (import.meta.env.VITE_TTS_KEY || '').trim()
const VOICE = (import.meta.env.VITE_TTS_VOICE || 'en-US-AriaNeural').trim()
const SPEED = Number(import.meta.env.VITE_TTS_SPEED) || 0.9
// VITE_TTS_URL 以 /v1/audio/speech 结尾 → 直连模式也走 OpenAI 兼容 POST
const OPENAI_MODE = /\/v1\/audio\/speech\/?$/.test(ENDPOINT)

const cache = new Map() // text -> objectURL
const inflight = new Map() // text -> Promise<objectURL>
let currentAudio = null
let modeP = null

// englishIn 从中英混排文本里提取要朗读的英文（剥掉 $...$ LaTeX 公式和中文，只留英文词）
export function englishIn(text) {
  const clean = String(text || '').replace(/\$[^$]*\$/g, ' ')
  const parts = clean.match(/[A-Za-z][A-Za-z'’-]*/g)
  return parts ? parts.join(' ') : ''
}

export function hasEnglish(text) {
  return !!englishIn(text)
}

// mode 探测一次并缓存结果（/api/config 是公开接口，失败时按直连/浏览器降级）
function mode() {
  if (!modeP) {
    modeP = api('/api/config')
      .then((c) => (c && c.tts ? 'proxy' : ENDPOINT ? 'direct' : 'browser'))
      .catch(() => (ENDPOINT ? 'direct' : 'browser'))
  }
  return modeP
}

// ---------------- 拉取音频 ----------------

function putBlob(text, blob) {
  const url = URL.createObjectURL(blob)
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

async function fetchProxy(text) {
  const res = await fetch('/api/tts?text=' + encodeURIComponent(text), { credentials: 'same-origin' })
  if (!res.ok) throw new Error('TTS HTTP ' + res.status)
  if ((res.headers.get('content-type') || '').includes('application/json')) throw new Error('TTS 返回错误')
  return putBlob(text, await res.blob())
}

async function fetchOpenAI(text) {
  const headers = { 'Content-Type': 'application/json' }
  if (API_KEY) headers.Authorization = 'Bearer ' + API_KEY
  const res = await fetch(ENDPOINT, {
    method: 'POST',
    headers,
    body: JSON.stringify({ model: 'tts-1', input: text, voice: VOICE, response_format: 'mp3', speed: SPEED })
  })
  if (!res.ok) throw new Error('TTS HTTP ' + res.status)
  if ((res.headers.get('content-type') || '').includes('application/json')) throw new Error('TTS 返回错误')
  return putBlob(text, await res.blob())
}

// 自定义 GET 模板模式的地址（URL 用 {text} 占位，没有则追加 ?text=）
function workerUrl(text) {
  if (!ENDPOINT || OPENAI_MODE || !text) return ''
  const enc = encodeURIComponent(text)
  if (ENDPOINT.includes('{text}')) return ENDPOINT.replace('{text}', enc)
  return ENDPOINT + (ENDPOINT.includes('?') ? '&' : '?') + 'text=' + enc
}

function fetchCached(text, fetcher) {
  if (cache.has(text)) return Promise.resolve(cache.get(text))
  let p = inflight.get(text)
  if (!p) {
    p = fetcher(text).finally(() => inflight.delete(text))
    inflight.set(text, p)
  }
  return p
}

// ---------------- 播放 ----------------

function playUrl(url) {
  if (currentAudio) currentAudio.pause()
  currentAudio = new Audio(url)
  currentAudio.play().catch(() => { /* 自动播放被拦时忽略（点按钮的场景不会发生） */ })
}

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

export async function speak(text) {
  const t = englishIn(text)
  if (!t) return
  if (window.speechSynthesis) window.speechSynthesis.cancel()
  const m = await mode()
  try {
    if (m === 'proxy') {
      playUrl(await fetchCached(t, fetchProxy))
      return
    }
    if (m === 'direct') {
      if (OPENAI_MODE) {
        playUrl(await fetchCached(t, fetchOpenAI))
        return
      }
      const url = workerUrl(t)
      if (url) {
        playUrl(url)
        return
      }
    }
  } catch { /* 代理/直连失败 → 浏览器兜底 */ }
  speakByBrowser(t)
}

// preload 提前把音频拉进缓存，学员点喇叭时基本秒出声
export function preload(text) {
  const t = englishIn(text)
  if (!t || cache.has(t) || inflight.has(t)) return
  mode().then((m) => {
    if (m === 'proxy') fetchCached(t, fetchProxy).catch(() => {})
    else if (m === 'direct' && OPENAI_MODE) fetchCached(t, fetchOpenAI).catch(() => {})
    else if (m === 'direct') {
      try {
        const a = new Audio()
        a.preload = 'auto'
        a.src = workerUrl(t)
      } catch { /* ignore */ }
    }
    // browser 模式无需预热
  })
}
