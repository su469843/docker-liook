<script setup>
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api'
import { store } from '../store'
import { fmtDate, practiceKindIcon, practiceKindLabel, subjectLabel, monthNow, PRACTICE_SUBJECTS, PRACTICE_KINDS, CHECKIN_KINDS } from '../format'
import { toastOk, toastErr } from '../ui'
import Icon from '../components/Icon.vue'
import ProctorDashboard from '../components/ProctorDashboard.vue'

const router = useRouter()

const loading = ref(true)
const err = ref('')
const practices = ref([])
const summary = ref(null)
const week = ref([])

const isStaff = computed(() => store.user?.role === 'teacher' || store.user?.role === 'admin')

const tab = ref('all') // 学员：all | todo | done
const kindFilter = ref('')

const checkinForm = ref({ subject: 'english', kind: 'word', count: 20, minutes: 15, content: '' })
const checkinSaving = ref(false)

const filtered = computed(() => {
  let list = practices.value || []
  if (kindFilter.value) list = list.filter((p) => p.kind === kindFilter.value)
  if (tab.value === 'todo') list = list.filter((p) => !p.submission)
  if (tab.value === 'done') list = list.filter((p) => !!p.submission)
  return list
})

const todoCount = computed(() => (practices.value || []).filter((p) => !p.submission).length)

// 老师视角：按课程分组，显示提交 / 待批改情况
const groups = computed(() => {
  const map = new Map()
  for (const p of filtered.value) {
    const key = p.course_id || 0
    if (!map.has(key)) map.set(key, { courseId: key, title: p.course_title || '未命名课程', list: [], pending: 0, submitted: 0 })
    const g = map.get(key)
    g.list.push(p)
    g.pending += p.pending_count || 0
    g.submitted += p.submitted_count || 0
  }
  return [...map.values()]
})
const staffTotalPending = computed(() => (practices.value || []).reduce((n, p) => n + (p.pending_count || 0), 0))
const staffTotalSubmitted = computed(() => (practices.value || []).reduce((n, p) => n + (p.submitted_count || 0), 0))

function fmtScore(n) {
  const v = Number(n || 0)
  return v % 1 === 0 ? String(v) : v.toFixed(1)
}

function statusOf(p) {
  if (p.submission) {
    return p.submission.graded ? `得分 ${fmtScore(p.submission.score)} / ${fmtScore(p.submission.total)}` : '已交卷 · 待批改'
  }
  if (p.deadline && new Date(p.deadline).getTime() < Date.now()) return '已截止'
  return '未提交'
}

function statusClass(p) {
  if (!p.submission) return p.deadline && new Date(p.deadline).getTime() < Date.now() ? 'expired' : 'todo'
  return p.submission.graded ? 'done' : 'pending'
}

function buildWeek(days) {
  const map = {}
  for (const d of days || []) map[d.day] = (map[d.day] || 0) + (d.count || 0)
  const out = []
  const now = new Date()
  const names = ['日', '一', '二', '三', '四', '五', '六']
  for (let i = 6; i >= 0; i--) {
    const d = new Date(now.getFullYear(), now.getMonth(), now.getDate() - i)
    const key = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
    out.push({ key, label: names[d.getDay()], done: (map[key] || 0) > 0, today: i === 0 })
  }
  return out
}

async function loadSummary() {
  const cur = monthNow()
  let days = []
  try {
    const curSum = await api(`/api/checkins?month=${cur}`)
    summary.value = curSum
    days = [...(curSum.days || [])]
    if (new Date().getDate() <= 7) {
      const prev = new Date()
      prev.setDate(1)
      prev.setMonth(prev.getMonth() - 1)
      const pm = `${prev.getFullYear()}-${String(prev.getMonth() + 1).padStart(2, '0')}`
      const prevSum = await api(`/api/checkins?month=${pm}`)
      days = [...(prevSum.days || []), ...days]
    }
  } catch (e) {
    err.value = e.message
  }
  week.value = buildWeek(days)
}

async function load() {
  loading.value = true
  err.value = ''
  try {
    const list = await api('/api/practices')
    practices.value = Array.isArray(list) ? list : []
    // 老师 / 管理员看管理视角，不需要打卡与「待完成」
    if (!isStaff.value) await loadSummary()
  } catch (e) {
    err.value = e.message
  } finally {
    loading.value = false
  }
}
onMounted(load)

async function saveCheckin() {
  if (checkinSaving.value) return
  checkinSaving.value = true
  try {
    summary.value = await api('/api/checkins', { method: 'POST', json: { ...checkinForm.value } })
    await loadSummary()
    toastOk('打卡成功，继续保持！')
  } catch (e) {
    toastErr(e)
  } finally {
    checkinSaving.value = false
  }
}

function open(p) {
  router.push(`/practice/${p.id}`)
}

function openStudio(courseId) {
  router.push(`/studio/${courseId}/practice`)
}

// 老师：监考台（离开次数 / 截图 / 浏览器与屏幕信息）
const showProctor = ref(false)
const proctorPractice = ref(null)
function openProctor(p) {
  proctorPractice.value = p
  showProctor.value = true
}
</script>

