<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '../api'
import { fmtDate, fmtDur, practiceKindIcon, practiceKindLabel, subjectLabel } from '../format'
import { compressImage, MAX_AUDIOS, MAX_PHOTOS, practiceFileUrl, uploadPracticeFile } from '../files'
import { hasEnglish, preload, speak } from '../tts'
import { toastOk, toastErr, confirmDialog } from '../ui'
import Icon from '../components/Icon.vue'
import FormulaText from '../components/FormulaText.vue'
import AudioRecorder from '../components/AudioRecorder.vue'
import AttachmentList from '../components/AttachmentList.vue'
import ProctorDashboard from '../components/ProctorDashboard.vue'

const route = useRoute()

const data = ref(null)
const loading = ref(true)
const err = ref('')
const submitting = ref(false)
const answers = reactive({})
const attachments = ref([])
const uploadingPhotos = ref(false)
const cameraInput = ref(null)
const galleryInput = ref(null)

const practice = computed(() => data.value?.practice || null)
const questions = computed(() => data.value?.questions || [])
const submission = computed(() => data.value?.submission || null)
const canManage = computed(() => !!practice.value?.can_manage)
// 老师预览：没有「我的提交」，只展示题目与参考答案
const preview = computed(() => canManage.value && !submission.value)
const reveal = computed(() => canManage.value || !!submission.value?.graded)
const done = computed(() => !!submission.value)
const photoAtts = computed(() => attachments.value.filter((a) => a.kind === 'image'))
const audioAtts = computed(() => attachments.value.filter((a) => a.kind === 'audio'))

// 题目加载后预拉一遍 TTS 音频（没配置 VITE_TTS_URL 时 preload 是空操作，不发生任何请求）
watch(questions, (qs) => {
  for (const q of qs || []) {
    preload(q.stem)
    for (const opt of q.options || []) preload(opt)
  }
})

// ---------------- 严格模式 / 监考 ----------------

const strict = computed(() => practice.value?.proctor?.mode === 'strict' && !canManage.value)
const proctorCfg = computed(() => practice.value?.proctor || {
  mode: 'normal', max_exits: 0, require_fullscreen: true, require_camera: false, require_screen: false
})
const showProctor = ref(false)
const started = ref(false)
const examRemaining = ref(0) // 秒
const exitCount = ref(0)
const overLimit = ref(false)
const warnBanner = ref('')
const mediaState = ref({ camera: 'none', screen: 'none' }) // none | granted | denied | unsupported
// 已开始过（刷新后可以「继续答题」，但请求全屏 / 摄像头必须发生在用户点击里）
const attemptStarted = computed(() => !!data.value?.attempt?.started_at)

let examTimer = null
let snapshotTimer = null
let cameraStream = null
let screenStream = null
let cameraVideo = null
let screenVideo = null
let leaving = false
let autoSubmitScheduled = false

function envInfo() {
  return {
    ua: navigator.userAgent,
    screen: `${window.screen?.width || 0}x${window.screen?.height || 0}`,
    window: `${window.outerWidth || 0}x${window.outerHeight || 0}`,
    language: navigator.language || '',
    timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || '',
    platform: navigator.platform || '',
    dpr: String(window.devicePixelRatio || 1)
  }
}

// captureShot 从摄像头 / 屏幕共享视频里抓一帧（没有权限时返回空，只上报环境信息）
async function captureShot() {
  const video = cameraVideo || screenVideo
  if (!video || !video.videoWidth) return ''
  const scale = Math.min(1, 480 / video.videoWidth)
  const canvas = document.createElement('canvas')
  canvas.width = Math.max(1, Math.round(video.videoWidth * scale))
  canvas.height = Math.max(1, Math.round(video.videoHeight * scale))
  try {
    canvas.getContext('2d').drawImage(video, 0, 0, canvas.width, canvas.height)
    return canvas.toDataURL('image/jpeg', 0.55)
  } catch {
    return ''
  }
}

