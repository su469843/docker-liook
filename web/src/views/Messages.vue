<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { api, postForm } from '../api'
import { onWS, sendWS, wsState } from '../ws'
import { fmtDur, fmtRel, fmtTime } from '../format'
import { compressImage } from '../files'
import { markChatRead, markGroupRead, refreshUnread, store } from '../store'
import { toastErr } from '../ui'
import { STICKERS, plainText, stickerToken } from '../stickers'
import GroupChat from '../components/GroupChat.vue'
import Icon from '../components/Icon.vue'
import Sticker from '../components/Sticker.vue'
import MessageBody from '../components/MessageBody.vue'
import ImageLightbox from '../components/ImageLightbox.vue'

// ---------------- 状态 ----------------
const route = useRoute()
const tab = ref('private')
const contacts = ref([])
const groups = ref([])
const activeContact = ref(null)
const activeGroup = ref(null)
const messages = ref([])
const input = ref('')
const search = ref('')
const showStickers = ref(false)
const preview = ref('')
const listEl = ref(null)
const fileEl = ref(null)
const loading = ref(true)
let off = null

const hasActive = computed(() => !!(activeContact.value || activeGroup.value))
const myID = computed(() => store.user?.id)
const online = computed(() => new Set(store.online))
const filteredContacts = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return contacts.value
  return contacts.value.filter((c) => (c.display_name || '').toLowerCase().includes(q))
})
const filteredGroups = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return groups.value
  return groups.value.filter((g) => (g.title || '').toLowerCase().includes(q))
})

function insertSticker(id) {
  input.value += stickerToken(id)
}

// ---------------- 数据加载 ----------------
async function loadContacts() {
  try { contacts.value = (await api('/api/contacts')) || [] } catch (e) { toastErr(e) }
}
async function loadGroups() {
  try { groups.value = (await api('/api/groups')) || [] } catch (e) { toastErr(e) }
}
async function load() {
  loading.value = true
  await Promise.all([loadContacts(), loadGroups()])
  loading.value = false
  // 支持从课程成员 / 作业批改页直接带着 ?peer=用户ID 进来私聊
  const peerID = Number(route.query.peer || 0)
  const peer = peerID ? contacts.value.find((c) => c.id === peerID) : null
  if (peer) openPrivate(peer)
  else if (contacts.value.length) openPrivate(contacts.value[0])
  else if (groups.value.length) openGroup(groups.value[0])
}

function scrollBottom() {
  nextTick(() => { if (listEl.value) listEl.value.scrollTop = listEl.value.scrollHeight })
}

async function openPrivate(c) {
  tab.value = 'private'
  activeContact.value = c
  activeGroup.value = null
  c.unread = 0
  showStickers.value = false
  try {
    // 拉历史时后端会一并标记已读
    messages.value = (await api(`/api/chat/${c.id}`)) || []
  } catch (e) {
    toastErr(e)
  }
  refreshUnread()
  scrollBottom()
}

function openGroup(g) {
  tab.value = 'group'
  activeGroup.value = g
  activeContact.value = null
  g.unread = 0
  markGroupRead(g.course_id) // 落库后刷新顶部角标，红点才真的灭
}

// ---------------- 私聊发送 ----------------
function sendText() {
  const content = input.value.trim()
  if (!content || !activeContact.value) return
  if (!sendWS({ type: 'chat.send', to: activeContact.value.id, msg: { type: 'text', content } })) {
    toastErr('连接已断开，正在重连…')
    return
  }
  input.value = ''
}

async function pickImage(ev) {
  const raw = ev.target.files[0]
  ev.target.value = ''
  if (!raw || !activeContact.value) return
  try {
    // 超过 3MB 的图片先压缩，聊天发图和作业交图都不容易失败
    const f = await compressImage(raw)
    const fd = new FormData()
    fd.append('file', f)
    const res = await postForm('/api/upload/image', fd)
    if (!sendWS({ type: 'chat.send', to: activeContact.value.id, msg: { type: 'image', content: res.url } })) {
      toastErr('连接已断开，正在重连…')
    }
  } catch (e) {
    toastErr(e)
  }
}

