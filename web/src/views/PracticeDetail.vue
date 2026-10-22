<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '../api'
import { fmtDate, practiceKindIcon, practiceKindLabel, subjectLabel } from '../format'
import { toastOk, toastErr, confirmDialog } from '../ui'
import Icon from '../components/Icon.vue'

const route = useRoute()

const data = ref(null)
const loading = ref(true)
const err = ref('')
const submitting = ref(false)
const answers = reactive({})

const practice = computed(() => data.value?.practice || null)
const questions = computed(() => data.value?.questions || [])
const submission = computed(() => data.value?.submission || null)
// 标准答案/解析是否可见：老师始终可见；学员在出分后可见
const reveal = computed(() => !!practice.value?.can_manage || !!submission.value?.graded)
// 已交卷（或老师预览）后进入结果视图
const done = computed(() => !!submission.value || !!practice.value?.can_manage)

function letter(i) {
  return String.fromCharCode(65 + i)
}

function fmtScore(n) {
  const v = Number(n || 0)
  return v % 1 === 0 ? String(v) : v.toFixed(1)
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

async function submit() {
  if (submitting.value || done.value) return
  const unanswered = questions.value.filter((q) => !String(answers[q.id] || '').trim()).length
  if (unanswered > 0) {
    const ok = await confirmDialog(`还有 ${unanswered} 题没有作答，确定交卷吗？`, { title: '交卷确认', okText: '交卷' })
    if (!ok) return
  }
  submitting.value = true
  try {
    await api(`/api/practices/${practice.value.id}/submit`, {
      method: 'POST',
      json: { answers: questions.value.map((q) => ({ question_id: q.id, answer: String(answers[q.id] || '') })) }
    })
    toastOk('交卷成功')
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
      <div class="row" style="margin-bottom:10px">
        <RouterLink to="/practice" class="btn ghost small"><Icon name="arrow-left" :size="16" /> 练习中心</RouterLink>
      </div>

      <div class="page-head" style="align-items:flex-start">
        <div>
          <h1 class="page-title">{{ practice.title }}</h1>
          <div class="page-sub row" style="gap:10px">
            <span class="practice-kind" :class="practice.kind">
              <Icon :name="practiceKindIcon(practice.kind)" :size="13" /> {{ practiceKindLabel(practice.kind) }}
            </span>
            <span class="badge-soft">{{ subjectLabel(practice.subject) }}</span>
            <span v-if="practice.course_title">{{ practice.course_title }}</span>
            <span>{{ questions.length }} 题 · {{ fmtScore(practice.total_score) }} 分</span>
            <span v-if="practice.deadline">截止 {{ fmtDate(practice.deadline) }}</span>
            <span v-if="practice.duration">建议用时 {{ practice.duration }} 分钟</span>
          </div>
        </div>
        <span v-if="practice.can_manage" class="badge-soft">老师预览</span>
      </div>

      <div v-if="practice.description" class="card" style="margin-bottom:18px;background:var(--line-2);border:none">
        <div class="muted small" style="white-space:pre-wrap">{{ practice.description }}</div>
      </div>

      <!-- 成绩 / 状态 -->
      <div v-if="submission" class="card result-banner" :class="{ pending: !submission.graded }">
        <template v-if="submission.graded">
          <div class="row" style="justify-content:space-between;align-items:flex-start">
            <div>
              <div class="muted small">本次得分</div>
              <div class="score-big">{{ fmtScore(submission.score) }}<span class="muted" style="font-size:16px"> / {{ fmtScore(submission.total) }}</span></div>
            </div>
            <div v-if="submission.comment" class="grade-comment">
              <div class="muted small">老师评语</div>
              <div>{{ submission.comment }}</div>
            </div>
          </div>
        </template>
        <template v-else>
          <div class="row" style="gap:8px">
            <Icon name="clock" :size="20" />
            <b>已交卷，等待老师批改</b>
            <span class="muted small">客观题已得 {{ fmtScore(submission.score) }} 分，简答题批改后出总分</span>
          </div>
        </template>
      </div>

      <!-- 题目 -->
      <div v-for="(q, qi) in questions" :key="q.id" class="q-block">
        <div class="row" style="justify-content:space-between;align-items:flex-start">
          <div class="q-stem">
            {{ qi + 1 }}. {{ q.stem }}
            <span class="muted small">（{{ q.type === 'short' ? '简答' : q.type === 'choice' ? '选择' : q.type === 'judge' ? '判断' : '填空' }} · {{ q.score }} 分）</span>
          </div>
          <span v-if="done && reveal && q.type !== 'short'" class="badge-soft" :class="correctOf(q.id) ? 'ok' : 'bad'">
            {{ correctOf(q.id) ? '正确' : '错误' }}
          </span>
          <span v-else-if="done && q.type === 'short'" class="badge-soft">老师评分</span>
        </div>

        <!-- 作答 -->
        <template v-if="!done">
          <div v-if="q.type === 'choice'" class="q-options">
            <div v-for="(opt, i) in q.options" :key="i" class="opt-row"
              :class="{ active: answers[q.id] === letter(i) }" @click="answers[q.id] = letter(i)">
              <span class="opt-letter">{{ letter(i) }}</span>
              <span>{{ opt }}</span>
            </div>
          </div>
          <div v-else-if="q.type === 'judge'" class="row" style="gap:10px;margin-top:10px">
            <button class="btn" :class="{ primary: answers[q.id] === 'true' }" @click="answers[q.id] = 'true'">对</button>
            <button class="btn" :class="{ primary: answers[q.id] === 'false' }" @click="answers[q.id] = 'false'">错</button>
          </div>
          <input v-else-if="q.type === 'fill'" v-model="answers[q.id]" class="input" style="margin-top:10px"
            :placeholder="'填写答案' + (q.options?.length ? '' : '')" />
          <textarea v-else v-model="answers[q.id]" class="textarea" style="margin-top:10px"
            placeholder="写下你的答案…"></textarea>
        </template>

        <!-- 结果 -->
        <template v-else>
          <div v-if="q.type === 'choice'" class="q-options">
            <div v-for="(opt, i) in q.options" :key="i" class="opt-row" :class="optionClass(q, i)">
              <span class="opt-letter">{{ letter(i) }}</span>
              <span>{{ opt }}</span>
            </div>
          </div>
          <div class="q-answer-row">
            <template v-if="!practice.can_manage">
              <span class="muted small">我的答案：</span>
              <b :class="{ 'ans-empty': !myAnswer(q.id) }">{{ myAnswer(q.id) || '未作答' }}</b>
            </template>
            <template v-if="reveal && q.answer">
              <span class="muted small" style="margin-left:18px">参考答案：</span>
              <b style="color:var(--brand-700)">{{ q.type === 'judge' ? (q.answer === 'true' ? '对' : '错') : q.answer }}</b>
            </template>
          </div>
          <div v-if="reveal && q.analysis" class="q-analysis">
            <span class="muted small">解析：</span>{{ q.analysis }}
          </div>
        </template>
      </div>

      <!-- 交卷 -->
      <div v-if="!done" class="card" style="position:sticky;bottom:14px;display:flex;justify-content:space-between;align-items:center;box-shadow:var(--shadow);margin-top:18px">
        <span class="muted small">交卷后客观题自动判分；含简答题时由老师批改后出总分</span>
        <button class="btn primary lg" :disabled="submitting || !questions.length" @click="submit">
          <Icon name="check" :size="17" /> {{ submitting ? '交卷中…' : '交卷' }}
        </button>
      </div>
    </template>
  </div>
</template>