async function report(kind, detail = {}, withImage = false) {
  try {
    const image = withImage ? await captureShot() : ''
    const res = await api(`/api/practices/${practice.value.id}/proctor/event`, {
      method: 'POST', json: { kind, detail, image }
    })
    if (res && typeof res.exits === 'number') {
      exitCount.value = res.exits
      if (res.over) scheduleAutoSubmit()
    }
    return res
  } catch {
    return null
  }
}

function scheduleAutoSubmit() {
  if (autoSubmitScheduled) return
  autoSubmitScheduled = true
  overLimit.value = true
  warnBanner.value = `已离开 ${exitCount.value} 次，超过允许次数（${proctorCfg.value.max_exits} 次），系统将自动交卷`
  toastErr('离开次数超限，即将自动交卷')
  setTimeout(() => {
    if (!done.value && started.value) submit(true)
  }, 1800)
}

function reportLeave(reason) {
  if (!started.value || done.value || leaving) return
  leaving = true
  exitCount.value += 1
  report('leave', { reason, ...envInfo() }, true).then((res) => {
    if (res && res.over) {
      scheduleAutoSubmit()
    } else {
      warnBanner.value = `已记录离开页面（${exitCount.value} / ${proctorCfg.value.max_exits} 次）`
    }
  })
}

function reportReturn() {
  if (!started.value || done.value || !leaving) return
  leaving = false
  report('return', { reason: 'back', ...envInfo() })
  setTimeout(() => { if (!overLimit.value) warnBanner.value = '' }, 2500)
}

function onVisibility() {
  if (document.hidden) reportLeave('visibility')
  else reportReturn()
}
function onBlur() { reportLeave('blur') }
function onFocus() { reportReturn() }
function onFullscreenChange() {
  if (document.fullscreenElement) reportReturn()
  else if (started.value && proctorCfg.value.require_fullscreen) reportLeave('fullscreen')
}
function onCopy() { if (started.value && !done.value) report('copy', { reason: 'copy', ...envInfo() }) }
function onPaste() { if (started.value && !done.value) report('paste', { reason: 'paste', ...envInfo() }) }
function onBeforeUnload(e) {
  if (!started.value || done.value) return
  e.preventDefault()
  e.returnValue = ''
  try {
    if (navigator.sendBeacon) {
      const payload = JSON.stringify({ kind: 'leave', detail: { reason: 'unload', ...envInfo() } })
      navigator.sendBeacon(`/api/practices/${practice.value.id}/proctor/event`,
        new Blob([payload], { type: 'application/json' }))
    }
  } catch { /* ignore */ }
}

async function setupCamera() {
  if (!navigator.mediaDevices?.getUserMedia) {
    mediaState.value.camera = 'unsupported'
    await report('media', { media: 'camera', granted: false, ...envInfo() })
    return
  }
  try {
    cameraStream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: 'user' }, audio: false })
    cameraVideo = document.createElement('video')
    cameraVideo.muted = true
    cameraVideo.playsInline = true
    cameraVideo.srcObject = cameraStream
    await cameraVideo.play().catch(() => {})
    mediaState.value.camera = 'granted'
    await report('media', { media: 'camera', granted: true, ...envInfo() })
  } catch {
    mediaState.value.camera = 'denied'
    await report('media', { media: 'camera', granted: false, ...envInfo() })
  }
}

async function setupScreen() {
  if (!navigator.mediaDevices?.getDisplayMedia) {
    mediaState.value.screen = 'unsupported'
    await report('media', { media: 'screen', granted: false, ...envInfo() })
    return
  }
  try {
    screenStream = await navigator.mediaDevices.getDisplayMedia({ video: true, audio: false })
    screenVideo = document.createElement('video')
    screenVideo.muted = true
    screenVideo.playsInline = true
    screenVideo.srcObject = screenStream
    await screenVideo.play().catch(() => {})
    mediaState.value.screen = 'granted'
    await report('media', { media: 'screen', granted: true, ...envInfo() })
  } catch {
    mediaState.value.screen = 'denied'
    await report('media', { media: 'screen', granted: false, ...envInfo() })
  }
}

