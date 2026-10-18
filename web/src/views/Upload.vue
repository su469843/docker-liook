<script setup>
import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { createFFmpeg, fetchFile } from '@ffmpeg/ffmpeg'
import { api, rawPut } from '../api'

const route = useRoute()
const router = useRouter()
const courses = ref([])
const courseId = ref(route.query.course ? Number(route.query.course) : '')
const title = ref('')
const file = ref(null)
const mode = ref('copy') // copy=快速切片（不转码） transcode=兼容转码(H.264)
const stage = ref('idle') // idle | loading | slice | upload | done
const progress = ref(0)
const progressMax = ref(1)
const progressText = ref('')
const err = ref('')
const running = ref(false)

onMounted(async () => {
  courses.value = await api('/api/courses')
  if (route.query.course && !courses.value.some((c) => c.id === courseId.value)) {
    courseId.value = ''
  }
})

const ffmpeg = createFFmpeg({ corePath: '/ffmpeg/ffmpeg-core.js', log: false })

function pickFile(e) {
  file.value = e.target.files[0] || null
  if (file.value && !title.value.trim()) {
    title.value = file.value.name.replace(/\.[^.]+$/, '')
  }
}

async function start() {
  err.value = ''
  if (!courseId.value || !file.value) {
    err.value = '请先选择课程和视频文件'
    return
  }
  running.value = true
  try {
    // 1. 加载 ffmpeg.wasm（约 30MB，只加载一次）
    if (!ffmpeg.isLoaded()) {
      stage.value = 'loading'
      progressText.value = '正在加载切片引擎（首次约 30MB，请耐心等待）…'
      await ffmpeg.load()
    }

    // 2. 浏览器内切片成 HLS
    stage.value = 'slice'
    progressText.value = '正在切片，视视频大小和电脑性能可能需要几分钟…'
    const ext = (file.value.name.match(/\.[^.]+$/) || ['.mp4'])[0]
    const inputName = 'input' + ext
    ffmpeg.FS('writeFile', inputName, await fetchFile(file.value))
    const args = mode.value === 'copy'
      ? ['-i', inputName, '-c', 'copy', '-hls_time', '10', '-hls_list_size', '0', '-f', 'hls', 'out.m3u8']
      : ['-i', inputName, '-c:v', 'libx264', '-preset', 'veryfast', '-c:a', 'aac', '-hls_time', '10', '-hls_list_size', '0', '-f', 'hls', 'out.m3u8']
    await ffmpeg.run(...args)

    const names = ffmpeg.FS('readdir', '/').filter((n) => n === 'out.m3u8' || /^out\d+\.ts$/.test(n))
    if (!names.includes('out.m3u8')) {
      throw new Error('切片失败：未生成 m3u8。可能是视频编码不受支持（快速切片模式要求 H.264/AAC），请改用「兼容转码」模式')
    }
    const segs = names.filter((n) => n.endsWith('.ts'))
      .sort((a, b) => a.localeCompare(b, undefined, { numeric: true }))

    // 3. 创建视频记录并逐片上传
    stage.value = 'upload'
    progress.value = 0
    progressMax.value = segs.length + 1
    const v = await api('/api/videos', { method: 'POST', json: { course_id: courseId.value, title: title.value.trim() } })
    await rawPut(`/api/videos/${v.id}/m3u8`, ffmpeg.FS('readFile', 'out.m3u8'), 'application/vnd.apple.mpegurl')
    progress.value = 1
    for (const n of segs) {
      progressText.value = `上传分片 ${progress.value}/${progressMax.value}（${n}）`
      await rawPut(`/api/videos/${v.id}/segments/${n}`, ffmpeg.FS('readFile', n), 'video/mp2t')
      progress.value++
    }
    await api(`/api/videos/${v.id}/complete`, { method: 'POST', json: { size: file.value.size } })

    // 4. 清理 MEMFS
    try { ffmpeg.FS('unlink', inputName) } catch {}
    segs.forEach((n) => { try { ffmpeg.FS('unlink', n) } catch {} })

    stage.value = 'done'
    progressText.value = '✅ 上传完成，正在跳转…'
    setTimeout(() => router.push('/course/' + courseId.value), 1200)
  } catch (e) {
    err.value = (e && e.message) || String(e)
    stage.value = 'idle'
  } finally {
    running.value = false
  }
}
</script>

<template>
  <div class="page">
    <div class="page-head"><h1>上传视频</h1></div>

    <div class="card upload-card">
      <label>选择课程</label>
      <select v-model="courseId" :disabled="running">
        <option value="" disabled>— 请选择课程 —</option>
        <option v-for="c in courses" :key="c.id" :value="c.id">{{ c.title }}</option>
      </select>

      <label>视频标题</label>
      <input v-model="title" placeholder="留空则使用文件名" :disabled="running" />

      <label>视频文件</label>
      <input type="file" accept="video/*,.mp4,.mkv,.mov,.flv,.avi,.webm,.ts" @change="pickFile" :disabled="running" />

      <label>切片模式</label>
      <div class="modes">
        <label class="mode" :class="{ active: mode === 'copy' }">
          <input type="radio" value="copy" v-model="mode" :disabled="running" />
          <div>
            <strong>⚡ 快速切片（推荐）</strong>
            <p class="muted">不转码直接切，速度快。要求源视频为 H.264/AAC 编码（大部分 mp4 都是）</p>
          </div>
        </label>
        <label class="mode" :class="{ active: mode === 'transcode' }">
          <input type="radio" value="transcode" v-model="mode" :disabled="running" />
          <div>
            <strong>🔄 兼容转码</strong>
            <p class="muted">统一转成 H.264，兼容性最好，但速度慢、耗电，大文件慎用</p>
          </div>
        </label>
      </div>

      <button class="btn primary block" @click="start" :disabled="running || stage === 'done'">
        {{ running ? '处理中…' : '开始切片并上传' }}
      </button>

      <div v-if="stage === 'slice' || stage === 'loading'" class="progress-area">
        <p>{{ progressText }}</p>
        <div class="spinner"></div>
      </div>
      <div v-if="stage === 'upload' || stage === 'done'" class="progress-area">
        <p>{{ progressText }}</p>
        <progress :value="progress" :max="progressMax"></progress>
      </div>
      <p v-if="err" class="error">{{ err }}</p>
    </div>

    <p class="muted small-note">💡 切片在浏览器内完成，服务器零压力。浏览器内存有限，建议单个文件不超过 2GB；超大文件请用「快速切片」模式。</p>
  </div>
</template>
