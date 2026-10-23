<script setup>
// AudioRecorder：浏览器录音 → 上传为作业语音（最长 5 分钟）。
import { computed, onBeforeUnmount, ref } from 'vue'
import { audioExt, audioMime, uploadPracticeFile } from '../files'
import { toastErr } from '../ui'
import Icon from './Icon.vue'

const props = defineProps({
  practiceId: { type: [Number, String], required: true },
  maxSeconds: { type: Number, default: 300 }
})
const emit = defineEmits(['uploaded'])

const supported = !!(navigator.mediaDevices && navigator.mediaDevices.getUserMedia && window.MediaRecorder)
const recording = ref(false)
const seconds = ref(0)
const uploading = ref(false)
const recorded = ref(null) // { blob, url, mime, seconds }
let recorder = null
let stream = null
let chunks = []
let timer = null

const maxLabel = computed(() => `${Math.floor(props.maxSeconds / 60)} 分钟`)

function stopTracks() {
  if (stream) {
    stream.getTracks().forEach((t) => t.stop())
    stream = null
  }
}

function clearTimer() {
  if (timer) {
    clearInterval(timer)
    timer = null
  }
}

async function start() {
  if (recording.value) return
  try {
    stream = await navigator.mediaDevices.getUserMedia({ audio: true })
    const mime = audioMime()
    recorder = new MediaRecorder(stream, mime ? { mimeType: mime } : undefined)
    chunks = []
    recorder.ondataavailable = (e) => {
      if (e.data && e.data.size) chunks.push(e.data)
    }
    recorder.onstop = () => {
      const type = recorder?.mimeType || mime || 'audio/webm'
      const blob = new Blob(chunks, { type })
      resetRecording()
      if (!blob.size) return
      recorded.value = { blob, url: URL.createObjectURL(blob), mime: type, seconds: seconds.value }
    }
    seconds.value = 0
    recorder.start()
    recording.value = true
    timer = setInterval(() => {
      seconds.value += 1
      if (seconds.value >= props.maxSeconds) stop()
    }, 1000)
  } catch (e) {
    toastErr('无法录音：' + (e?.message || e) + '（请允许麦克风权限）')
    stopTracks()
  }
}

function stop() {
  if (!recording.value) return
  recording.value = false
  clearTimer()
  stopTracks()
  try { recorder?.stop() } catch { /* ignore */ }
}

function resetRecording() {
  recording.value = false
  clearTimer()
  stopTracks()
  recorder = null
  chunks = []
}

function discard() {
  if (recorded.value?.url) URL.revokeObjectURL(recorded.value.url)
  recorded.value = null
}

async function upload() {
  if (!recorded.value || uploading.value) return
  uploading.value = true
  try {
    const r = recorded.value
    const file = new File([r.blob], `voice-${Date.now()}.${audioExt(r.mime)}`, { type: r.mime })
    const meta = await uploadPracticeFile(props.practiceId, file, 'audio')
    meta.duration = Math.round(r.seconds * 10) / 10
    meta.name = `语音 ${r.seconds} 秒`
    emit('uploaded', meta)
    discard()
  } catch (e) {
    toastErr(e)
  } finally {
    uploading.value = false
  }
}

onBeforeUnmount(() => {
  stop()
  discard()
})
</script>

<template>
  <div class="audio-recorder">
    <template v-if="!supported">
      <span class="muted small">当前浏览器不支持录音，请用手机浏览器或 Chrome / Edge 打开</span>
    </template>
    <template v-else-if="recorded">
      <audio :src="recorded.url" controls style="width:100%;max-width:420px"></audio>
      <div class="row" style="gap:8px;margin-top:8px">
        <span class="muted small">时长 {{ recorded.seconds }} 秒</span>
        <button class="btn small" :disabled="uploading" @click="discard">重录</button>
        <button class="btn primary small" :disabled="uploading" @click="upload">
          <Icon name="check" :size="14" /> {{ uploading ? '上传中…' : '使用这段录音' }}
        </button>
      </div>
    </template>
    <template v-else-if="recording">
      <div class="row" style="gap:10px">
        <span class="rec-dot"></span>
        <b>录音中 {{ seconds }} 秒</b>
        <span class="muted small">最长 {{ maxLabel }}</span>
        <button class="btn primary small" @click="stop"><Icon name="check" :size="14" /> 停止录音</button>
      </div>
    </template>
    <template v-else>
      <button class="btn small" @click="start"><Icon name="mic" :size="15" /> 开始录音</button>
      <span class="muted small" style="margin-left:10px">读课文 / 背单词 / 口头回答都可以，最长 {{ maxLabel }}</span>
    </template>
  </div>
</template>