function onKeydown(e) {
  if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
    e.preventDefault()
    sendText()
  }
}

// 快捷短语：老师/学员常用的话一点就填，省得每次手打
const quickReplies = computed(() => store.user?.role === 'teacher'
  ? ['今天作业记得提交哦', '上课时间有调整，注意查看', '这部分再想想，加油', '已收到，晚点批改']
  : ['老师您好，这道题不太懂', '收到，谢谢老师', '作业已提交，请老师查收'])

function useQuick(text) {
  input.value = input.value.trim() ? input.value.trimEnd() + ' ' + text : text
}

// ---------------- WebSocket ----------------
function handleServerMsg(m) {
  switch (m.type) {
    case 'chat.sent':
      if (activeContact.value && m.msg && m.msg.receiver_id === activeContact.value.id) {
        messages.value.push(m.msg)
        scrollBottom()
      }
      break
    case 'chat.new':
      if (activeContact.value && m.from && m.from.id === activeContact.value.id) {
        messages.value.push(m.msg)
        scrollBottom()
        sendWS({ type: 'chat.read', to: m.from.id })
      } else if (m.from) {
        const c = contacts.value.find((x) => x.id === m.from.id)
        if (c) c.unread = (c.unread || 0) + 1
        refreshUnread()
      }
      break
    case 'group.new':
      if (m.from) {
        const g = groups.value.find((x) => x.course_id === m.group)
        if (g) {
          g.last_content = (m.gmsg && m.gmsg.content) || ''
          g.last_type = (m.gmsg && m.gmsg.type) || 'text'
          g.last_sender = (m.from && m.from.display_name) || ''
          g.last_at = (m.gmsg && m.gmsg.created_at) || new Date().toISOString()
          g.last_id = (m.gmsg && m.gmsg.id) || 0
          if (!activeGroup.value || activeGroup.value.course_id !== m.group) g.unread = (g.unread || 0) + 1
        } else {
          loadGroups()
        }
        refreshUnread()
      }
      break
    case 'call.offer': onIncomingCall(m); break
    case 'call.answer': onCallAnswer(m); break
    case 'call.ice': onCallIce(m); break
    case 'call.end': onCallEnd(m); break
    case 'call.busy':
      teardownCall()
      flashError(m.message || '对方忙')
      break
    case 'error':
      flashError(m.message || '操作失败')
      break
  }
}

// ---------------- 音视频通话（WebRTC 1v1） ----------------
const call = ref(null)
const callError = ref('')
const localVideoEl = ref(null)
const remoteVideoEl = ref(null)
const remoteAudioEl = ref(null)
const micOn = ref(true)
const camOn = ref(true)
const netState = ref('')          // 连接状态：连接中… / 已连接 / 网络中断…
const remoteHasVideo = ref(false)
let pc = null
let localStream = null
let remoteStream = null
let pendingOffer = null
let audioCtx = null
let iceCache = null
let pendingRemoteICE = []
let errTimer = null
let stateTimer = null

const DEFAULT_ICE = [{ urls: 'stun:stun.l.google.com:19302' }]

const callStatusText = computed(() => {
  if (!call.value) return ''
  if (call.value.status === 'incoming') return '来电'
  if (call.value.status === 'outgoing') return '等待对方接听…'
  return netState.value ? '通话中 · ' + netState.value : '通话中'
})

function flashError(msg, ms = 6000) {
  clearTimeout(errTimer)
  callError.value = msg
  errTimer = setTimeout(() => { callError.value = '' }, ms)
}
function clearCallError() {
  clearTimeout(errTimer)
  errTimer = null
  callError.value = ''
}