function startClock(startedAt) {
  if (examTimer) clearInterval(examTimer)
  const limit = Number(practice.value?.duration) || 0
  if (!limit) {
    examRemaining.value = 0
    return
  }
  const base = new Date(startedAt || Date.now()).getTime()
  const end = base + limit * 60000
  const tick = () => {
    examRemaining.value = Math.max(0, Math.round((end - Date.now()) / 1000))
    if (examRemaining.value <= 0) {
      if (examTimer) clearInterval(examTimer)
      examTimer = null
      if (!done.value && started.value) {
        toastErr('答题时间到，自动交卷')
        submit(true)
      }
    }
  }
  tick()
  examTimer = setInterval(tick, 1000)
}

function bindExamEvents() {
  document.addEventListener('visibilitychange', onVisibility)
  window.addEventListener('blur', onBlur)
  window.addEventListener('focus', onFocus)
  document.addEventListener('fullscreenchange', onFullscreenChange)
  document.addEventListener('copy', onCopy)
  document.addEventListener('paste', onPaste)
  window.addEventListener('beforeunload', onBeforeUnload)
}

function cleanupExam() {
  if (examTimer) { clearInterval(examTimer); examTimer = null }
  if (snapshotTimer) { clearInterval(snapshotTimer); snapshotTimer = null }
  document.removeEventListener('visibilitychange', onVisibility)
  window.removeEventListener('blur', onBlur)
  window.removeEventListener('focus', onFocus)
  document.removeEventListener('fullscreenchange', onFullscreenChange)
  document.removeEventListener('copy', onCopy)
  document.removeEventListener('paste', onPaste)
  window.removeEventListener('beforeunload', onBeforeUnload)
  for (const st of [cameraStream, screenStream]) {
    if (st) st.getTracks().forEach((t) => t.stop())
  }
  cameraStream = screenStream = null
  cameraVideo = screenVideo = null
  if (document.fullscreenElement && document.exitFullscreen) document.exitFullscreen().catch(() => {})
}

async function startExam() {
  if (!practice.value) return
  warnBanner.value = ''
  try {
    // 1) 全屏（必须在用户点击的调用栈里）
    if (proctorCfg.value.require_fullscreen && document.documentElement.requestFullscreen) {
      await document.documentElement.requestFullscreen().catch(() => {})
    }
    // 2) 摄像头 / 屏幕共享
    if (proctorCfg.value.require_camera) await setupCamera()
    if (proctorCfg.value.require_screen) await setupScreen()
    // 3) 通知服务端（幂等，刷新续答时返回首次开始时间）
    const res = await api(`/api/practices/${practice.value.id}/proctor/start`, {
      method: 'POST', json: { detail: envInfo() }
    })
    const att = res?.attempt || {}
    exitCount.value = att.exits || 0
    started.value = true
    if (data.value) {
      data.value.attempt = { ...(data.value.attempt || {}), ...att, started_at: att.started_at || new Date().toISOString() }
    }
    autoSubmitScheduled = false
    startClock(att.started_at)
    bindExamEvents()
    // 4) 定期抓拍（摄像头 / 屏幕共享时每 3 分钟一张）
    if ((mediaState.value.camera === 'granted' || mediaState.value.screen === 'granted') && !snapshotTimer) {
      snapshotTimer = setInterval(() => {
        if (started.value && !done.value) report('snapshot', { reason: 'periodic', ...envInfo() }, true)
      }, 180000)
    }
  } catch (e) {
    toastErr(e)
  }
}

onBeforeUnmount(cleanupExam)

function letter(i) {
  return String.fromCharCode(65 + i)
}

function fmtScore(n) {
  const v = Number(n || 0)
  return v % 1 === 0 ? String(v) : v.toFixed(1)
}

