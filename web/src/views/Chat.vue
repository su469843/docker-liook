<script setup>
import { ref, onMounted, onBeforeUnmount, nextTick, computed } from 'vue'
import { api } from '../api'
import { store, roleLabel } from '../store'

// ---------------- 状态 ----------------
const contacts = ref([])
const active = ref(null)        // 当前聊天对象
const messages = ref([])
const input = ref('')
const err = ref('')
const online = ref(new Set())
const showEmoji = ref(false)
const fileEl = ref(null)
const listEl = ref(null)
let ws = null
let wsTimer = null

// ---------------- 表情面板（Unicode，零依赖） ----------------
const EMOJI_GROUPS = {
  '😀 常用': '😀 😄 😅 😂 🤣 😊 😇 🙂 😉 😍 🥰 😘 😜 🤪 🤔 🤨 😐 😴 😭 😢 😡 🤯 🥳 😎 🤗 😱 🤢 🥺 😤 🤝'.split(' '),
  '👍 手势': '👍 👎 👌 ✌️ 🤞 🤟 🤘 👏 🙌 🙏 💪 🖐️ ✋ 🤙 👊 ✊ 🫶 👋 🤲 ☝️ 👇 👈 👉 💅 🫡 🫰'.split(' '),
  '❤️ 爱心': '❤️ 🧡 💛 💚 💙 💜 🖤 🤍 🤎 💔 ❣️ 💕 💞 💓 💗 💖 💘 💝 💟 ✨ 🌟 ⭐ 🔥 💥 🎉 🎊'.split(' '),
  '🐶 动物': '🐶 🐱 🐭 🐹 🐰 🦊 🐻 🐼 🐨 🐯 🦁 🐮 🐷 🐸 🐵 🐔 🐧 🐦 🦆 🦄 🐝 🦋 🐢 🐟 🐳 🦈'.split(' '),
  '🍔 食物': '🍏 🍎 🍐 🍊 🍋 🍌 🍉 🍇 🍓 🫐 🍒 🍑 🥭 🍍 🥥 🥝 🍅 🥑 🍆 🥕 🌽 🌶️ 🥟 🍜 🍕 🍔'.split(' '),
  '⚽ 活动': '⚽ 🏀 🏈 ⚾ 🎾 🏐 🏓 🏸 🥊 ⛳ 🎯 🎮 🎲 🎸 🎹 🎤 🎧 🏆 🥇 🎖️ 🎫 🎬 📷 🎨 🚴 🏊'.split(' '),
  '🚗 旅行': '🚗 🚕 🚙 🚌 🚎 🏎️ 🚓 🚑 🚒 🚚 🚜 🛵 🚲 ✈️ 🚀 🛸 🚁 ⛵ 🚢 🏝️ 🗻 🎡 🎢 🗺️ 🧭 🏕️'.split(' '),
  '💡 物品': '💡 🔦 📱 💻 ⌨️ 🖥️ 🖱️ 💾 📀 📷 📹 📞 📺 📻 ⏰ ⌛ 📚 📖 ✏️ 📌 📎 🔑 🔒 🎁 🛒 💰'.split(' ')
}
const emojiTabs = Object.keys(EMOJI_GROUPS)
const emojiTab = ref(emojiTabs[0])

// ---------------- WebSocket ----------------
function wsURL() {
  const p = location.protocol === 'https:' ? 'wss' : 'ws'
  return `${p}://${location.host}/ws`
}

function connect() {
  ws = new WebSocket(wsURL())
  ws.onopen = () => { err.value = '' }
  ws.onmessage = (ev) => {
    let m
    try { m = JSON.parse(ev.data) } catch { return }
    handleServerMsg(m)
  }
  ws.onclose = () => {
    wsTimer = setTimeout(connect, 3000) // 断线重连
  }
}

function send(obj) {
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify(obj))
    return true
  }
  return false
}

function handleServerMsg(m) {
  switch (m.type) {
    case 'presence':
      online.value = new Set(m.ids || [])
      contacts.value.forEach((c) => { c.online = online.value.has(c.id) })
      break
    case 'chat.sent': // 自己发的消息回执
      if (active.value && m.msg && (m.msg.sender_id === active.value.id || m.msg.receiver_id === active.value.id)) {
        messages.value.push(m.msg)
        scrollBottom()
      }
      break
    case 'chat.new': // 收到新消息
      if (active.value && m.from && m.from.id === active.value.id) {
        messages.value.push(m.msg)
        scrollBottom()
        send({ type: 'chat.read', to: m.from.id })
      } else if (m.from) {
        const c = contacts.value.find((x) => x.id === m.from.id)
        if (c) c.unread = (c.unread || 0) + 1
      }
      break
    case 'call.offer':
      onIncomingCall(m)
      break
    case 'call.answer':
      onCallAnswer(m)
      break
    case 'call.ice':
      onCallIce(m)
      break
    case 'call.end':
      onCallEnd(m)
      break
    case 'call.busy':
      teardownCall()
      err.value = m.message || '对方忙'
      break
    case 'error':
      err.value = m.message || '操作失败'
      break
  }
}