function mediaSupported() {
  return !!(navigator.mediaDevices && typeof navigator.mediaDevices.getUserMedia === 'function')
}
function mediaErrText(e) {
  if (typeof window !== 'undefined' && window.isSecureContext === false) {
    return '当前页面不是 HTTPS 安全上下文，浏览器禁止使用摄像头/麦克风（请用 https 或 localhost 访问）'
  }
  const n = (e && e.name) || ''
  if (!n) return '当前浏览器不支持摄像头/麦克风，请使用新版 Chrome / Edge / Safari'
  if (n === 'NotAllowedError' || n === 'SecurityError') return '摄像头/麦克风权限被拒绝，请在浏览器地址栏允许后重试'
  if (n === 'NotFoundError' || n === 'OverconstrainedError') return '没有找到可用的摄像头或麦克风'
  if (n === 'NotReadableError' || n === 'TrackStartError') return '摄像头/麦克风被其它程序占用，请关闭其它会议/录制软件后重试'
  return '无法访问摄像头/麦克风：' + ((e && e.message) || e)
}

async function getICE() {
  try {
    const cfg = await api('/api/ice')
    return cfg.iceServers?.length ? cfg.iceServers : DEFAULT_ICE
  } catch {
    return DEFAULT_ICE
  }
}

// 打开本地采集：摄像头不可用（没设备/被占用）时自动降级为纯语音，避免整通电话直接失败
async function openLocal(wantVideo) {
  const audio = { echoCancellation: true, noiseSuppression: true, autoGainControl: true }
  if (!wantVideo) return { stream: await navigator.mediaDevices.getUserMedia({ audio }), video: false }
  try {
    const stream = await navigator.mediaDevices.getUserMedia({
      audio,
      video: { width: { ideal: 1280 }, height: { ideal: 720 }, facingMode: 'user' },
    })
    return { stream, video: stream.getVideoTracks().length > 0 }
  } catch {
    const stream = await navigator.mediaDevices.getUserMedia({ audio })
    return { stream, video: false }
  }
}

function beepStart() {
  try {
    audioCtx = audioCtx || new AudioContext()
    audioCtx.resume?.()
    const osc = audioCtx.createOscillator()
    const gain = audioCtx.createGain()
    osc.frequency.value = 660
    gain.gain.value = 0.06
    osc.connect(gain).connect(audioCtx.destination)
    osc.start()
    beepStart.stop = () => { try { osc.stop() } catch {} }
  } catch {}
}
function beepStop() {
  if (beepStart.stop) { beepStart.stop(); beepStart.stop = null }
}

function playEl(el) {
  try {
    const p = el.play?.()
    if (p && p.catch) p.catch(() => {})
  } catch {}
}

// 把媒体流挂到 <video> / <audio> 上。
// 关键：通话界面（含 video/audio 元素）是在 call.status 变化之后才渲染的，
// ontrack 往往比渲染更早触发，所以这里可能被调用多次，必须幂等且可重复。
function attachMedia() {
  const lv = localVideoEl.value
  if (lv && localStream) {
    if (lv.srcObject !== localStream) lv.srcObject = localStream
    playEl(lv)
  }
  const el = call.value?.video ? remoteVideoEl.value : remoteAudioEl.value
  if (el && remoteStream) {
    if (el.srcObject !== remoteStream) el.srcObject = remoteStream
    playEl(el) // 有些浏览器（iOS Safari）首次自动播放会被拦，点击画面可恢复
  }
}

function newPC(peerId) {
  const conn = new RTCPeerConnection({ iceServers: iceCache || DEFAULT_ICE })
  pc = conn
  remoteHasVideo.value = false
  conn.onicecandidate = (ev) => {
    if (ev.candidate) sendWS({ type: 'call.ice', to: peerId, candidate: JSON.stringify(ev.candidate) })
  }
  conn.ontrack = (ev) => {
    remoteStream = (ev.streams && ev.streams[0]) || new MediaStream([ev.track])
    if (ev.track.kind === 'video') remoteHasVideo.value = true
    attachMedia()
    nextTick(attachMedia)
  }
  conn.onconnectionstatechange = () => {
    if (pc !== conn) return
    switch (conn.connectionState) {
      case 'connected':
        netState.value = '已连接'
        clearTimeout(stateTimer)
        attachMedia()
        break
      case 'connecting':
        netState.value = '连接中…'
        break
      case 'disconnected':
        netState.value = '网络中断…'
        break
      case 'failed':
        teardownCall()
        flashError('音视频连接失败：可能是 NAT 穿透失败，可让管理员在服务端配置 TURN_URL / TURN_USER / TURN_PASS 后重试')
        break
    }
  }
  return conn
}