function fmtClock(sec) {
  const s = Math.max(0, Math.floor(sec || 0))
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const ss = s % 60
  return (h > 0 ? h + ':' : '') + String(m).padStart(2, '0') + ':' + String(ss).padStart(2, '0')
}

function mediaLabel(state, kind) {
  const name = kind === 'camera' ? '摄像头' : '屏幕共享'
  if (state === 'granted') return name + '已开启'
  if (state === 'denied') return name + '未授权'
  if (state === 'unsupported') return '浏览器不支持' + name
  return ''
}

function myAnswer(qid) {
  const hit = (submission.value?.answers || []).find((a) => a.question_id === qid)
  return hit ? hit.answer : ''
}

function correctOf(qid) {
  const hit = (submission.value?.answers || []).find((a) => a.question_id === qid)
  return hit ? !!hit.correct : false
}

function optionClass(q, i) {
  const l = letter(i)
  const mine = String(myAnswer(q.id) || '').toUpperCase()
  const right = String(q.answer || '').toUpperCase() === l
  if (reveal.value && right) return 'correct'
  if (mine && mine[0] === l) return reveal.value ? 'wrong' : 'active'
  return ''
}

async function load() {
  loading.value = true
  err.value = ''
  try {
    data.value = await api(`/api/practices/${route.params.id}`)
    for (const q of questions.value) {
      if (!(q.id in answers)) answers[q.id] = ''
    }
  } catch (e) {
    err.value = e.message
  } finally {
    loading.value = false
  }
}
onMounted(load)

// ---------------- 附件 ----------------

async function pickPhotos(e) {
  const files = Array.from(e.target.files || [])
  e.target.value = ''
  if (!files.length) return
  if (photoAtts.value.length + files.length > MAX_PHOTOS) {
    toastErr(`照片最多 ${MAX_PHOTOS} 张`)
    return
  }
  uploadingPhotos.value = true
  try {
    for (const raw of files) {
      if (!raw.type.startsWith('image/')) continue
      const file = await compressImage(raw)
      const meta = await uploadPracticeFile(practice.value.id, file, 'image')
      meta.name = raw.name || '照片'
      attachments.value.push(meta)
    }
  } catch (e) {
    toastErr(e)
  } finally {
    uploadingPhotos.value = false
  }
}

function onAudioUploaded(meta) {
  if (audioAtts.value.length >= MAX_AUDIOS) {
    toastErr(`语音最多 ${MAX_AUDIOS} 段`)
    return
  }
  attachments.value.push(meta)
}

function removeAttachment(att) {
  attachments.value = attachments.value.filter((a) => a.key !== att.key)
}

