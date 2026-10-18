<script setup>
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import Hls from 'hls.js'

const props = defineProps({
  lessonId: { type: Number, required: true },
  startAt: { type: Number, default: 0 },
  poster: { type: String, default: '' }
})
const emit = defineEmits(['time', 'ended', 'error'])

const el = ref(null)
let hls = null
let startApplied = false

function destroy() {
  if (hls) { hls.destroy(); hls = null }
}

function attach() {
  destroy()
  startApplied = false
  const video = el.value
  if (!video) return
  const src = `/hls/${props.lessonId}/index.m3u8`
  if (Hls.isSupported()) {
    hls = new Hls({ maxBufferLength: 30 })
    hls.loadSource(src)
    hls.attachMedia(video)
    hls.on(Hls.Events.MANIFEST_PARSED, () => {
      if (props.startAt > 0) { video.currentTime = props.startAt; startApplied = true }
      video.play().catch(() => {})
    })
    hls.on(Hls.Events.ERROR, (_e, d) => {
      if (d.fatal) {
        if (d.type === Hls.ErrorTypes.NETWORK_ERROR) hls.startLoad()
        else emit('error', '播放出错：' + (d.details || '未知错误'))
      }
    })
  } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
    video.src = src
    if (props.startAt > 0) video.currentTime = props.startAt
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
watch(() => props.lessonId, () => attach())
onBeforeUnmount(destroy)

defineExpose({ seek, el })
</script>

<template>
  <div class="video-wrap">
    <video ref="el" controls playsinline :poster="poster" @timeupdate="onTime" @ended="emit('ended')"></video>
  </div>
</template>