async function flushRemoteICE() {
  const list = pendingRemoteICE
  pendingRemoteICE = []
  for (const cand of list) {
    try { await pc?.addIceCandidate(cand) } catch {}
  }
}

async function startCall(video) {
  if (!activeContact.value || call.value) return
  if (!mediaSupported()) { flashError(mediaErrText()); return }
  clearCallError()
  netState.value = ''
  pendingRemoteICE = []
  remoteStream = null
  const peer = activeContact.value
  // 先进入通话界面：用户在授权弹窗期间也能看到状态
  call.value = { peer, role: 'caller', video: !!video, status: 'outgoing' }
  clearTimeout(stateTimer)
  stateTimer = setTimeout(() => {
    if (call.value && call.value.role === 'caller' && call.value.status !== 'active') {
      flashError('对方还没有接听（被叫方需要保持页面在线、允许摄像头和麦克风）')
    }
  }, 25000)
  try {
    const opened = await openLocal(!!video)
    localStream = opened.stream
    camOn.value = opened.video
    if (video && !opened.video) {
      call.value.video = false
      flashError('未检测到可用摄像头，已改为语音通话')
    }
    if (!iceCache) iceCache = await getICE()
    await nextTick()
    attachMedia()
    newPC(peer.id)
    localStream.getTracks().forEach((t) => pc.addTrack(t, localStream))
    const offer = await pc.createOffer()
    await pc.setLocalDescription(offer)
    sendWS({ type: 'call.offer', to: peer.id, sdp: JSON.stringify(offer), video: !!call.value.video })
  } catch (e) {
    teardownCall()
    flashError(mediaErrText(e))
  }
}

function onIncomingCall(m) {
  if (call.value) { sendWS({ type: 'call.end', to: m.from.id }); return }
  clearCallError()
  clearTimeout(stateTimer)
  netState.value = ''
  remoteStream = null
  pendingRemoteICE = []
  pendingOffer = m.sdp
  call.value = { peer: m.from, role: 'callee', video: !!m.video, status: 'incoming' }
  camOn.value = !!m.video
  beepStart()
  stateTimer = setTimeout(() => {
    if (call.value && call.value.status === 'incoming') {
      declineCall()
      flashError('来电长时间无人接听，已自动挂断')
    }
  }, 45000)
}

async function acceptCall() {
  if (!call.value || call.value.status !== 'incoming' || !pendingOffer) return
  clearCallError()
  beepStop()
  clearTimeout(stateTimer)
  const peerId = call.value.peer.id
  const wantVideo = !!call.value.video
  const offerSDP = pendingOffer
  if (!mediaSupported()) { declineCall(); flashError(mediaErrText()); return }
  try {
    const opened = await openLocal(wantVideo)
    localStream = opened.stream
    camOn.value = opened.video
    if (wantVideo && !opened.video) flashError('你的摄像头不可用，本次只发送语音（仍能看到对方画面）')
    if (!iceCache) iceCache = await getICE() // 被叫方同样需要 TURN 配置
    newPC(peerId)
    localStream.getTracks().forEach((t) => pc.addTrack(t, localStream))
    await pc.setRemoteDescription(JSON.parse(offerSDP))
    await flushRemoteICE()
    const answer = await pc.createAnswer()
    await pc.setLocalDescription(answer)
    sendWS({ type: 'call.answer', to: peerId, sdp: JSON.stringify(answer), video: wantVideo })
    pendingOffer = null
    if (!call.value) return
    call.value.status = 'active'
    netState.value = '连接中…'
    await nextTick()
    attachMedia() // 界面已渲染，把本地预览 + 远端流挂上去
  } catch (e) {
    declineCall()
    flashError('接听失败：' + ((e && e.message) || e))
  }
}

function declineCall() {
  if (call.value) sendWS({ type: 'call.end', to: call.value.peer.id })
  teardownCall()
}