async function submit(auto = false) {
  if (submitting.value || done.value) return
  if (strict.value && !started.value) {
    toastErr('请先点击「开始答题」')
    return
  }
  if (!auto && questions.value.length) {
    const unanswered = questions.value.filter((q) => !String(answers[q.id] || '').trim()).length
    if (unanswered > 0) {
      const ok = await confirmDialog(`还有 ${unanswered} 题没有作答，确定交卷吗？`, { title: '交卷确认', okText: '交卷' })
      if (!ok) return
    }
  } else if (!auto && !questions.value.length && !attachments.value.length) {
    toastErr('请先拍照或录音，再提交作业')
    return
  }
  submitting.value = true
  try {
    await api(`/api/practices/${practice.value.id}/submit`, {
      method: 'POST',
      json: {
        answers: questions.value.map((q) => ({ question_id: q.id, answer: String(answers[q.id] || '') })),
        attachments: attachments.value
      }
    })
    cleanupExam()
    started.value = false
    toastOk(auto ? '已自动交卷' : '交卷成功')
    data.value = await api(`/api/practices/${route.params.id}`)
  } catch (e) {
    toastErr(e)
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <div class="container">
    <div v-if="loading" class="skeleton" style="height:260px"></div>
    <div v-else-if="err" class="alert error">{{ err }}</div>

    <template v-else-if="practice">
      <div class="row" style="margin-bottom:10px;gap:8px">
        <RouterLink to="/practice" class="btn ghost small"><Icon name="arrow-left" :size="16" /> 练习中心</RouterLink>
        <RouterLink v-if="canManage" :to="`/studio/${practice.course_id}/practice`" class="btn ghost small">
          <Icon name="edit" :size="15" /> 去批改 / 管理
        </RouterLink>
      </div>

      <div class="page-head" style="align-items:flex-start">
        <div>
          <h1 class="page-title">{{ practice.title }}</h1>
          <div class="page-sub row" style="gap:10px;flex-wrap:wrap">
            <span class="practice-kind" :class="practice.kind">
              <Icon :name="practiceKindIcon(practice.kind)" :size="13" /> {{ practiceKindLabel(practice.kind) }}
            </span>
            <span class="badge-soft">{{ subjectLabel(practice.subject) }}</span>
            <span v-if="practice.course_title">{{ practice.course_title }}</span>
            <span>{{ questions.length }} 题 · {{ fmtScore(practice.total_score) }} 分</span>
            <span v-if="practice.deadline">截止 {{ fmtDate(practice.deadline) }}</span>
            <span v-if="practice.duration">建议用时 {{ practice.duration }} 分钟</span>
            <span v-if="practice.allow_photo" class="badge-soft"><Icon name="camera" :size="12" /> 可拍照</span>
            <span v-if="practice.allow_voice" class="badge-soft"><Icon name="mic" :size="12" /> 可语音</span>
            <span v-if="strict" class="badge-soft warn"><Icon name="lock" :size="12" /> 严格监考</span>
          </div>
        </div>
        <div v-if="canManage" class="row" style="gap:8px">
          <button class="btn small" @click="showProctor = true"><Icon name="eye" :size="15" /> 监考台</button>
          <span class="badge-soft">老师预览</span>
        </div>
      </div>

      <div v-if="practice.description" class="card" style="margin-bottom:18px;background:var(--line-2);border:none">
        <div class="muted small" style="white-space:pre-wrap">{{ practice.description }}</div>
      </div>

      <div v-if="preview" class="card" style="margin-bottom:18px;background:#fff7e6;border:1px solid #ffd8a8">
        <div class="row" style="gap:10px;align-items:flex-start">
          <Icon name="target" :size="18" />
          <div>
            <b>老师预览模式</b>
            <p class="muted small" style="margin:4px 0 0">
              这里展示的是题目和标准答案，学员看到的是作答界面。学员交上来的照片 / 语音可在「去批改」里圈画批注。
            </p>
            <p v-if="practice.proctor?.mode === 'strict'" class="muted small" style="margin:4px 0 0">
              已开启严格监考：限时 {{ practice.duration ? practice.duration + ' 分钟' : '不限' }}、允许离开
              {{ practice.proctor.max_exits ?? 0 }} 次<span v-if="practice.proctor.require_camera">、摄像头抓拍</span><span v-if="practice.proctor.require_screen">、屏幕共享抓拍</span>。
              点右上角「监考台」可以看学员的离开次数、时间线和截图。
            </p>
          </div>
        </div>
      </div>

      <!-- 严格模式：开始答题前的须知 -->
      <div v-if="strict && !done && !started" class="card exam-rules">
        <div class="row" style="gap:12px;align-items:flex-start">
          <span class="exam-lock"><Icon name="lock" :size="24" /></span>
          <div style="flex:1">
            <h3 style="margin:0 0 6px">严格监考模式</h3>
            <ul class="exam-rule-list">
              <li v-if="practice.duration">限时 <b>{{ practice.duration }} 分钟</b>，时间到自动交卷</li>
              <li v-if="proctorCfg.require_fullscreen">答题期间必须保持 <b>全屏</b>，退出全屏会被记录</li>
              <li>允许离开页面 <b>{{ proctorCfg.max_exits }}</b> 次，超过后自动交卷</li>
              <li v-if="proctorCfg.require_camera">需要开启 <b>摄像头</b>，离开页面时会抓拍一张照片</li>
              <li v-if="proctorCfg.require_screen">需要共享 <b>屏幕</b>（浏览器会弹窗确认）</li>
              <li>切屏 / 最小化 / 退出全屏都会记录时间、IP、浏览器与屏幕信息，老师可查看</li>
            </ul>
            <div v-if="attemptStarted" class="alert info" style="margin:12px 0 0">
              你之前已经开始答题，点击「继续答题」会沿用原来的开始时间继续计时。
            </div>
            <button class="btn primary lg" style="margin-top:14px" @click="startExam">
              <Icon name="play" :size="17" /> {{ attemptStarted ? '继续答题' : '开始答题' }}
            </button>
          </div>
        </div>
      </div>

      <!-- 严格模式：答题中的监考状态条 -->
      <div v-if="strict && started && !done" class="proctor-bar">
        <span class="pb-item"><Icon name="clock" :size="15" />
          {{ practice.duration ? '剩余 ' + fmtClock(examRemaining) : '不限时' }}
        </span>
        <span class="pb-item" :class="{ danger: exitCount > proctorCfg.max_exits }">
          <Icon name="eye" :size="15" /> 已离开 {{ exitCount }} / {{ proctorCfg.max_exits }}
        </span>
        <span v-if="proctorCfg.require_camera" class="pb-item" :class="{ warn: mediaState.camera !== 'granted' }">
          <Icon :name="mediaState.camera === 'granted' ? 'camera' : 'camera-off'" :size="15" />
          {{ mediaLabel(mediaState.camera, 'camera') }}
        </span>
        <span v-if="proctorCfg.require_screen" class="pb-item" :class="{ warn: mediaState.screen !== 'granted' }">
          <Icon name="video-file" :size="15" /> {{ mediaLabel(mediaState.screen, 'screen') }}
        </span>
        <span style="flex:1"></span>
        <span class="muted small">请勿切换页面</span>
      </div>
      <div v-if="warnBanner" class="alert warn exam-warn"><Icon name="eye" :size="16" /> {{ warnBanner }}</div>

      <!-- 成绩 / 状态 -->
      <div v-if="submission" class="card result-banner" :class="{ pending: !submission.graded }">
        <template v-if="submission.graded">
          <div class="row" style="justify-content:space-between;align-items:flex-start;gap:14px;flex-wrap:wrap">
            <div>
              <div class="muted small">本次得分</div>
              <div class="score-big">{{ fmtScore(submission.score) }}<span class="muted" style="font-size:16px"> / {{ fmtScore(submission.total) }}</span></div>
            </div>
            <div v-if="submission.comment" class="grade-comment">
              <div class="muted small">老师评语</div>
              <div style="white-space:pre-wrap">{{ submission.comment }}</div>
            </div>
          </div>
        </template>
        <template v-else>
          <div class="row" style="gap:8px">
            <Icon name="clock" :size="20" />
            <b>已交卷，等待老师批改</b>
            <span class="muted small">
              <template v-if="questions.length">客观题已得 {{ fmtScore(submission.score) }} 分，简答题 / 照片批改后出总分</template>
              <template v-else>老师批改照片 / 语音后会给出评语和分数</template>
            </span>
          </div>
        </template>
      </div>

      <!-- 老师语音评语 -->
      <div v-if="submission && submission.graded && submission.feedback_audio" class="card" style="margin-bottom:18px">
        <div class="panel-title" style="margin:0 0 8px"><Icon name="mic" :size="16" /> 老师语音评语</div>
        <audio :src="`/api/practices/${practice.id}/submissions/${submission.id}/feedback-audio`" controls
          preload="metadata" style="width:100%;max-width:520px"></audio>
        <p v-if="submission.feedback_audio_duration" class="muted small" style="margin:6px 0 0">
          时长 {{ submission.feedback_audio_duration }} 秒
        </p>
      </div>

      <!-- 我的附件 -->
      <div v-if="submission && submission.attachments && submission.attachments.length" class="card" style="margin-bottom:18px">
        <div class="panel-title" style="margin:0 0 10px"><Icon name="camera" :size="16" /> 我提交的作业</div>
        <AttachmentList :attachments="submission.attachments" :annotations="submission.annotations || {}" />
        <p v-if="!submission.graded" class="muted small" style="margin:8px 0 0">老师批改后，这里会显示圈画批注。</p>
      </div>

      <!-- 题目（严格模式下开始答题后才显示） -->
      <template v-if="!strict || started">
      <div v-for="(q, qi) in questions" :key="q.id" class="q-block">
        <div class="row" style="justify-content:space-between;align-items:flex-start">
          <div class="q-stem">
            {{ qi + 1 }}. <FormulaText :text="q.stem" />
            <span class="muted small">（{{ q.type === 'short' ? '简答' : q.type === 'choice' ? '选择' : q.type === 'judge' ? '判断' : '填空' }} · {{ q.score }} 分）</span>
            <button v-if="hasEnglish(q.stem)" class="speak-btn" type="button" title="朗读" @click="speak(q.stem)">
              <Icon name="volume" :size="14" />
            </button>
          </div>
          <span v-if="done && reveal && !preview && q.type !== 'short'" class="badge-soft" :class="correctOf(q.id) ? 'ok' : 'bad'">
            {{ correctOf(q.id) ? '正确' : '错误' }}
          </span>
          <span v-else-if="done && !preview && q.type === 'short'" class="badge-soft">老师评分</span>
        </div>

        <!-- 作答 -->
        <template v-if="!done && !preview">
          <div v-if="q.type === 'choice'" class="q-options">
            <div v-for="(opt, i) in q.options" :key="i" class="opt-row"
              :class="{ active: answers[q.id] === letter(i) }" @click="answers[q.id] = letter(i)">
              <span class="opt-letter">{{ letter(i) }}</span>
              <span><FormulaText :text="opt" /></span>
              <button v-if="hasEnglish(opt)" class="speak-btn" type="button" title="朗读" @click.stop="speak(opt)">
                <Icon name="volume" :size="14" />
              </button>
            </div>
          </div>
          <div v-else-if="q.type === 'judge'" class="row" style="gap:10px;margin-top:10px">
            <button class="btn" :class="{ primary: answers[q.id] === 'true' }" @click="answers[q.id] = 'true'">对</button>
            <button class="btn" :class="{ primary: answers[q.id] === 'false' }" @click="answers[q.id] = 'false'">错</button>
          </div>
          <input v-else-if="q.type === 'fill'" v-model="answers[q.id]" class="input" style="margin-top:10px"
            placeholder="填写答案" />
          <textarea v-else v-model="answers[q.id]" class="textarea" style="margin-top:10px"
            placeholder="写下你的答案…"></textarea>
        </template>

        <!-- 结果 / 预览 -->
        <template v-else>
          <div v-if="q.type === 'choice'" class="q-options">
            <div v-for="(opt, i) in q.options" :key="i" class="opt-row" :class="preview ? (q.answer === letter(i) ? 'correct' : '') : optionClass(q, i)">
              <span class="opt-letter">{{ letter(i) }}</span>
              <span><FormulaText :text="opt" /></span>
              <button v-if="hasEnglish(opt)" class="speak-btn" type="button" title="朗读" @click.stop="speak(opt)">
                <Icon name="volume" :size="14" />
              </button>
            </div>
          </div>
          <div v-if="!preview" class="q-answer-row">
            <span class="muted small">我的答案：</span>
            <b :class="{ 'ans-empty': !myAnswer(q.id) }">
              <FormulaText :text="myAnswer(q.id) || '未作答'" />
            </b>
          </div>
          <div v-if="reveal && q.answer" class="q-answer-row">
            <span class="muted small" :style="{ marginLeft: preview ? '0px' : '18px' }">参考答案：</span>
            <b style="color:var(--brand-700)">
              <FormulaText :text="q.type === 'judge' ? (q.answer === 'true' ? '对' : '错') : q.answer" />
            </b>
          </div>
          <div v-if="reveal && q.analysis" class="q-analysis">
            <span class="muted small">解析：</span><FormulaText :text="q.analysis" />
          </div>
        </template>
      </div>
      </template>

      <!-- 作业附件提交 -->
      <div v-if="!done && !preview && (!strict || started) && (practice.allow_photo || practice.allow_voice)" class="card" style="margin-top:18px">
        <div class="panel-title" style="margin:0 0 6px"><Icon name="camera" :size="17" /> 拍照 / 语音交作业</div>
        <p class="muted small" style="margin:0 0 12px">纸笔作业拍照上传，英语朗读可以录语音；照片超过 3MB 会自动压缩。</p>

        <template v-if="practice.allow_photo">
          <input ref="cameraInput" type="file" accept="image/*" capture="environment" multiple style="display:none" @change="pickPhotos" />
          <input ref="galleryInput" type="file" accept="image/*" multiple style="display:none" @change="pickPhotos" />
          <div class="row" style="gap:8px;margin-bottom:10px">
            <button class="btn small" @click="cameraInput?.click()"><Icon name="camera" :size="15" /> 拍照</button>
            <button class="btn small" @click="galleryInput?.click()"><Icon name="image" :size="15" /> 从相册选择</button>
            <span class="muted small">{{ photoAtts.length }} / {{ MAX_PHOTOS }} 张</span>
            <span v-if="uploadingPhotos" class="muted small">上传中…</span>
          </div>
          <div v-if="photoAtts.length" class="photo-grid">
            <div v-for="att in photoAtts" :key="att.key" class="photo-tile">
              <img :src="practiceFileUrl(att.key)" :alt="att.name || '作业照片'" />
              <button class="photo-del" type="button" title="移除" @click="removeAttachment(att)"><Icon name="close" :size="13" /></button>
            </div>
          </div>
        </template>

        <template v-if="practice.allow_voice">
          <div class="divider" style="margin:14px 0"></div>
          <div class="row" style="justify-content:space-between;margin-bottom:8px">
            <b class="small"><Icon name="mic" :size="15" /> 语音作业</b>
            <span class="muted small">{{ audioAtts.length }} / {{ MAX_AUDIOS }} 段</span>
          </div>
          <AudioRecorder :practice-id="practice.id" @uploaded="onAudioUploaded" />
          <div v-if="audioAtts.length" style="margin-top:10px">
            <AttachmentList :attachments="audioAtts" />
            <div class="row" style="gap:6px;margin-top:6px">
              <button v-for="att in audioAtts" :key="att.key" class="btn small ghost" @click="removeAttachment(att)">
                <Icon name="trash" :size="13" /> 删除 {{ att.name || '语音' }}
              </button>
            </div>
          </div>
        </template>
      </div>

      <!-- 交卷 -->
      <div v-if="!done && !preview && (!strict || started)" class="card" style="position:sticky;bottom:14px;display:flex;justify-content:space-between;align-items:center;box-shadow:var(--shadow);margin-top:18px">
        <span class="muted small">
          <template v-if="strict">答题过程中请保持全屏，离开页面会被记录；离开次数超限或时间到会自动交卷</template>
          <template v-else-if="questions.length">交卷后客观题自动判分；含简答题或照片 / 语音时由老师批改后出总分</template>
          <template v-else>提交后老师会批改照片 / 语音并给评语</template>
        </span>
        <button class="btn primary lg" :disabled="submitting || uploadingPhotos" @click="submit(false)">
          <Icon name="check" :size="17" /> {{ submitting ? '提交中…' : '提交作业' }}
        </button>
      </div>

      <!-- 老师监考台 -->
      <ProctorDashboard v-if="showProctor" :practice-id="practice.id" :title="practice.title" @close="showProctor = false" />
    </template>
  </div>
</template>
