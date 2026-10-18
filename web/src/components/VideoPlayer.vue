<script setup>
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import Hls from 'hls.js'

const props = defineProps({
  lessonId: { type: Number, required: true },
  // 带签名的播放地址（后端下发，带有效期）
  src: { type: String, default: '' },
  mode: { type: String, default: 'hls' }, // hls | mp4
  startAt: { type: Number, default: 0 },
  poster: { type: String, default: '' }
})
const emit = defineEmits(['time', 'ended', 'error'])

const el = ref(null)
let hls = null

function destroy() {
  if (hls) { hls.destroy(); hls = null }
  const v = el.value
  if (v) { try { v.removeAttribute('src'); v.load() } catch {} }
}

function applyStart(video) {
  if (props.startAt > 0) video.currentTime = props.startAt
  video.play().catch(() => {})
}

function attach() {
  destroy()
  const video = el.value
  if (!video) return
  const src = props.src || `/hls/${props.lessonId}/index.m3u8`
  const isMP4 = props.mode === 'mp4' || /\.(mp4|m4v|webm)(\?|$)/i.test(src)

  // 源文件直出：<video> 原生播放，支持 Range 渐进加载
  if (isMP4) {
    video.src = src
    video.onloadedmetadata = () => applyStart(video)
    video.load()
    video.play().catch(() => {})
    return
  }

  if (Hls.isSupported()) {
    hls = new Hls({ maxBufferLength: 30 })
    hls.loadSource(src)
    hls.attachMedia(video)
    hls.on(Hls.Events.MANIFEST_PARSED, () => applyStart(video))
    hls.on(Hls.Events.ERROR, (_e, d) => {
      if (!d.fatal) return
      if (d.type === Hls.ErrorTypes.NETWORK_ERROR) hls.startLoad()
      else emit('error', '播放出错：' + (d.details || '未知错误'))
    })
  } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
    video.src = src // Safari 原生 HLS
    video.onloadedmetadata = () => applyStart(video)
    video.load()
    video.play().catch(() => {})
  } else {
    emit('error', '当前浏览器不支持 HLS 播放')
  }
}

function seek(t) {
  const video = el.value
  if (!video) return
  video.currentTime = Math.max(0, t || 0)
  video.play().catch(() => {})
}

function onTime() {
  emit('time', el.value?.currentTime || 0)
}

onMounted(attach)
watch(() => [props.lessonId, props.src], () => attach())
onBeforeUnmount(destroy)

defineExpose({ seek, el })
</script>

<template>
  <div class="video-wrap">
    <video ref="el" controls playsinline :poster="poster" @timeupdate="onTime" @ended="emit('ended')"></video>
  </div>
</template>
