<script setup>
// 错题本：已交卷练习里判错的客观题，按练习分组；可一键生成「错题重练」
// （重练练习仅本人可见，固定普通模式、无监考）
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api'
import { fmtDate, practiceKindLabel, subjectLabel } from '../format'
import { toastOk, toastErr, confirmDialog } from '../ui'
import Icon from '../components/Icon.vue'
import FormulaText from '../components/FormulaText.vue'

const router = useRouter()

const loading = ref(true)
const err = ref('')
const groups = ref([])
const redoing = ref(0) // 正在生成重练的 practice_id
const openGroup = ref(0) // 展开详情的 practice_id

const totalWrong = computed(() => groups.value.reduce((n, g) => n + g.questions.length, 0))

function letter(i) {
  return String.fromCharCode(65 + i)
}

function myAnswerOf(g, item) {
  if (item.type === 'judge') {
    return item.my_answer === 'true' ? '对' : item.my_answer === 'false' ? '错' : (item.my_answer || '未作答')
  }
  if (item.type === 'choice') {
    const idx = (item.options || []).indexOf(item.my_answer)
    return idx >= 0 ? letter(idx) + ' · ' + item.my_answer : (item.my_answer || '未作答')
  }
  return item.my_answer || '未作答'
}

function answerOf(item) {
  if (item.type === 'judge') return item.answer === 'true' ? '对' : '错'
  return item.answer || '—'
}

async function load() {
  loading.value = true
  err.value = ''
  try {
    const list = await api('/api/wrongbook')
    groups.value = Array.isArray(list) ? list : []
    if (groups.value.length) openGroup.value = groups.value[0].practice_id
  } catch (e) {
    err.value = e.message
  } finally {
    loading.value = false
  }
}
onMounted(load)

async function redo(g) {
  if (redoing.value) return
  const ok = await confirmDialog(
    `把「${g.title}」里 ${g.questions.length} 道错题合成一份新的练习重新做？普通模式，不计时。`,
    { title: '错题重练', okText: '开始重练' }
  )
  if (!ok) return
  redoing.value = g.practice_id
  try {
    const p = await api(`/api/practices/${g.practice_id}/redo-wrong`, { method: 'POST' })
    toastOk('重练已生成')
    router.push(`/practice/${p.id}`)
  } catch (e) {
    toastErr(e)
  } finally {
    redoing.value = 0
  }
}
</script>

<template>
  <div class="container">
    <div class="page-head">
      <div>
        <h1 class="page-title">错题本</h1>
        <div class="page-sub" v-if="!loading">共 {{ groups.length }} 份练习、{{ totalWrong }} 道错题；点「重练错题」合成一份新练习再做一遍</div>
      </div>
      <RouterLink to="/practice" class="btn small"><Icon name="arrow-left" :size="15" /> 练习中心</RouterLink>
    </div>

    <div v-if="loading" class="skeleton" style="height:220px"></div>
    <div v-else-if="err" class="alert error">{{ err }}</div>
    <div v-else-if="!groups.length" class="card" style="text-align:center;padding:44px 20px;color:var(--text-3)">
      <Icon name="check" :size="44" />
      <p style="margin:10px 0 0">错题本是空的——交卷并出分后，答错的客观题会自动收进这里</p>
    </div>

    <template v-else>
      <div v-for="g in groups" :key="g.practice_id" class="card wrong-group">
        <div class="row" style="justify-content:space-between;gap:10px;cursor:pointer" @click="openGroup = openGroup === g.practice_id ? 0 : g.practice_id">
          <div class="row" style="gap:8px;flex-wrap:wrap;min-width:0">
            <span class="practice-kind" :class="g.kind"><Icon name="refresh" :size="13" /> {{ practiceKindLabel(g.kind) }}</span>
            <span class="badge-soft">{{ subjectLabel(g.subject) }}</span>
            <b class="wrong-title">{{ g.title }}</b>
            <span class="badge-soft bad">{{ g.questions.length }} 道错题</span>
          </div>
          <div class="row" style="gap:8px;flex-shrink:0" @click.stop>
            <span class="muted small">{{ fmtDate(g.submitted_at) }}</span>
            <button class="btn small primary" :disabled="redoing === g.practice_id" @click="redo(g)">
              <Icon name="refresh" :size="14" /> {{ redoing === g.practice_id ? '生成中…' : '重练错题' }}
            </button>
            <button class="btn small ghost" @click="openGroup = openGroup === g.practice_id ? 0 : g.practice_id">
              {{ openGroup === g.practice_id ? '收起' : '展开' }}
            </button>
          </div>
        </div>

        <template v-if="openGroup === g.practice_id">
          <div class="divider" style="margin:12px 0"></div>
          <div v-for="item in g.questions" :key="item.question_id" class="wrong-item">
            <div class="q-stem">
              <FormulaText :text="item.stem" />
              <span class="muted small">（{{ item.type === 'choice' ? '选择' : item.type === 'judge' ? '判断' : '填空' }} · {{ item.score }} 分）</span>
            </div>
            <div v-if="item.type === 'choice' && item.options?.length" class="q-options" style="margin-top:6px">
              <div v-for="(opt, i) in item.options" :key="i" class="opt-row" :class="opt === item.answer ? 'correct' : (opt === item.my_answer ? 'wrong' : '')">
                <span class="opt-letter">{{ letter(i) }}</span>
                <span><FormulaText :text="opt" /></span>
              </div>
            </div>
            <div class="row" style="gap:16px;margin-top:6px;flex-wrap:wrap">
              <span class="small">我的答案：<b style="color:var(--danger)">{{ myAnswerOf(g, item) }}</b></span>
              <span class="small">正确答案：<b style="color:var(--brand-700)">{{ answerOf(item) }}</b></span>
            </div>
            <div v-if="item.analysis" class="q-analysis" style="margin-top:6px">
              <span class="muted small">解析：</span><FormulaText :text="item.analysis" />
            </div>
          </div>
        </template>
      </div>
    </template>
  </div>
</template>

<style scoped>
.wrong-group { margin-bottom: 14px; }
.wrong-title { font-size: 15.5px; }
.wrong-item { border-top: 1px dashed var(--line); padding: 12px 2px; }
</style>
