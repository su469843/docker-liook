<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { api, postForm } from '../api'
import { onWS, sendWS, wsState } from '../ws'
import { fmtRel, fmtTime, fmtDur } from '../format'
import { markGroupRead, store } from '../store'
import { toastErr } from '../ui'
import { STICKERS, stickerToken } from '../stickers'
import Icon from './Icon.vue'
import Sticker from './Sticker.vue'
import MessageBody from './MessageBody.vue'
import ImageLightbox from './ImageLightbox.vue'

const props = defineProps({
  courseId: { type: Number, required: true },
  course: { type: Object, default: null },
  canQuote: { type: Boolean, default: false },
  lesson: { type: Object, default: null },
  time: { type: Number, default: 0 }
})
const emit = defineEmits(['seek'])

const messages = ref([])
const input = ref('')
const quote = ref(null)
const listEl = ref(null)
const loading = ref(true)
const showStickers = ref(false)
const imgEl = ref(null)
const preview = ref('')
const sending = ref(false)
let off = null

function insertSticker(id) {
  input.value += stickerToken(id)
}

const myID = computed(() => store.user?.id)
const online = computed(() => store.online.includes(props.course?.teacher_id))

async function load() {
  loading.value = true
  try {
    messages.value = (await api(`/api/groups/${props.courseId}/messages`)) || []
  } catch (e) {
    toastErr(e)
  }
  loading.value = false
  scrollBottom()
  markGroupRead(props.courseId) // 打开即已读，红点/角标由后端落库后刷新
}

function scrollBottom() {
  nextTick(() => {
    if (listEl.value) listEl.value.scrollTop = listEl.value.scrollHeight
  })
}

function addMessage(g) {
  if (!g || messages.value.some((x) => x.id === g.id)) return
  messages.value.push(g)
  scrollBottom()
}

function onMsg(m) {
  if (m.group !== props.courseId) return
  if (m.type === 'group.sent' && m.gmsg) addMessage(m.gmsg)
  if (m.type === 'group.new' && m.gmsg) {
    addMessage(m.gmsg)
    markGroupRead(props.courseId) // 正看着这个群，直接标记已读，别在顶部留红点
  }
}

function send() {
  const content = input.value.trim()
  if (!content) return
  const payload = { type: 'group.send', group: props.courseId, msg: { type: 'text', content } }
  if (quote.value) payload.ref = { lesson_id: quote.value.lesson_id, time: quote.value.time }
  if (!sendWS(payload)) {
    toastErr('连接已断开，正在重连…')
    return
  }
  input.value = ''
  quote.value = null
  showStickers.value = false
}

function setQuote() {
  if (!props.lesson) {
    toastErr('先打开一个视频课时，才能引用进度')
    return
  }
  quote.value = { lesson_id: props.lesson.id, time: props.time || 0, title: props.lesson.title }
}

async function sendImage(ev) {
  const f = ev.target.files[0]
  ev.target.value = ''
  if (!f) return
  sending.value = true
  try {
    const fd = new FormData()
    fd.append('file', f)
    const res = await postForm('/api/upload/image', fd)
    const payload = { type: 'group.send', group: props.courseId, msg: { type: 'image', content: res.url } }
    if (!sendWS(payload)) toastErr('连接已断开，正在重连…')
  } catch (e) {
    toastErr(e)
  } finally {
    sending.value = false
  }
}

function jump(m) {
  if (m.ref_lesson_id) emit('seek', { lesson_id: m.ref_lesson_id, time: m.ref_time || 0 })
}

function onKeydown(e) {
  if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
    e.preventDefault()
    send()
  }
}

onMounted(() => {
  load()
  off = onWS(onMsg)
})
onBeforeUnmount(() => { if (off) off() })
watch(() => props.courseId, () => { load() })
</script>

<template>
  <div style="display:flex;flex-direction:column;height:100%;min-height:0">
    <div class="msg-list" ref="listEl">
      <div v-if="loading" class="muted center" style="padding:20px">加载中…</div>
      <div v-else-if="!messages.length" class="group-note">
        <Icon name="megaphone" :size="18" />
        这里是课程群聊，可以和老师、同学一起讨论课程内容。播放视频时可以点「引用当前进度」把视频时间点发出来。
      </div>
      <div v-for="m in messages" :key="m.id" class="msg-row" :class="{ mine: m.sender_id === myID }">
        <span v-if="m.sender_id !== myID" class="avatar sm teacher">{{ (m.sender_name || '?')[0] }}</span>
        <div class="msg-stack">
          <span v-if="m.sender_id !== myID" class="msg-sender">{{ m.sender_name }}</span>
          <div class="bubble">
            <img v-if="m.type === 'image'" :src="m.content" alt="图片" @click="preview = m.content" />
            <MessageBody v-else :content="m.content" />
            <div v-if="m.ref_lesson_id" class="quote-chip" style="margin-top:6px" @click="jump(m)">
              <Icon name="play" :size="13" /><span class="qt">{{ m.ref_title }}<template v-if="m.ref_time"> · {{ fmtDur(m.ref_time) }}</template></span>
            </div>
          </div>
          <span class="msg-time">{{ fmtTime(m.created_at) }}</span>
        </div>
      </div>
    </div>

    <div v-if="quote" class="quote-bar" style="margin:0 12px 8px">
      <span>引用：{{ quote.title }}<template v-if="quote.time"> · {{ fmtDur(quote.time) }}</template></span>
      <span class="spacer" style="flex:1"></span>
      <button class="btn-link" @click="quote = null">取消</button>
    </div>

    <div class="composer">
      <div class="composer-tools">
        <button class="btn-link" type="button" @click="showStickers = !showStickers"><Icon name="sticker" :size="16" /> 表情</button>
        <button class="btn-link" type="button" @click="imgEl.click()"><Icon name="image" :size="16" /> 图片</button>
        <button v-if="canQuote" class="btn-link" type="button" @click="setQuote"><Icon name="quote" :size="16" /> 引用当前进度</button>
        <span class="spacer" style="flex:1"></span>
        <span class="muted small">{{ wsState.connected ? '已连接' : '连接中…' }}</span>
      </div>
      <div v-if="showStickers" class="sticker-panel">
        <div class="sticker-grid">
          <button v-for="s in STICKERS" :key="s.id" type="button" :title="s.label" @click="insertSticker(s.id)">
            <Sticker :id="s.id" :size="34" />
          </button>
        </div>
      </div>
      <div class="composer-input">
        <textarea v-model="input" rows="1" placeholder="说点什么…（Enter 发送，Shift+Enter 换行）" @keydown="onKeydown"></textarea>
        <button class="btn primary" type="button" :disabled="sending" @click="send"><Icon name="send" :size="16" /> 发送</button>
      </div>
      <input ref="imgEl" type="file" accept="image/png,image/jpeg,image/gif,image/webp" hidden @change="sendImage" />
    </div>

    <ImageLightbox v-if="preview" :src="preview" @close="preview = ''" />
  </div>
</template>