function onCallAnswer(m) {
  if (!pc || !call.value || call.value.role !== 'caller') return
  pc.setRemoteDescription(JSON.parse(m.sdp))
    .then(async () => {
      await flushRemoteICE()
      if (!call.value) return
      call.value.status = 'active'
      await nextTick()
      attachMedia()
    })
    .catch((e) => flashError('建立连接失败：' + ((e && e.message) || e)))
}

function onCallIce(m) {
  let cand = m.candidate
  if (typeof cand === 'string') {
    try { cand = JSON.parse(cand) } catch { return }
  }
  if (!cand || typeof cand !== 'object') return
  if (pc && pc.remoteDescription) pc.addIceCandidate(cand).catch(() => {})
  else pendingRemoteICE.push(cand)
}

function onCallEnd(m) {
  if (!call.value || call.value.peer.id !== m.from_id) return
  teardownCall()
  flashError(m.message || '通话已结束', 3500)
}

function hangup() {
  if (call.value) sendWS({ type: 'call.end', to: call.value.peer.id })
  teardownCall()
  clearCallError()
}

function teardownCall() {
  beepStop()
  clearTimeout(stateTimer)
  stateTimer = null
  pendingOffer = null
  pendingRemoteICE = []
  remoteStream = null
  remoteHasVideo.value = false
  micOn.value = true
  camOn.value = true
  netState.value = ''
  for (const el of [localVideoEl.value, remoteVideoEl.value, remoteAudioEl.value]) {
    if (el) { try { el.srcObject = null } catch {} }
  }
  if (localStream) {
    localStream.getTracks().forEach((t) => t.stop())
    localStream = null
  }
  if (pc) { try { pc.close() } catch {} pc = null }
  call.value = null
}

// 通话界面渲染完成 / 切换（语音↔视频）后重新挂载媒体流
watch(
  () => [call.value?.status, call.value?.video],
  () => { nextTick(attachMedia) },
  { flush: 'post' },
)

function toggleMic() {
  micOn.value = !micOn.value
  localStream?.getAudioTracks().forEach((t) => (t.enabled = micOn.value))
}
function toggleCam() {
  camOn.value = !camOn.value
  localStream?.getVideoTracks().forEach((t) => (t.enabled = camOn.value))
}

// ---------------- 生命周期 ----------------
onMounted(() => {
  load()
  off = onWS(handleServerMsg)
})
onBeforeUnmount(() => {
  if (off) off()
  teardownCall()
})

const roleOf = (u) => ({ admin: '管理员', teacher: '老师', student: '学员' }[u?.role] || '')
</script>

