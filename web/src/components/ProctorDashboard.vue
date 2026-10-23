<script setup>
// ProctorDashboard：老师监考台
// - 汇总本练习所有学员：是否开始 / 是否提交 / 离开次数 / 用时 / IP / 浏览器 / 屏幕
// - 点开某个学员可看完整事件时间线（含每次离开的抓拍图）
// - 支持导出 CSV、手动刷新（默认每 20 秒自动刷新）
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { api } from '../api'
import { downloadCSV } from '../files'
import { fmtDate, fmtDur, fmtTime } from '../format'
import { toastErr } from '../ui'
import Modal from './Modal.vue'
import Icon from './Icon.vue'

const props = defineProps({
  practiceId: { type: [Number, String], required: true },
  title: { type: String, default: '' }
})
const emit = defineEmits(['close'])

const loading = ref(true)
const err = ref('')
const proctor = ref({ mode: 'normal', max_exits: 0 })
const students = ref([])
const selected = ref(null)
const events = ref([])
const eventsLoading = ref(false)
let timer = null

const submittedCount = computed(() => students.value.filter((s) => s.submitted).length)
const violationList = computed(() => students.value.filter((s) => s.proctor_status === 'violation'))
const notSubmitted = computed(() => students.value.filter((s) => !s.submitted))

const STATUS = {
  not_started: { label: '未开始', cls: 'gray' },
  in_progress: { label: '答题中', cls: 'blue' },
  ok: { label: '正常', cls: 'ok' },
  warning: { label: '有过离开', cls: 'warn' },
  violation: { label: '超限 / 违规', cls: 'bad' },
  late: { label: '超时交卷', cls: 'bad' }
}
const EVENT_LABEL = {
  start: '开始答题', leave: '离开页面', return: '返回答题', copy: '复制',
  paste: '粘贴', snapshot: '抓拍', media: '设备授权', submit: '交卷'
}

function statusOf(s) {
  return STATUS[s.proctor_status] || { label: s.proctor_status || '—', cls: 'gray' }
}

function durationOf(s) {
  if (!s.duration_used) return '—'
  return fmtDur(s.duration_used)
}

function detailText(ev) {
  const d = ev.detail || {}
  const bits = []
  if (d.reason) bits.push('原因 ' + d.reason)
  if (d.media) bits.push((d.granted ? '已授权 ' : '未授权 ') + (d.media === 'camera' ? '摄像头' : '屏幕'))
  if (d.late) bits.push('超时交卷')
  if (d.exits !== undefined && d.exits !== null) bits.push('离开 ' + d.exits + ' 次')
  if (d.duration_sec) bits.push('用时 ' + fmtDur(d.duration_sec))
  if (d.extra) bits.push(d.extra)
  return bits.join(' · ')
}

function envText(ev) {
  const d = ev.detail || {}
  const bits = []
  if (d.ip) bits.push('IP ' + d.ip)
  if (d.screen) bits.push('屏幕 ' + d.screen)
  if (d.window) bits.push('窗口 ' + d.window)
  if (d.language) bits.push(d.language)
  if (d.timezone) bits.push(d.timezone)
  if (d.ua) bits.push(d.ua)
  return bits.join(' · ')
}

function openImage(url) {
  if (url) window.open(url, '_blank')
}

async function load() {
  err.value = ''
  try {
    const res = await api(`/api/practices/${props.practiceId}/proctor/overview`)
    proctor.value = res?.proctor || proctor.value
    students.value = res?.students || []
  } catch (e) {
    err.value = e.message
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  await load()
  timer = setInterval(load, 20000)
})
onBeforeUnmount(() => { if (timer) clearInterval(timer) })

async function openStudent(s) {
  selected.value = s
  events.value = []
  eventsLoading.value = true
  try {
    events.value = await api(`/api/practices/${props.practiceId}/proctor/events?student_id=${s.student_id}`)
  } catch (e) {
    toastErr(e)
  } finally {
    eventsLoading.value = false
  }
}

function exportCSV() {
  const head = ['学员', '状态', '退出次数', '答完用时', '得分', '满分', '开始时间', '交卷时间', 'IP', '浏览器', '屏幕', '截图数', '事件数']
  const rows = [head]
  for (const s of students.value) {
    rows.push([
      s.student_name || '学员 #' + s.student_id,
      statusOf(s).label,
      s.exit_count || 0,
      s.duration_used ? fmtDur(s.duration_used) : '',
      s.submitted ? s.score : '',
      s.submitted ? s.total : '',
      s.started_at ? `${fmtDate(s.started_at)} ${fmtTime(s.started_at)}` : '',
      s.submitted_at ? `${fmtDate(s.submitted_at)} ${fmtTime(s.submitted_at)}` : '',
      s.ip || '', s.user_agent || '', s.screen || '',
      s.snapshot_count || 0, s.event_count || 0
    ])
  }
  downloadCSV(`监考记录-${props.practiceId}.csv`, rows)
}
</script>