<template>
  <div class="container">
    <div class="page-head">
      <div>
        <h1 class="page-title">练习中心</h1>
        <div class="page-sub" v-if="isStaff">老师视角：查看各课程作业的提交 / 待批改，点「批改」进入圈画批注</div>
        <div class="page-sub" v-else>作业 / 考卷 / 单词 / 句子练习，客观题交卷即出分，单词句子每天打卡</div>
      </div>
      <RouterLink v-if="!isStaff" to="/my" class="btn small"><Icon name="book" :size="15" /> 我的课程</RouterLink>
      <RouterLink v-else :to="store.user?.role === 'admin' ? '/admin' : '/studio'" class="btn small">
        <Icon name="arrow-left" :size="15" /> 返回后台
      </RouterLink>
    </div>

    <div v-if="loading" class="skeleton" style="height:240px"></div>

    <template v-else>
      <!-- 学员：单词打卡 -->
      <div v-if="!isStaff" class="card checkin-card">
        <div class="row" style="justify-content:space-between;align-items:flex-start">
          <div class="row" style="gap:12px;align-items:flex-start">
            <span class="checkin-fire"><Icon name="fire" :size="26" /></span>
            <div>
              <div style="font-size:17px;font-weight:600">单词 / 句子打卡</div>
              <div class="muted small" style="margin-top:2px">
                已连续打卡 <b style="color:var(--brand-700)">{{ summary?.streak || 0 }}</b> 天 ·
                本月 {{ summary?.month_days || 0 }} 天 / {{ summary?.month_count || 0 }} 个词句 ·
                累计 {{ summary?.total_days || 0 }} 天
              </div>
            </div>
          </div>
          <div class="checkin-strip">
            <div v-for="d in week" :key="d.key" class="checkin-day" :class="{ done: d.done, today: d.today }">
              <div class="cd-label">{{ d.today ? '今天' : d.label }}</div>
              <div class="cd-dot">
                <Icon v-if="d.done" name="check" :size="13" />
                <span v-else>·</span>
              </div>
            </div>
          </div>
        </div>
        <div class="divider"></div>
        <div class="row" style="align-items:flex-end">
          <div class="field" style="margin:0;width:110px">
            <label>科目</label>
            <select v-model="checkinForm.subject" class="select">
              <option v-for="s in PRACTICE_SUBJECTS" :key="s.id" :value="s.id">{{ s.name }}</option>
            </select>
          </div>
          <div class="field" style="margin:0;width:110px">
            <label>类型</label>
            <select v-model="checkinForm.kind" class="select">
              <option v-for="k in CHECKIN_KINDS" :key="k.id" :value="k.id">{{ k.name }}</option>
            </select>
          </div>
          <div class="field" style="margin:0;width:110px">
            <label>数量（个）</label>
            <input v-model.number="checkinForm.count" class="input" type="number" min="1" max="2000" />
          </div>
          <div class="field" style="margin:0;width:110px">
            <label>时长（分钟）</label>
            <input v-model.number="checkinForm.minutes" class="input" type="number" min="0" max="1440" />
          </div>
          <div class="field" style="margin:0;flex:1;min-width:180px">
            <label>今天学了什么（选填）</label>
            <input v-model="checkinForm.content" class="input" placeholder="例如：Unit 1 单词 apple / banana" maxlength="200" />
          </div>
          <button class="btn primary" :disabled="checkinSaving" @click="saveCheckin">
            <Icon name="check" :size="16" /> {{ checkinSaving ? '保存中…' : (summary?.today_done ? '更新今日打卡' : '立即打卡') }}
          </button>
        </div>
      </div>

      <!-- 老师：汇总 -->
      <div v-if="isStaff" class="card practice-stats">
        <div class="stat-item"><div class="stat-num">{{ practices.length }}</div><div class="muted small">作业 / 练习</div></div>
        <div class="stat-item"><div class="stat-num">{{ staffTotalSubmitted }}</div><div class="muted small">学员提交</div></div>
        <div class="stat-item"><div class="stat-num" :class="{ warn: staffTotalPending > 0 }">{{ staffTotalPending }}</div><div class="muted small">待批改</div></div>
        <div class="stat-item stat-tip">
          <span class="muted small">学员用拍照 / 语音交作业后，到这里进入批改，可直接在照片上圈画、写评语。</span>
        </div>
      </div>

      <!-- 筛选 -->
      <div class="row" style="justify-content:space-between;margin:26px 0 14px;flex-wrap:wrap;gap:10px">
        <div class="tabs" style="margin:0;border:none">
          <button class="tab" :class="{ active: tab === 'all' }" @click="tab = 'all'">全部 {{ practices.length }}</button>
          <template v-if="!isStaff">
            <button class="tab" :class="{ active: tab === 'todo' }" @click="tab = 'todo'">待完成 {{ todoCount }}</button>
            <button class="tab" :class="{ active: tab === 'done' }" @click="tab = 'done'">已完成</button>
          </template>
        </div>
        <div class="row" style="gap:8px;flex-wrap:wrap">
          <button class="chip" :class="{ active: !kindFilter }" @click="kindFilter = ''">所有类型</button>
          <button v-for="k in PRACTICE_KINDS" :key="k.id" class="chip" :class="{ active: kindFilter === k.id }"
            @click="kindFilter = kindFilter === k.id ? '' : k.id">{{ k.name }}</button>
        </div>
      </div>

      <div v-if="err" class="alert error">{{ err }}</div>
      <div v-else-if="!filtered.length" class="card" style="text-align:center;padding:44px 20px;color:var(--text-3)">
        <Icon name="target" :size="44" />
        <p style="margin:10px 0 0">{{ isStaff ? '还没有布置作业，进入课程管理即可出题' : '还没有练习，老师布置后会出现在这里' }}</p>
      </div>

      <!-- 老师：按课程分组 -->
      <template v-else-if="isStaff">
        <div v-for="g in groups" :key="g.courseId" class="card" style="margin-bottom:16px">
          <div class="row" style="justify-content:space-between;margin-bottom:10px">
            <div class="row" style="gap:8px">
              <Icon name="book" :size="17" />
              <b>{{ g.title }}</b>
              <span class="muted small">{{ g.list.length }} 份作业</span>
              <span v-if="g.pending" class="badge-soft bad">待批改 {{ g.pending }}</span>
            </div>
            <button class="btn small" @click="openStudio(g.courseId)"><Icon name="edit" :size="14" /> 管理 / 批改</button>
          </div>
          <div v-for="p in g.list" :key="p.id" class="practice-row" @click="open(p)">
            <div class="row" style="gap:8px">
              <span class="practice-kind" :class="p.kind">
                <Icon :name="practiceKindIcon(p.kind)" :size="13" /> {{ practiceKindLabel(p.kind) }}
              </span>
              <span class="badge-soft">{{ subjectLabel(p.subject) }}</span>
              <span v-if="!p.published" class="badge-soft bad">未发布</span>
              <span v-if="p.allow_photo" class="badge-soft"><Icon name="camera" :size="11" /> 拍照</span>
              <span v-if="p.allow_voice" class="badge-soft"><Icon name="mic" :size="11" /> 语音</span>
              <span v-if="p.proctor?.mode === 'strict'" class="badge-soft warn"><Icon name="lock" :size="11" /> 严格监考</span>
            </div>
            <div class="practice-row-title">{{ p.title }}</div>
            <div class="muted small row" style="gap:14px;flex-wrap:wrap">
              <span>{{ p.question_count }} 题 · {{ fmtScore(p.total_score) }} 分</span>
              <span>已交 {{ p.submitted_count || 0 }}<template v-if="p.student_count"> / {{ p.student_count }} 人</template></span>
              <span v-if="p.pending_count" style="color:var(--warn)">待批改 {{ p.pending_count }}</span>
              <span v-if="p.deadline">截止 {{ fmtDate(p.deadline) }}</span>
              <button v-if="p.proctor?.mode === 'strict'" class="btn small ghost" @click.stop="openProctor(p)">
                <Icon name="eye" :size="13" /> 监考台
              </button>
            </div>
          </div>
        </div>
      </template>

      <!-- 学员：卡片列表 -->
      <div v-else class="practice-grid">
        <div v-for="p in filtered" :key="p.id" class="card practice-card" @click="open(p)">
          <div class="row" style="justify-content:space-between;gap:8px">
            <div class="row" style="gap:8px;flex-wrap:wrap">
              <span class="practice-kind" :class="p.kind">
                <Icon :name="practiceKindIcon(p.kind)" :size="13" /> {{ practiceKindLabel(p.kind) }}
              </span>
              <span class="badge-soft">{{ subjectLabel(p.subject) }}</span>
              <span v-if="p.allow_photo" class="badge-soft"><Icon name="camera" :size="11" /></span>
              <span v-if="p.allow_voice" class="badge-soft"><Icon name="mic" :size="11" /></span>
            </div>
            <span class="practice-status" :class="statusClass(p)">{{ statusOf(p) }}</span>
          </div>
          <h3 style="margin:12px 0 6px;font-size:17px;line-height:1.4">{{ p.title }}</h3>
          <div v-if="p.course_title" class="muted small row" style="gap:4px">
            <Icon name="book" :size="13" /> {{ p.course_title }}
          </div>
          <div class="muted small row" style="gap:14px;margin-top:10px;flex-wrap:wrap">
            <span class="row" style="gap:4px"><Icon name="doc" :size="13" /> {{ p.question_count }} 题 · {{ fmtScore(p.total_score) }} 分</span>
            <span v-if="p.deadline" class="row" style="gap:4px"><Icon name="clock" :size="13" /> {{ fmtDate(p.deadline) }} 截止</span>
          </div>
        </div>
      </div>
    </template>

    <ProctorDashboard v-if="showProctor" :practice-id="proctorPractice?.id || 0"
      :title="proctorPractice?.title || ''" @close="showProctor = false" />
  </div>
</template>