// ---------------- 联系人/历史 ----------------
async function loadContacts() {
  contacts.value = await api('/api/contacts')
}

async function openChat(c) {
  active.value = c
  c.unread = 0
  showEmoji.value = false
  messages.value = await api(`/api/chat/${c.id}`)
  nextTick(scrollBottom)
}

function scrollBottom() {
  if (listEl.value) listEl.value.scrollTop = listEl.value.scrollHeight
}

function sendText() {
  const content = input.value.trim()
  if (!content || !active.value) return
  if (!send({ type: 'chat.send', to: active.value.id, msg: { type: 'text', content } })) {
    err.value = '连接已断开，正在重连…'
    return
  }
  input.value = ''
}

function pickEmoji(e) {
  input.value += e
  showEmoji.value = false
}

async function pickImage(ev) {
  const f = ev.target.files[0]
  if (!f || !active.value) return
  const fd = new FormData()
  fd.append('file', f)
  try {
    const res = await api('/api/chat/upload', { method: 'POST', body: fd })
    send({ type: 'chat.send', to: active.value.id, msg: { type: 'image', content: res.url } })
  } catch (e) {
    err.value = e.message
  } finally {
    ev.target.value = ''
  }
}

function fmtTime(ts) {
  if (!ts) return ''
  return ts.slice(11, 16) // UTC HH:MM
}

function openImg(url) {
  window.open(url, '_blank')
}

// ---------------- 视频通话（WebRTC 1v1） ----------------
const call = ref(null)
// call = { peer, role: 'caller'|'callee', video: bool, status: 'outgoing'|'incoming'|'active' }
const localVideoEl = ref(null)
const remoteVideoEl = ref(null)
const remoteAudioEl = ref(null)
const micOn = ref(true)
const camOn = ref(true)
let pc = null
let localStream = null
let pendingOffer = null
let audioCtx = null

async function getICE() {
  try {
    const cfg = await api('/api/ice')
    return cfg.iceServers?.length ? cfg.iceServers : [{ urls: 'stun:stun.l.google.com:19302' }]
  } catch {
    return [{ urls: 'stun:stun.l.google.com:19302' }]
  }
}

// 缓存的 ICE 配置与「早到」的远端候选
// 对端可能在 setRemoteDescription 之前就开始发候选，先排队，设置远端描述后统一补上
let iceCache = null
let pendingRemoteICE = []

