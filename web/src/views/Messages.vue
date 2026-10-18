<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { api, postForm } from '../api'
import { onWS, sendWS, wsState } from '../ws'
import { fmtDur, fmtRel, fmtTime } from '../format'
import { refreshUnread, store } from '../store'
import { toastErr } from '../ui'
import { STICKERS, plainText, stickerToken } from '../stickers'
import GroupChat from '../components/GroupChat.vue'
import Icon from '../components/Icon.vue'
import Sticker from '../components/Sticker.vue'
import MessageBody from '../components/MessageBody.vue'

// ---------------- 状态 ----------------
const tab = ref('private')
const contacts = ref([])
const groups = ref([])
const activeContact = ref(null)
const activeGroup = ref(null)
const messages = ref([])
const input = ref('')
const showStickers = ref(false)
const listEl = ref(null)
const fileEl = ref(null)
const loading = ref(true)
let off = null

const hasActive = computed(() => !!(activeContact.value || activeGroup.value))
const myID = computed(() => store.user?.id)
const online = computed(() => new Set(store.online))

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
  if (contacts.value.length) openPrivate(contacts.value[0])
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
    messages.value = (await api(`/api/chat/${c.id}`)) || []
    sendWS({ type: 'chat.read', to: c.id })
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
  refreshUnread()
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
  const f = ev.target.files[0]
  ev.target.value = ''
  if (!f || !activeContact.value) return
  try {
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
      callError.value = m.message || '对方忙'
      break
    case 'error':
      callError.value = m.message || '操作失败'
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
let pc = null
let localStream = null
let pendingOffer = null
let audioCtx = null
let iceCache = null
let pendingRemoteICE = []

async function getICE() {
  try {
    const cfg = await api('/api/ice')
    return cfg.iceServers?.length ? cfg.iceServers : [{ urls: 'stun:stun.l.google.com:19302' }]
  } catch {
    return [{ urls: 'stun:stun.l.google.com:19302' }]
  }
}

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
  pc = new RTCPeerConnection({ iceServers: iceCache || [{ urls: 'stun:stun.l.google.com:19302' }] })
  pc.onicecandidate = (ev) => {
    if (ev.candidate) sendWS({ type: 'call.ice', to: peerId, candidate: JSON.stringify(ev.candidate) })
  }
  pc.ontrack = (ev) => {
    if (remoteVideoEl.value) remoteVideoEl.value.srcObject = ev.streams[0]
    if (remoteAudioEl.value) remoteAudioEl.value.srcObject = ev.streams[0]
  }
  pc.onconnectionstatechange = () => {
    if (['failed', 'closed'].includes(pc?.connectionState)) teardownCall()
  }
  return pc
}

async function flushRemoteICE() {
  const list = pendingRemoteICE
  pendingRemoteICE = []
  for (const cand of list) {
    try { await pc?.addIceCandidate(cand) } catch {}
  }
}

async function startCall(video) {
  if (!activeContact.value) return
  callError.value = ''
  if (!iceCache) iceCache = await getICE()
  call.value = { peer: activeContact.value, role: 'caller', video, status: 'outgoing' }
  try {
    localStream = await navigator.mediaDevices.getUserMedia({ video: !!video, audio: true })
    if (localVideoEl.value) localVideoEl.value.srcObject = localStream
    newPC(activeContact.value.id)
    localStream.getTracks().forEach((t) => pc.addTrack(t, localStream))
    const offer = await pc.createOffer()
    await pc.setLocalDescription(offer)
    sendWS({ type: 'call.offer', to: activeContact.value.id, sdp: JSON.stringify(offer), video: !!video })
  } catch (e) {
    callError.value = '无法访问摄像头/麦克风: ' + (e.message || e)
    teardownCall()
  }
}

function onIncomingCall(m) {
  if (call.value) { sendWS({ type: 'call.end', to: m.from.id }); return }
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
    sendWS({ type: 'call.answer', to: call.value.peer.id, sdp: JSON.stringify(answer), video: wantVideo })
    call.value.status = 'active'
    pendingOffer = null
  } catch (e) {
    callError.value = '无法访问摄像头/麦克风: ' + (e.message || e)
    declineCall()
  }
}

function declineCall() {
  if (call.value) sendWS({ type: 'call.end', to: call.value.peer.id })
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
  else pendingRemoteICE.push(cand)
}

function onCallEnd(m) {
  if (call.value && call.value.peer.id === m.from_id) {
    teardownCall()
    callError.value = '通话已结束'
  }
}

function hangup() {
  if (call.value) sendWS({ type: 'call.end', to: call.value.peer.id })
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
        <div class="im-list-tabs">
          <button :class="{ active: tab === 'private' }" @click="tab = 'private'">私聊</button>
          <button :class="{ active: tab === 'group' }" @click="tab = 'group'">课程群</button>
        </div>
        <div class="im-scroll">
          <template v-if="tab === 'private'">
            <div v-if="loading" class="muted center" style="padding:24px">加载中…</div>
            <div v-else-if="!contacts.length" class="group-note" style="margin:14px">
              加入课程后，这里会出现老师；老师这里会出现自己的学员。
            </div>
            <div v-for="c in contacts" :key="c.id" class="im-item"
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
            <div v-if="!groups.length" class="group-note" style="margin:14px">
              还没有课程群。老师创建课程、学员加入后，这里会出现课程群聊。
            </div>
            <div v-for="g in groups" :key="g.course_id" class="im-item"
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
                  <img v-if="m.type === 'image'" :src="m.content" alt="图片" />
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
    <div v-if="call && call.status !== 'incoming'" class="call-overlay">
      <template v-if="call.video">
        <video ref="remoteVideoEl" autoplay playsinline class="remote"></video>
        <video ref="localVideoEl" autoplay playsinline muted class="local"></video>
      </template>
      <div v-else class="audio-call">
        <span class="avatar big teacher">{{ (call.peer.display_name || '?')[0] }}</span>
        <h3>{{ call.peer.display_name }}</h3>
        <p class="muted">语音通话中</p>
        <audio ref="remoteAudioEl" autoplay></audio>
      </div>
      <div class="call-status">
        {{ call.status === 'outgoing' ? '等待对方接听…' : '通话中' }} · {{ call.peer.display_name }}
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
