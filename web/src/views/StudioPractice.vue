<script setup>
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '../api'
import {
  fmtDate, toLocalInput, monthNow, practiceKindIcon, practiceKindLabel, subjectLabel,
  PRACTICE_KINDS, PRACTICE_SUBJECTS, QUESTION_TYPES
} from '../format'
import { toastOk, toastErr, confirmDialog } from '../ui'
import Modal from '../components/Modal.vue'
import Icon from '../components/Icon.vue'

const route = useRoute()
const courseId = computed(() => route.params.courseId)

const course = ref(null)
const loading = ref(true)
const err = ref('')
const practices = ref([])
const tab = ref('practices')

function fmtScore(n) {
  const v = Number(n || 0)
  return v % 1 === 0 ? String(v) : v.toFixed(1)
}

function letter(i) {
  return String.fromCharCode(65 + i)
}

async function load() {
  loading.value = true
  err.value = ''
  try {
    course.value = await api(`/api/courses/${courseId.value}`)
    if (!course.value.can_manage) {
      err.value = '你没有管理该课程的权限'
      loading.value = false
      return
    }
    await loadPractices()
  } catch (e) {
    err.value = e.message
  }
  loading.value = false
}
onMounted(load)

async function loadPractices() {
  const list = await api(`/api/practices?course_id=${courseId.value}`)
  practices.value = Array.isArray(list) ? list : []
}

// ---------------- 新建 / 编辑 ----------------

function blankQuestion(type = 'choice') {
  return {
    type,
    stem: '',
    options: type === 'choice' ? ['', ''] : [],
    answer: type === 'judge' ? 'true' : '',
    score: 5,
    analysis: ''
  }
}

function blankForm() {
  return {
    title: '',
    kind: 'homework',
    subject: 'english',
    description: '',
    deadline: '',
    duration: 0,
    published: true,
    questions: [blankQuestion('choice')]
  }
}

const showEdit = ref(false)
const editing = ref(null)
const saving = ref(false)
const form = ref(blankForm())

function openCreate() {
  editing.value = null
  form.value = blankForm()
  showEdit.value = true
}

async function openEdit(p) {
  try {
    const detail = await api(`/api/practices/${p.id}`)
    const pr = detail.practice
    editing.value = pr
    form.value = {
      title: pr.title,
      kind: pr.kind,
      subject: pr.subject,
      description: pr.description || '',
      deadline: toLocalInput(pr.deadline),
      duration: pr.duration || 0,
      published: !!pr.published,
      questions: (detail.questions || []).map((q) => ({
        type: q.type,
        stem: q.stem,
        options: q.options && q.options.length ? [...q.options] : (q.type === 'choice' ? ['', ''] : []),
        answer: q.answer || (q.type === 'judge' ? 'true' : ''),
        score: q.score || 5,
        analysis: q.analysis || ''
      }))
    }
    if (!form.value.questions.length) form.value.questions.push(blankQuestion('choice'))
    showEdit.value = true
  } catch (e) {
    toastErr(e)
  }
}

function addQuestion(type) {
  form.value.questions.push(blankQuestion(type))
}

function removeQuestion(i) {
  if (form.value.questions.length <= 1) {
    toastErr('至少保留一道题')
    return
  }
  form.value.questions.splice(i, 1)
}

function moveQuestion(i, dir) {
  const j = i + dir
  const arr = form.value.questions
  if (j < 0 || j >= arr.length) return
  const tmp = arr[i]
  arr[i] = arr[j]
  arr[j] = tmp
}

function changeType(q, type) {
  q.type = type
  if (type === 'choice') {
    if (!Array.isArray(q.options) || q.options.length < 2) q.options = ['', '']
    if (!/^[A-H]$/.test(String(q.answer || '').toUpperCase())) q.answer = 'A'
    else q.answer = String(q.answer).toUpperCase()
  } else if (type === 'judge') {
    q.options = []
    if (q.answer !== 'true' && q.answer !== 'false') q.answer = 'true'
  } else {
    q.options = []
    if (q.answer === 'true' || q.answer === 'false') q.answer = ''
  }
}

function addOption(q) {
  if (q.options.length >= 8) return
  q.options.push('')
}

function removeOption(q, i) {
  if (q.options.length <= 2) {
    toastErr('选择题至少保留两个选项')
    return
  }
  q.options.splice(i, 1)
  const idx = String(q.answer || 'A').toUpperCase().charCodeAt(0) - 65
  if (!(idx >= 0 && idx < q.options.length)) q.answer = 'A'
}