<template>
  <div>
    <div class="im-layout" :class="{ 'has-active': hasActive }">
      <aside class="im-list">
        <div class="im-search">
          <input v-model="search" class="input" placeholder="搜索联系人 / 课程群…" style="height:34px" />
        </div>
        <div class="im-list-tabs">
          <button :class="{ active: tab === 'private' }" @click="tab = 'private'">私聊</button>
          <button :class="{ active: tab === 'group' }" @click="tab = 'group'">课程群</button>
        </div>
        <div class="im-scroll">
          <template v-if="tab === 'private'">
            <div v-if="loading" class="muted center" style="padding:24px">加载中…</div>
            <div v-else-if="!filteredContacts.length" class="group-note" style="margin:14px">
              {{ search ? '没有匹配的联系人' : '加入课程后，这里会出现老师；老师这里会出现自己的学员。' }}
            </div>
            <div v-for="c in filteredContacts" :key="c.id" class="im-item"
              :class="{ active: activeContact && activeContact.id === c.id }" @click="openPrivate(c)">
              <span class="avatar" :class="c.role">{{ (c.display_name || '?')[0] }}</span>
              <div class="im-item-main">
                <div class="im-item-name">
                  {{ c.display_name }}
                  <span v-if="online.has(c.id)" class="pill green" style="font-size:10.5px">在线</span>
                </div>
                <div class="im-item-last">{{ roleOf(c) }}</div>
              </div>
              <span v-if="c.unread" class="unread-pill">{{ c.unread > 99 ? '99+' : c.unread }}</span>
            </div>
          </template>

          <template v-else>
            <div v-if="!filteredGroups.length" class="group-note" style="margin:14px">
              {{ search ? '没有匹配的课程群' : '还没有课程群。老师创建课程、学员加入后，这里会出现课程群聊。' }}
            </div>
            <div v-for="g in filteredGroups" :key="g.course_id" class="im-item"
              :class="{ active: activeGroup && activeGroup.course_id === g.course_id }" @click="openGroup(g)">
              <span class="avatar teacher">群</span>
              <div class="im-item-main">
                <div class="im-item-name">{{ g.title }}</div>
                <div class="im-item-last">
                  <template v-if="g.last_id">{{ g.last_sender }}：{{ g.last_type === 'image' ? '[图片]' : plainText(g.last_content) }}</template>
                  <template v-else>{{ g.member_count }} 人在群 · 暂无消息</template>
                </div>
              </div>
              <div style="display:flex;flex-direction:column;align-items:flex-end;gap:4px">
                <span class="im-item-time">{{ fmtRel(g.last_at) }}</span>
                <span v-if="g.unread" class="unread-pill">{{ g.unread > 99 ? '99+' : g.unread }}</span>
              </div>
            </div>
          </template>
        </div>
      </aside>

      <section class="im-win">
        <!-- 私聊 -->
        <template v-if="tab === 'private' && activeContact">
          <div class="im-head">
            <button class="btn ghost small" @click="activeContact = null" style="margin-right:4px"><Icon name="arrow-left" :size="16" /></button>
            <span class="avatar" :class="activeContact.role">{{ (activeContact.display_name || '?')[0] }}</span>
            <div class="im-item-main">
              <div class="im-title">{{ activeContact.display_name }}</div>
              <div class="muted small">
                {{ roleOf(activeContact) }}
                {{ online.has(activeContact.id) ? ' · 在线' : ' · 离线' }}
              </div>
            </div>
            <button class="btn small" :disabled="!!call" @click="startCall(false)"><Icon name="phone" :size="15" /> 语音</button>
            <button class="btn small primary" :disabled="!!call" @click="startCall(true)"><Icon name="camcorder" :size="15" /> 视频</button>
          </div>

          <div class="msg-list" ref="listEl">
            <div v-if="!messages.length" class="group-note" style="align-self:center">
              和老师/同学打个招呼吧，支持文字、表情、图片和音视频通话。
            </div>
            <div v-for="m in messages" :key="m.id" class="msg-row" :class="{ mine: m.sender_id === myID }">
              <span v-if="m.sender_id !== myID" class="avatar sm" :class="activeContact.role">{{ (activeContact.display_name || '?')[0] }}</span>
              <div class="msg-stack">
                <div class="bubble">
                  <img v-if="m.type === 'image'" :src="m.content" alt="图片" @click="preview = m.content" />
                  <MessageBody v-else :content="m.content" />
                </div>
                <span class="msg-time">{{ fmtTime(m.created_at) }}</span>
              </div>
            </div>
          </div>

          <div class="composer">
            <div v-if="showStickers" class="sticker-panel">
              <div class="sticker-grid">
                <button v-for="s in STICKERS" :key="s.id" type="button" :title="s.label" @click="insertSticker(s.id)">
                  <Sticker :id="s.id" :size="34" />
                </button>
              </div>
            </div>
            <div class="composer-tools">
              <button class="btn-link" type="button" @click="showStickers = !showStickers"><Icon name="sticker" :size="16" /> 表情</button>
              <button class="btn-link" type="button" @click="fileEl.click()"><Icon name="image" :size="16" /> 图片</button>
              <span class="spacer" style="flex:1"></span>
              <span class="muted small">{{ wsState.connected ? '已连接' : '连接中…' }}</span>
            </div>
            <div class="quick-replies">
              <button v-for="q in quickReplies" :key="q" type="button" class="quick-chip" @click="useQuick(q)">{{ q }}</button>
            </div>
            <div class="composer-input">
              <textarea v-model="input" rows="1" placeholder="输入消息…（Enter 发送，Shift+Enter 换行）" @keydown="onKeydown"></textarea>
              <button class="btn primary" type="button" @click="sendText"><Icon name="send" :size="16" /> 发送</button>
            </div>
            <input ref="fileEl" type="file" accept="image/png,image/jpeg,image/gif,image/webp" hidden @change="pickImage" />
          </div>
        </template>

        <!-- 课程群 -->
        <template v-else-if="tab === 'group' && activeGroup">
          <div class="im-head">
            <button class="btn ghost small" @click="activeGroup = null" style="margin-right:4px"><Icon name="arrow-left" :size="16" /></button>
            <span class="avatar teacher">群</span>
            <div class="im-item-main">
              <div class="im-title">{{ activeGroup.title }}</div>
              <div class="muted small">{{ activeGroup.member_count }} 人在群 · {{ activeGroup.teacher_name }} 老师</div>
            </div>
            <RouterLink :to="'/course/' + activeGroup.course_id" class="btn small"><Icon name="book" :size="15" /> 课程主页</RouterLink>
          </div>
          <GroupChat :course-id="activeGroup.course_id" :course="activeGroup" />
        </template>

        <div v-else class="empty" style="margin:auto;border:none;background:none">
          <span class="empty-ico"><Icon name="chat" :size="44" /></span>
          从左侧选择一个会话开始聊天
        </div>
      </section>
    </div>

    <p v-if="callError" class="alert error" style="position:fixed;bottom:20px;left:50%;transform:translateX(-50%);z-index:160">
      {{ callError }}
    </p>

    <ImageLightbox v-if="preview" :src="preview" @close="preview = ''" />

    <!-- 来电 -->
    <div v-if="call && call.status === 'incoming'" class="call-mask">
      <div class="call-card">
        <span class="avatar big teacher">{{ (call.peer.display_name || '?')[0] }}</span>
        <h3>{{ call.peer.display_name }}</h3>
        <p class="muted">{{ call.video ? '邀请你视频通话…' : '邀请你语音通话…' }}</p>
        <div class="row" style="margin-top:8px">
          <button class="btn danger" @click="declineCall">拒接</button>
          <button class="btn primary" @click="acceptCall">
            <Icon :name="call.video ? 'camcorder' : 'phone'" :size="15" /> 接听
          </button>
        </div>
      </div>
    </div>

    <!-- 通话中 -->
    <div v-if="call && call.status !== 'incoming'" class="call-overlay" @click="attachMedia">
      <template v-if="call.video">
        <video ref="remoteVideoEl" autoplay playsinline class="remote"></video>
        <div v-if="call.status === 'active' && netState === '已连接' && !remoteHasVideo" class="call-hint">
          对方未开启摄像头
        </div>
        <video ref="localVideoEl" autoplay playsinline muted class="local"></video>
        <div v-if="call.status === 'active' && !camOn" class="call-hint local-hint">摄像头已关闭</div>
      </template>
      <div v-else class="audio-call">
        <span class="avatar big teacher">{{ (call.peer.display_name || '?')[0] }}</span>
        <h3>{{ call.peer.display_name }}</h3>
        <p class="muted">{{ call.status === 'outgoing' ? '正在呼叫…' : '语音通话中' }}</p>
        <audio ref="remoteAudioEl" autoplay></audio>
      </div>
      <div class="call-status">
        {{ callStatusText }} · {{ call.peer.display_name }}
      </div>
      <div class="call-controls">
        <button class="ctrl" :class="{ off: !micOn }" :title="micOn ? '静音' : '取消静音'" @click="toggleMic">
          <Icon :name="micOn ? 'mic' : 'mic-off'" :size="20" />
        </button>
        <button v-if="call.video && call.status === 'active'" class="ctrl" :class="{ off: !camOn }" :title="camOn ? '关闭摄像头' : '开启摄像头'" @click="toggleCam">
          <Icon :name="camOn ? 'camera' : 'camera-off'" :size="20" />
        </button>
        <button class="ctrl hangup" title="挂断" @click="hangup"><Icon name="hangup" :size="22" /></button>
      </div>
    </div>
  </div>
</template>