function beepStart() {
  try {
    audioCtx = audioCtx || new AudioContext()
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

function newPC(peerId) {
  pc = new RTCPeerConnection({
    iceServers: iceCache || [{ urls: 'stun:stun.l.google.com:19302' }]
  })
  pc.onicecandidate = (ev) => {
    if (ev.candidate) send({ type: 'call.ice', to: peerId, candidate: JSON.stringify(ev.candidate) })
  }
  pc.ontrack = (ev) => {
    // 视频通话用 <video>，语音通话用 <audio>（纯音频时没有视频元素）
    if (remoteVideoEl.value) remoteVideoEl.value.srcObject = ev.streams[0]
    if (remoteAudioEl.value) remoteAudioEl.value.srcObject = ev.streams[0]
  }
  pc.onconnectionstatechange = () => {
    if (['failed', 'closed'].includes(pc.connectionState)) teardownCall()
  }
  return pc
}

// 应用排队中的远端候选（在 setRemoteDescription 之后调用）
async function flushRemoteICE() {
  const list = pendingRemoteICE
  pendingRemoteICE = []
  for (const cand of list) {
    try { await pc?.addIceCandidate(cand) } catch {}
  }
}

async function startCall(video) {
  if (!active.value) return
  err.value = ''
  if (!iceCache) iceCache = await getICE()
  call.value = { peer: active.value, role: 'caller', video, status: 'outgoing' }
  try {
    localStream = await navigator.mediaDevices.getUserMedia({ video: !!video, audio: true })
    if (localVideoEl.value) localVideoEl.value.srcObject = localStream
    newPC(active.value.id)
    localStream.getTracks().forEach((t) => pc.addTrack(t, localStream))
    const offer = await pc.createOffer()
    await pc.setLocalDescription(offer)
    send({ type: 'call.offer', to: active.value.id, sdp: JSON.stringify(offer), video: !!video })
  } catch (e) {
    err.value = '无法访问摄像头/麦克风: ' + (e.message || e)
    teardownCall()
  }
}

function onIncomingCall(m) {
  if (call.value) {
    send({ type: 'call.end', to: m.from.id })
    return
  }
  call.value = { peer: m.from, role: 'callee', video: !!m.video, status: 'incoming' }
  pendingOffer = m.sdp
  beepStart()
}

async function acceptCall() {
  if (!call.value || !pendingOffer) return
  beepStop()
  const wantVideo = !!call.value.video
  try {
    localStream = await navigator.mediaDevices.getUserMedia({ video: wantVideo, audio: true })
    if (localVideoEl.value) localVideoEl.value.srcObject = localStream
    newPC(call.value.peer.id)
    localStream.getTracks().forEach((t) => pc.addTrack(t, localStream))
    await pc.setRemoteDescription(JSON.parse(pendingOffer))
    await flushRemoteICE()
    const answer = await pc.createAnswer()
    await pc.setLocalDescription(answer)
    send({ type: 'call.answer', to: call.value.peer.id, sdp: JSON.stringify(answer), video: wantVideo })
    call.value.status = 'active'
    pendingOffer = null
  } catch (e) {
    err.value = '无法访问摄像头/麦克风: ' + (e.message || e)
    declineCall()
  }
}

function declineCall() {
  if (call.value) send({ type: 'call.end', to: call.value.peer.id })
  teardownCall()
}

function onCallAnswer(m) {
  if (!pc || !call.value) return
  pc.setRemoteDescription(JSON.parse(m.sdp))
    .then(async () => {
      await flushRemoteICE()
      if (call.value) call.value.status = 'active'
    })
    .catch(() => {})
}

function onCallIce(m) {
  let cand = null
  try { cand = JSON.parse(m.candidate) } catch { return }
  if (!cand) return
  if (pc && pc.remoteDescription) pc.addIceCandidate(cand).catch(() => {})
  else pendingRemoteICE.push(cand) // 远端描述尚未设置，先排队
}

function onCallEnd(m) {
  if (call.value && call.value.peer.id === m.from_id) {
    teardownCall()
    err.value = '通话已结束'
  }
}

function hangup() {
  if (call.value) send({ type: 'call.end', to: call.value.peer.id })
  teardownCall()
}

function teardownCall() {
  beepStop()
  pendingOffer = null
  pendingRemoteICE = []
  micOn.value = true
  camOn.value = true
  if (localStream) {
    localStream.getTracks().forEach((t) => t.stop())
    localStream = null
  }
  if (pc) { try { pc.close() } catch {} pc = null }
  call.value = null
}

function toggleMic() {
  micOn.value = !micOn.value
  localStream?.getAudioTracks().forEach((t) => (t.enabled = micOn.value))
}

function toggleCam() {
  camOn.value = !camOn.value
  localStream?.getVideoTracks().forEach((t) => (t.enabled = camOn.value))
}

// ---------------- 生命周期 ----------------
onMounted(async () => {
  await loadContacts()
  connect()
})

onBeforeUnmount(() => {
  clearTimeout(wsTimer)
  teardownCall()
  if (ws) { ws.onclose = null; ws.close() }
})

const roleOf = (u) => roleLabel(u?.role)
const iAmStudent = computed(() => store.user?.role === 'student')
</script>

<template>
  <div class="page chat-page">
    <div class="chat-layout">
      <!-- 联系人 -->
      <aside class="card contact-list">
        <h4>💬 联系人</h4>
        <p v-if="!contacts.length" class="muted small-note">
          {{ iAmStudent ? '加入课程后即可与老师私聊' : '学员加入你的课程后可私聊' }}
        </p>
        <div v-for="c in contacts" :key="c.id" class="contact" :class="{ active: active && active.id === c.id }"
          @click="openChat(c)">
          <span class="avatar">{{ (c.display_name || '?')[0] }}</span>
          <span class="contact-info">
            <strong>{{ c.display_name }}</strong>
            <small class="muted">{{ roleOf(c) }}{{ online.has(c.id) ? ' · 🟢在线' : '' }}</small>
          </span>
          <span v-if="c.unread" class="unread">{{ c.unread > 99 ? '99+' : c.unread }}</span>
        </div>
      </aside>

      <!-- 聊天窗 -->
      <section class="card chat-window" v-if="active">
        <header class="chat-head">
          <div>
            <strong>{{ active.display_name }}</strong>
            <small class="muted"> {{ roleOf(active) }}{{ online.has(active.id) ? ' · 🟢在线' : ' · 离线' }}</small>
          </div>
          <div class="row">
            <button class="btn small" @click="startCall(false)" :disabled="!!call">📞 语音</button>
            <button class="btn small primary" @click="startCall(true)" :disabled="!!call">🎥 视频</button>
          </div>
        </header>

        <div class="msg-list" ref="listEl">
          <p v-if="!messages.length" class="muted center">打个招呼吧 👋</p>
          <div v-for="m in messages" :key="m.id" class="msg-row"
            :class="{ mine: m.sender_id === store.user?.id }">
            <div class="bubble" :class="{ img: m.type === 'image' }">
              <img v-if="m.type === 'image'" :src="m.content" @click="openImg(m.content)" />
              <template v-else>{{ m.content }}</template>
              <small class="msg-time">{{ fmtTime(m.created_at) }}</small>
            </div>
          </div>
        </div>

        <!-- 表情面板 -->
        <div v-if="showEmoji" class="emoji-panel">
          <div class="emoji-tabs">
            <button v-for="t in emojiTabs" :key="t" type="button"
              :class="{ active: emojiTab === t }" @click="emojiTab = t">{{ t.split(' ')[0] }}</button>
          </div>
          <div class="emoji-grid">
            <button v-for="e in EMOJI_GROUPS[emojiTab]" :key="e" type="button" @click="pickEmoji(e)">{{ e }}</button>
          </div>
        </div>

        <footer class="chat-input">
          <button class="icon-btn" @click="showEmoji = !showEmoji" title="表情">😊</button>
          <button class="icon-btn" @click="fileEl?.click()" title="发送图片">🖼️</button>
          <input ref="fileEl" type="file" accept="image/png,image/jpeg,image/gif,image/webp" hidden @change="pickImage" />
          <input v-model="input" placeholder="输入消息，Enter 发送…" @keydown.enter.prevent="sendText" />
          <button class="btn primary small" @click="sendText">发送</button>
        </footer>
      </section>
      <section class="card chat-window empty" v-else>
        <p class="muted center">← 从左侧选择联系人开始聊天<br /><br />
          聊天支持文字、表情、图片，还可以发起 1 对 1 音视频通话</p>
      </section>
    </div>

    <p v-if="err" class="error">{{ err }}</p>

    <!-- 来电弹窗 -->
    <div v-if="call && call.status === 'incoming'" class="call-modal">
      <div class="card call-card">
        <div class="avatar big">{{ (call.peer.display_name || '?')[0] }}</div>
        <h3>{{ call.peer.display_name }}</h3>
        <p class="muted">{{ call.video ? '邀请你视频通话…' : '邀请你语音通话…' }}</p>
        <div class="row call-actions">
          <button class="btn danger" @click="declineCall">拒接</button>
          <button class="btn primary" @click="acceptCall">接听 {{ call.video ? '🎥' : '📞' }}</button>
        </div>
      </div>
    </div>

    <!-- 通话界面 -->
    <div v-if="call && call.status !== 'incoming'" class="call-overlay">
      <template v-if="call.video">
        <video ref="remoteVideoEl" autoplay playsinline class="remote-video"></video>
        <video ref="localVideoEl" autoplay playsinline muted class="local-video"></video>
      </template>
      <div v-else class="audio-call">
        <div class="avatar big">{{ (call.peer.display_name || '?')[0] }}</div>
        <h3>{{ call.peer.display_name }}</h3>
        <p class="muted">语音通话</p>
        <audio ref="remoteAudioEl" autoplay></audio>
      </div>
      <div class="call-status">
        {{ call.status === 'outgoing' ? '等待对方接听…' : (call.status === 'active' ? '通话中' : '连接中…') }}
        · {{ call.peer.display_name }}
      </div>
      <div class="call-controls">
        <button class="ctrl" :class="{ off: !micOn }" @click="toggleMic">{{ micOn ? '🎙️' : '🔇' }}</button>
        <button v-if="call.video && call.status === 'active'" class="ctrl" :class="{ off: !camOn }" @click="toggleCam">{{ camOn ? '📷' : '🚫' }}</button>
        <button class="ctrl hangup" @click="hangup">📴</button>
      </div>
    </div>
  </div>
</template>