<template>
  <Modal :title="'监考台 · ' + (title || '')" wide @close="emit('close')">
    <div v-if="loading" class="skeleton" style="height:240px"></div>
    <div v-else-if="err" class="alert error">{{ err }}</div>
    <template v-else>
      <div class="proctor-summary">
        <div class="ps-item"><span class="muted small">严格模式</span><b>{{ proctor.mode === 'strict' ? '已开启' : '未开启' }}</b></div>
        <div class="ps-item"><span class="muted small">允许离开</span><b>{{ proctor.max_exits ?? 0 }} 次</b></div>
        <div class="ps-item"><span class="muted small">已提交</span><b>{{ submittedCount }} / {{ students.length }}</b></div>
        <div class="ps-item"><span class="muted small">未提交</span><b :style="{ color: notSubmitted.length ? 'var(--warn)' : 'inherit' }">{{ notSubmitted.length }}</b></div>
        <div class="ps-item"><span class="muted small">超限/违规</span><b :style="{ color: violationList.length ? 'var(--danger)' : 'inherit' }">{{ violationList.length }}</b></div>
        <div class="row" style="gap:8px;margin-left:auto">
          <button class="btn small" @click="load"><Icon name="refresh" :size="14" /> 刷新</button>
          <button class="btn small" @click="exportCSV"><Icon name="download" :size="14" /> 导出 CSV</button>
        </div>
      </div>

      <div class="table-wrap" style="margin-top:12px">
        <table class="table">
          <thead>
            <tr>
              <th>学员</th><th>状态</th><th>离开</th><th>用时</th><th>提交时间</th>
              <th>IP / 屏幕</th><th>浏览器</th><th>抓拍</th><th style="width:80px">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="s in students" :key="s.student_id" :class="{ 'row-warn': s.proctor_status === 'violation' || s.proctor_status === 'late' }">
              <td>
                <div class="row" style="gap:6px">
                  <b>{{ s.student_name || '学员 #' + s.student_id }}</b>
                  <span v-if="s.graded" class="badge-soft ok">已批改</span>
                </div>
              </td>
              <td><span class="badge-soft" :class="statusOf(s).cls">{{ statusOf(s).label }}</span></td>
              <td>
                <span :style="{ color: s.exit_count > (proctor.max_exits ?? 0) ? 'var(--danger)' : 'inherit' }">
                  {{ s.exit_count || 0 }}<span class="muted small"> / {{ proctor.max_exits ?? 0 }}</span>
                </span>
              </td>
              <td>{{ durationOf(s) }}</td>
              <td class="muted small">{{ s.submitted_at ? fmtDate(s.submitted_at) + ' ' + fmtTime(s.submitted_at) : '—' }}</td>
              <td class="muted small" style="max-width:190px">{{ s.ip || '—' }}<span v-if="s.screen"> · {{ s.screen }}</span></td>
              <td class="muted small" style="max-width:220px">{{ s.user_agent || '—' }}</td>
              <td>{{ s.snapshot_count || 0 }}</td>
              <td>
                <button class="btn small" @click="openStudent(s)">详情</button>
              </td>
            </tr>
            <tr v-if="!students.length"><td colspan="9" class="muted center" style="padding:26px">还没有学员加入课程</td></tr>
          </tbody>
        </table>
      </div>

      <!-- 某个学员的事件时间线 -->
      <div v-if="selected" class="proctor-timeline">
        <div class="divider"></div>
        <div class="row" style="justify-content:space-between;margin-bottom:10px">
          <b>{{ selected.student_name || '学员 #' + selected.student_id }} · 答题记录</b>
          <div class="row" style="gap:8px">
            <span class="badge-soft" :class="statusOf(selected).cls">{{ statusOf(selected).label }}</span>
            <button class="btn small ghost" @click="selected = null">收起</button>
          </div>
        </div>
        <div v-if="eventsLoading" class="skeleton" style="height:120px"></div>
        <div v-else-if="!events.length" class="empty" style="padding:18px 0">还没有监考事件（未开始答题，或不是严格模式）</div>
        <div v-else class="timeline">
          <div v-for="ev in events" :key="ev.id" class="tl-item">
            <span class="tl-dot" :class="ev.kind"></span>
            <div class="tl-main">
              <div class="row" style="gap:8px;flex-wrap:wrap">
                <b class="small">{{ EVENT_LABEL[ev.kind] || ev.kind }}</b>
                <span class="muted small">{{ fmtDate(ev.created_at) }} {{ fmtTime(ev.created_at) }}</span>
                <span v-if="ev.image_url" class="badge-soft">有抓拍</span>
              </div>
              <div v-if="detailText(ev)" class="muted small" style="margin-top:2px">{{ detailText(ev) }}</div>
              <div v-if="envText(ev)" class="muted small" style="margin-top:2px;word-break:break-all">{{ envText(ev) }}</div>
              <div v-if="ev.image_url" class="tl-shot">
                <img :src="ev.image_url" alt="离开时抓拍" loading="lazy" @click="openImage(ev.image_url)" />
              </div>
            </div>
          </div>
        </div>
      </div>
    </template>
  </Modal>
</template>
