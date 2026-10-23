<script setup>
// VoiceFeedback：老师给学员录一段语音评语（最多 3 分钟，≤3MB），或播放已录好的评语。
import { computed, onBeforeUnmount, ref } from 'vue'
import { audioExt, audioMime, fmtBytes } from '../files'
import { toastErr } from '../ui'
import Icon from './Icon.vue'

const props = defineProps({
  practiceId: { type: [Number, String], default: 0 },
  submissionId: { type: [Number, String], default: 0 },
  // 已有的语音评语地址（为空表示还没录）
  existingUrl: { type: String, default: '' },
  maxSeconds: { type: Number, default: 180 }
})
const emit = defineEmits(['saved'])

const supported = !!(navigator.mediaDevices && navigator.mediaDevices.getUserMedia && window.MediaRecorder)
const recording = ref(false)
const seconds = ref(0)
const uploading = ref(false)
const recorded = ref(null) // { blob, url, mime, seconds }
const currentKey = ref('')
const currentUrl = ref(props.existingUrl)
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
  if (!props.practiceId || !props.submissionId) {
    toastErr('请先选择一份学员提交')
    return
  }
  uploading.value = true
  try {
    const r = recorded.value
    const file = new File([r.blob], `feedback-${Date.now()}.${audioExt(r.mime)}`, { type: r.mime })
    const fd = new FormData()
    fd.append('file', file)
    const res = await fetch(`/api/practices/${props.practiceId}/submissions/${props.submissionId}/feedback-audio`, {
      method: 'POST', body: fd, credentials: 'same-origin'
    })
    const text = await res.text()
    let data = null
    try { data = text ? JSON.parse(text) : null } catch { data = text }
    if (!res.ok) throw new Error((data && data.error) || '上传失败')
    currentKey.value = data.key || ''
    currentUrl.value = data.url || ''
    emit('saved', { key: currentKey.value, duration: Math.round(r.seconds * 10) / 10 })
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
  <div class="voice-feedback">
    <div class="row" style="gap:8px;flex-wrap:wrap;align-items:center">
      <b class="small"><Icon name="mic" :size="15" /> 语音评语</b>
      <span class="muted small">录一段话给学员讲题 / 纠正读音</span>
      <span v-if="currentKey || currentUrl" class="badge-soft ok">已录制</span>
    </div>

    <div v-if="currentUrl" style="margin-top:8px">
      <audio :src="currentUrl" controls preload="metadata" style="width:100%;max-width:420px"></audio>
    </div>

    <template v-if="!supported">
      <p class="muted small" style="margin:8px 0 0">当前浏览器不支持录音，请用 Chrome / Edge / 手机浏览器。</p>
    </template>
    <template v-else-if="recorded">
      <div style="margin-top:8px">
        <audio :src="recorded.url" controls style="width:100%;max-width:420px"></audio>
      </div>
      <div class="row" style="gap:8px;margin-top:8px">
        <span class="muted small">时长 {{ recorded.seconds }} 秒 · {{ fmtBytes(recorded.blob.size) }}</span>
        <button class="btn small" :disabled="uploading" @click="discard">重录</button>
        <button class="btn primary small" :disabled="uploading" @click="upload">
          <Icon name="check" :size="14" /> {{ uploading ? '上传中…' : '使用这段评语' }}
        </button>
      </div>
    </template>
    <template v-else-if="recording">
      <div class="row" style="gap:10px;margin-top:8px">
        <span class="rec-dot"></span>
        <b>录音中 {{ seconds }} 秒</b>
        <span class="muted small">最长 {{ maxLabel }}</span>
        <button class="btn primary small" @click="stop"><Icon name="check" :size="14" /> 停止录音</button>
      </div>
    </template>
    <template v-else>
      <button class="btn small" style="margin-top:8px" @click="start">
        <Icon name="mic" :size="15" /> 录制语音评语
      </button>
    </template>
    <p class="muted small" style="margin:6px 0 0">录完记得点「保存批改」，学员出分后可以听到。</p>
  </div>
</template>