async function save() {
  if (saving.value) return
  if (!form.value.title.trim()) {
    toastErr('请填写练习标题')
    return
  }
  const payload = {
    title: form.value.title,
    kind: form.value.kind,
    subject: form.value.subject,
    description: form.value.description,
    deadline: form.value.deadline || '',
    duration: Number(form.value.duration) || 0,
    published: !!form.value.published,
    questions: form.value.questions.map((q) => ({
      type: q.type,
      stem: q.stem,
      options: q.type === 'choice' ? q.options.filter((o) => String(o).trim()) : [],
      answer: q.answer,
      score: Number(q.score) || 1,
      analysis: q.analysis || ''
    }))
  }
  saving.value = true
  try {
    if (editing.value) {
      await api(`/api/practices/${editing.value.id}`, { method: 'PUT', json: payload })
    } else {
      await api(`/api/courses/${courseId.value}/practices`, { method: 'POST', json: payload })
    }
    showEdit.value = false
    await loadPractices()
    toastOk(editing.value ? '已保存' : '练习已创建')
  } catch (e) {
    toastErr(e)
  } finally {
    saving.value = false
  }
}

async function togglePublish(p) {
  try {
    await api(`/api/practices/${p.id}`, {
      method: 'PUT',
      json: {
        title: p.title, kind: p.kind, subject: p.subject, description: p.description,
        deadline: p.deadline, duration: p.duration, published: !p.published
      }
    })
    p.published = !p.published
    toastOk(p.published ? '已发布，学员可见' : '已撤下')
  } catch (e) {
    toastErr(e)
  }
}

async function removePractice(p) {
  const ok = await confirmDialog(`确定删除《${p.title}》吗？学员的提交记录会一起删除。`, { danger: true, okText: '删除' })
  if (!ok) return
  try {
    await api(`/api/practices/${p.id}`, { method: 'DELETE' })
    await loadPractices()
    toastOk('已删除')
  } catch (e) {
    toastErr(e)
  }
}

// ---------------- 批改 ----------------

const showGrade = ref(false)
const gradePractice = ref(null)
const submissions = ref([])
const gradeQuestions = ref([])
const gradeLoading = ref(false)
const gradeTarget = ref(null)
const gradeForm = ref({ score: 0, comment: '' })
const gradeSaving = ref(false)

async function openGrade(p) {
  gradePractice.value = p
  showGrade.value = true
  gradeLoading.value = true
  gradeTarget.value = null
  submissions.value = []
  gradeQuestions.value = []
  try {
    const [subs, detail] = await Promise.all([
      api(`/api/practices/${p.id}/submissions`),
      api(`/api/practices/${p.id}`)
    ])
    submissions.value = Array.isArray(subs) ? subs : []
    gradeQuestions.value = detail.questions || []
    if (submissions.value.length) pickSubmission(submissions.value[0])
  } catch (e) {
    toastErr(e)
  } finally {
    gradeLoading.value = false
  }
}

function pickSubmission(s) {
  gradeTarget.value = s
  gradeForm.value = { score: Number(s.score || 0), comment: s.comment || '' }
}

function answerOf(s, qid) {
  const hit = (s.answers || []).find((a) => a.question_id === qid)
  return hit ? hit.answer : ''
}

async function saveGrade() {
  if (!gradeTarget.value || gradeSaving.value) return
  gradeSaving.value = true
  try {
    const updated = await api(`/api/practices/${gradePractice.value.id}/submissions/${gradeTarget.value.id}/grade`, {
      method: 'POST',
      json: { score: Number(gradeForm.value.score) || 0, comment: gradeForm.value.comment || '' }
    })
    const i = submissions.value.findIndex((x) => x.id === updated.id)
    if (i >= 0) submissions.value[i] = updated
    gradeTarget.value = updated
    toastOk('批改已保存')
  } catch (e) {
    toastErr(e)
  } finally {
    gradeSaving.value = false
  }
}

// ---------------- 打卡统计 ----------------

const statsMonth = ref(monthNow())
const stats = ref([])
const statsLoading = ref(false)

async function loadStats() {
  statsLoading.value = true
  try {
    const list = await api(`/api/courses/${courseId.value}/checkins?month=${statsMonth.value}`)
    stats.value = Array.isArray(list) ? list : []
  } catch (e) {
    toastErr(e)
  } finally {
    statsLoading.value = false
  }
}

