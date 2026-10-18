<script setup>
import { ref, onMounted, nextTick, onBeforeUnmount, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import Hls from 'hls.js'
import { api } from '../api'
import { store } from '../store'

const route = useRoute()
const router = useRouter()
const videos = ref([])
const current = ref(null)
const err = ref('')
const videoEl = ref(null)
const students = ref([])
let hls = null

const role = computed(() => store.user?.role)
const isManager = computed(() => role.value === 'admin' || role.value === 'teacher')

async function load() {
  try {
    videos.value = await api(`/api/courses/${route.params.id}/videos`)
    if (isManager.value) {
      students.value = await api(`/api/courses/${route.params.id}/students`)
    }
  } catch (e) {
    err.value = e.message
  }
}
onMounted(load)

function play(v) {
  if (v.status !== 'ready') {
    err.value = '该视频还在上传中，暂无法播放'
    return
  }
  err.value = ''
  current.value = v
  nextTick(() => {
    const url = `/hls/${v.id}/index.m3u8`
    const el = videoEl.value
    if (hls) { hls.destroy(); hls = null }
    if (Hls.isSupported()) {
      hls = new Hls()
      hls.loadSource(url)
      hls.attachMedia(el)
      hls.on(Hls.Events.MANIFEST_PARSED, () => el.play())
      hls.on(Hls.Events.ERROR, (_, d) => {
        if (d.fatal) err.value = '播放出错: ' + d.details
      })
    } else if (el.canPlayType('application/vnd.apple.mpegurl')) {
      el.src = url
      el.play()
    } else {
      err.value = '当前浏览器不支持 HLS 播放'
    }
  })
}

onBeforeUnmount(() => {
  if (hls) hls.destroy()
})

async function remove(v) {
  if (!confirm(`删除视频「${v.title}」？此操作不可恢复。`)) return
  await api('/api/videos/' + v.id, { method: 'DELETE' })
  if (current.value && current.value.id === v.id) current.value = null
  await load()
}

function copyCode(code) {
  navigator.clipboard?.writeText(code)
}

async function regenerate() {
  if (!confirm('重新生成邀请码？旧邀请码将失效。')) return
  // 邀请码在课程卡片上展示，这里提示去那里看
  const res = await api(`/api/courses/${route.params.id}/invite`, { method: 'POST' })
  alert('新邀请码：' + res.invite_code)
}
</script>

<template>
  <div class="page">
    <div class="page-head">
      <h1>课程视频</h1>
      <div class="row">
        <button v-if="isManager" class="btn" @click="regenerate">重新生成邀请码</button>
        <button v-if="isManager" class="btn primary" @click="router.push('/upload?course=' + route.params.id)">+ 上传视频</button>
      </div>
    </div>

    <p v-if="err" class="error">{{ err }}</p>

    <div v-if="current" class="card player-card">
      <h3>{{ current.title }}</h3>
      <video ref="videoEl" controls playsinline
        style="width:100%;max-height:70vh;background:#000;border-radius:8px"></video>
    </div>

    <div v-if="isManager && students.length" class="card students-card">
      <h4>👥 已加入的学员（{{ students.length }}）</h4>
      <div class="student-chips">
        <span v-for="s in students" :key="s.id" class="chip" :class="{ online: false }">
          {{ s.display_name }}
        </span>
      </div>
    </div>

    <div v-if="videos.length" class="list">
      <div v-for="v in videos" :key="v.id" class="card video-item"
        :class="{ active: current && current.id === v.id }">
        <div class="video-info" @click="play(v)">
          <strong>{{ v.title }}</strong>
          <span class="muted">
            {{ v.status === 'ready' ? '✅ 可播放' : '⏳ 上传中' }}
            · {{ (v.size / 1024 / 1024).toFixed(1) }} MB
          </span>
        </div>
        <button v-if="isManager" class="btn small danger" @click="remove(v)">删除</button>
      </div>
    </div>
    <p v-else class="muted center">{{ isManager ? '该课程还没有视频，点右上角上传 →' : '老师还没有上传视频，敬请期待' }}</p>
  </div>
</template>