function switchTab(name) {
  tab.value = name
  if (name === 'checkins') loadStats()
}
</script>

<template>
  <div class="container">
    <div class="row" style="margin-bottom:10px">
      <RouterLink :to="'/studio/' + courseId" class="btn ghost small"><Icon name="arrow-left" :size="16" /> 返回课程管理</RouterLink>
    </div>

    <div class="page-head">
      <div>
        <h1 class="page-title">作业与练习</h1>
        <div class="page-sub">{{ course?.title || '加载中…' }} · 出题、发布、批改与打卡统计</div>
      </div>
      <button class="btn primary" @click="openCreate"><Icon name="plus" :size="16" /> 新建练习</button>
    </div>

    <div v-if="loading" class="skeleton" style="height:220px"></div>
    <div v-else-if="err" class="alert error">{{ err }}</div>

    <template v-else>
      <div class="tabs">
        <button class="tab" :class="{ active: tab === 'practices' }" @click="switchTab('practices')">作业练习 {{ practices.length }}</button>
        <button class="tab" :class="{ active: tab === 'checkins' }" @click="switchTab('checkins')">打卡统计</button>
      </div>

      <template v-if="tab === 'practices'">
        <div v-if="!practices.length" class="card" style="text-align:center;padding:44px 20px;color:var(--text-3)">
          <Icon name="file-plus" :size="44" />
          <p style="margin:10px 0 0">还没有练习，点右上角「新建练习」出题吧</p>
          <p class="small">支持作业、考卷、单词练习、句子练习；题型有选择 / 填空 / 判断 / 简答</p>
        </div>

        <div v-for="p in practices" :key="p.id" class="card practice-manage-row">
          <div class="row" style="justify-content:space-between;align-items:flex-start;gap:12px">
            <div style="min-width:0">
              <div class="row" style="gap:8px">
                <span class="practice-kind" :class="p.kind">
                  <Icon :name="practiceKindIcon(p.kind)" :size="13" /> {{ practiceKindLabel(p.kind) }}
                </span>
                <span class="badge-soft">{{ subjectLabel(p.subject) }}</span>
                <span v-if="!p.published" class="badge-soft bad">未发布</span>
              </div>
              <h3 style="margin:8px 0 4px;font-size:16px">{{ p.title }}</h3>
              <div class="muted small row" style="gap:14px">
                <span>{{ p.question_count }} 题 · {{ fmtScore(p.total_score) }} 分</span>
                <span>已提交 {{ p.submitted_count || 0 }}</span>
                <span v-if="p.pending_count" style="color:var(--warn)">待批改 {{ p.pending_count }}</span>
                <span v-if="p.deadline">截止 {{ fmtDate(p.deadline) }}</span>
              </div>
            </div>
            <div class="row" style="gap:8px;flex-shrink:0">
              <button class="btn small" @click="togglePublish(p)">{{ p.published ? '撤下' : '发布' }}</button>
              <button class="btn small" @click="openEdit(p)"><Icon name="edit" :size="14" /> 编辑</button>
              <button class="btn small" @click="openGrade(p)"><Icon name="check" :size="14" /> 批改</button>
              <button class="btn small danger" title="删除" @click="removePractice(p)"><Icon name="trash" :size="14" /></button>
            </div>
          </div>
        </div>
      </template>

      <template v-else>
        <div class="card">
          <div class="row" style="justify-content:space-between;margin-bottom:14px">
            <div class="row">
              <input v-model="statsMonth" type="month" class="input" style="width:170px" />
              <button class="btn small" @click="loadStats">查询</button>
            </div>
            <span class="muted small">按学员统计本月打卡天数与词句数量</span>
          </div>
          <div v-if="statsLoading" class="skeleton" style="height:120px"></div>
          <div v-else-if="!stats.length" class="empty" style="padding:26px 0">本月还没有学员打卡</div>
          <div v-else class="table-wrap">
            <table class="table">
              <thead>
                <tr><th>学员</th><th>打卡天数</th><th>词句数量</th><th>今日</th></tr>
              </thead>
              <tbody>
                <tr v-for="s in stats" :key="s.student_id">
                  <td>{{ s.student_name || '学员 #' + s.student_id }}</td>
                  <td>{{ s.days }} 天</td>
                  <td>{{ s.count }} 个</td>
                  <td><span v-if="s.today" class="badge-soft ok">已打卡</span><span v-else class="muted small">—</span></td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </template>
    </template>

    <!-- 出题弹窗 -->
    <Modal v-if="showEdit" :title="editing ? '编辑练习' : '新建练习'" wide :close-on-mask="false" @close="!saving && (showEdit = false)">
      <div class="form-grid">
        <div class="field">
          <label>练习标题 *</label>
          <input v-model="form.title" class="input" placeholder="例如：Unit 1 单词小测" />
        </div>
        <div class="field">
          <label>类型</label>
          <select v-model="form.kind" class="select">
            <option v-for="k in PRACTICE_KINDS" :key="k.id" :value="k.id">{{ k.name }}</option>
          </select>
        </div>
        <div class="field">
          <label>科目</label>
          <select v-model="form.subject" class="select">
            <option v-for="s in PRACTICE_SUBJECTS" :key="s.id" :value="s.id">{{ s.name }}</option>
          </select>
        </div>
        <div class="field">
          <label>截止时间（选填）</label>
          <input v-model="form.deadline" type="datetime-local" class="input" />
        </div>
        <div class="field">
          <label>建议用时（分钟，0 = 不限）</label>
          <input v-model.number="form.duration" type="number" min="0" max="1440" class="input" />
        </div>
        <div class="field">
          <label>发布状态</label>
          <label class="switch" :class="{ on: form.published }" @click="form.published = !form.published">
            <span class="switch-track"><span class="switch-dot"></span></span>
            <span class="small">{{ form.published ? '已发布（学员可见）' : '暂不发布' }}</span>
          </label>
        </div>
      </div>
      <div class="field">
        <label>说明（选填）</label>
        <textarea v-model="form.description" class="textarea" style="min-height:64px" placeholder="例如：Unit 1 单词表，交卷后自动判分"></textarea>
      </div>

      <div class="divider"></div>
      <div class="row" style="justify-content:space-between;margin-bottom:10px">
        <div class="panel-title" style="margin:0"><Icon name="edit" :size="17" /> 题目（{{ form.questions.length }}）</div>
        <div class="row" style="gap:6px">
          <button v-for="t in QUESTION_TYPES" :key="t.id" class="btn small" @click="addQuestion(t.id)">
            <Icon name="plus" :size="13" /> {{ t.name }}
          </button>
        </div>
      </div>

      <div v-for="(q, qi) in form.questions" :key="qi" class="q-editor">
        <div class="row" style="justify-content:space-between;align-items:center">
          <div class="row" style="gap:8px">
            <b>第 {{ qi + 1 }} 题</b>
            <select :value="q.type" class="select" style="width:110px;height:32px" @change="changeType(q, $event.target.value)">
              <option v-for="t in QUESTION_TYPES" :key="t.id" :value="t.id">{{ t.name }}</option>
            </select>
            <span class="muted small">分值</span>
            <input v-model.number="q.score" type="number" min="0.5" max="100" step="0.5" class="input" style="width:82px;height:32px" />
          </div>
          <div class="row" style="gap:4px">
            <button class="btn small ghost" :disabled="qi === 0" @click="moveQuestion(qi, -1)">上移</button>
            <button class="btn small ghost" :disabled="qi === form.questions.length - 1" @click="moveQuestion(qi, 1)">下移</button>
            <button class="btn small danger" @click="removeQuestion(qi)">删除</button>
          </div>
        </div>
        <textarea v-model="q.stem" class="textarea" style="min-height:60px;margin-top:10px" placeholder="题干，例如：“苹果”的英文是？"></textarea>

        <template v-if="q.type === 'choice'">
          <div class="muted small" style="margin:8px 0 6px">选项（点左侧字母设为正确答案）</div>
          <div v-for="(opt, oi) in q.options" :key="oi" class="row" style="margin-bottom:6px;flex-wrap:nowrap">
            <button class="opt-letter" :class="{ active: String(q.answer).toUpperCase() === letter(oi) }"
              style="border:none;cursor:pointer" @click="q.answer = letter(oi)">{{ letter(oi) }}</button>
            <input v-model="q.options[oi]" class="input" :placeholder="'选项 ' + letter(oi)" />
            <button class="btn small ghost" @click="removeOption(q, oi)"><Icon name="close" :size="14" /></button>
          </div>
          <button v-if="q.options.length < 8" class="btn small" @click="addOption(q)"><Icon name="plus" :size="13" /> 添加选项</button>
        </template>

        <template v-else-if="q.type === 'judge'">
          <div class="row" style="margin-top:8px">
            <span class="muted small">正确答案</span>
            <select v-model="q.answer" class="select" style="width:100px">
              <option value="true">对</option>
              <option value="false">错</option>
            </select>
          </div>
        </template>

        <template v-else-if="q.type === 'fill'">
          <input v-model="q.answer" class="input" style="margin-top:8px"
            placeholder="参考答案，多个可接受答案用 | 分隔，例如 apple|apples" />
        </template>

        <template v-else>
          <textarea v-model="q.answer" class="textarea" style="min-height:56px;margin-top:8px"
            placeholder="参考答案（选填，老师批改时参考）"></textarea>
        </template>

        <input v-model="q.analysis" class="input" style="margin-top:8px" placeholder="解析（选填，出分后学员可见）" />
      </div>

      <template #foot>
        <button class="btn" :disabled="saving" @click="showEdit = false">取消</button>
        <button class="btn primary" :disabled="saving" @click="save">{{ saving ? '保存中…' : '保存' }}</button>
      </template>
    </Modal>

    <!-- 批改弹窗 -->
    <Modal v-if="showGrade" :title="'批改 · ' + (gradePractice?.title || '')" wide @close="showGrade = false">
      <div v-if="gradeLoading" class="skeleton" style="height:240px"></div>
      <div v-else-if="!submissions.length" class="empty" style="padding:30px 0">还没有学员提交</div>
      <div v-else class="grade-layout">
        <div class="grade-list">
          <button v-for="s in submissions" :key="s.id" class="grade-item"
            :class="{ active: gradeTarget && gradeTarget.id === s.id }" @click="pickSubmission(s)">
            <div class="row" style="justify-content:space-between">
              <b>{{ s.student_name || '学员 #' + s.student_id }}</b>
              <span class="badge-soft" :class="s.graded ? 'ok' : 'bad'">{{ s.graded ? '已批改' : '待批改' }}</span>
            </div>
            <div class="muted small" style="margin-top:2px">
              {{ s.graded ? '得分 ' + fmtScore(s.score) + ' / ' + fmtScore(s.total) : '客观题 ' + fmtScore(s.score) + ' 分' }}
            </div>
          </button>
        </div>

        <div v-if="gradeTarget" class="grade-detail">
          <div class="row" style="justify-content:space-between;margin-bottom:10px">
            <b>{{ gradeTarget.student_name || '学员 #' + gradeTarget.student_id }}</b>
            <span class="muted small">{{ fmtDate(gradeTarget.submitted_at) }} 提交</span>
          </div>
          <div v-for="(q, qi) in gradeQuestions" :key="q.id" class="q-review">
            <div class="q-stem" style="font-size:13.5px">{{ qi + 1 }}. {{ q.stem }} <span class="muted small">（{{ q.score }} 分）</span></div>
            <div class="small" style="margin-top:4px">
              <span class="muted">学员答案：</span>
              <b :style="{ color: answerOf(gradeTarget, q.id) ? 'inherit' : 'var(--text-3)' }">{{ answerOf(gradeTarget, q.id) || '未作答' }}</b>
              <template v-if="q.answer">
                <span class="muted" style="margin-left:14px">参考答案：</span>
                <b style="color:var(--brand-700)">{{ q.type === 'judge' ? (q.answer === 'true' ? '对' : '错') : q.answer }}</b>
              </template>
            </div>
            <div v-if="q.analysis" class="muted small" style="margin-top:2px">解析：{{ q.analysis }}</div>
          </div>
          <div class="divider" style="margin:14px 0"></div>
          <div class="form-grid">
            <div class="field">
              <label>总分（满分 {{ fmtScore(gradePractice?.total_score) }}）</label>
              <input v-model.number="gradeForm.score" type="number" min="0" :max="gradePractice?.total_score || 100" step="0.5" class="input" />
            </div>
            <div class="field">
              <label>评语（选填）</label>
              <input v-model="gradeForm.comment" class="input" placeholder="例如：单词拼写再细心一点" />
            </div>
          </div>
          <div class="row" style="justify-content:flex-end">
            <button class="btn primary" :disabled="gradeSaving" @click="saveGrade">{{ gradeSaving ? '保存中…' : '保存批改' }}</button>
          </div>
        </div>
      </div>
    </Modal>
  </div>
</template>
