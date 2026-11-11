<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '../api'
import { fmtDate, practiceKindIcon, practiceKindLabel, subjectLabel } from '../format'
import { compressImage, MAX_AUDIOS, MAX_PHOTOS, practiceFileUrl, uploadPracticeFile } from '../files'
import { hasEnglish, preload, speak } from '../tts'
import { toastOk, toastErr, confirmDialog } from '../ui'
import Icon from '../components/Icon.vue'
import FormulaText from '../components/FormulaText.vue'
import AudioRecorder from '../components/AudioRecorder.vue'
import AttachmentList from '../components/AttachmentList.vue'

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

// 解析折叠：答错的题默认展开，答对的题默认收起（可点开看）
const openAnalysis = reactive({})

function analysisOpen(q) {
  if (preview.value || !done.value) return true
  if (!correctOf(q.id)) return true
  return !!openAnalysis[q.id]
}

function toggleAnalysis(qid) {
  openAnalysis[qid] = !openAnalysis[qid]
}

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

async function submit() {
  if (submitting.value || done.value) return
  if (!questions.value.length && !attachments.value.length) {
    toastErr('请先拍照或录音，再提交作业')
    return
  }
  if (questions.value.length) {
    const unanswered = questions.value.filter((q) => !String(answers[q.id] || '').trim()).length
    if (unanswered > 0) {
      const ok = await confirmDialog(`还有 ${unanswered} 题没有作答，确定交卷吗？`, { title: '交卷确认', okText: '交卷' })
      if (!ok) return
    }
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
          </div>
        </div>
        <div v-if="canManage" class="row" style="gap:8px">
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
          </div>
        </div>
      </div>

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

      <!-- 题目 -->
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
          <!-- 解析：答错的题默认展开；答对的题默认收起，点「查看解析」展开 -->
          <div v-if="reveal && q.analysis && analysisOpen(q)" class="q-analysis">
            <span class="muted small">解析：</span><FormulaText :text="q.analysis" />
          </div>
          <div v-else-if="reveal && q.analysis && !preview" class="q-answer-row">
            <button class="btn small ghost" @click="toggleAnalysis(q.id)">
              <Icon name="eye" :size="13" /> 查看解析
            </button>
          </div>
        </template>
      </div>

      <!-- 作业附件提交 -->
      <div v-if="!done && !preview && (practice.allow_photo || practice.allow_voice)" class="card" style="margin-top:18px">
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
      <div v-if="!done && !preview" class="card" style="position:sticky;bottom:14px;display:flex;justify-content:space-between;align-items:center;box-shadow:var(--shadow);margin-top:18px">
        <span class="muted small">
          <template v-if="questions.length">交卷后客观题自动判分；含简答题或照片 / 语音时由老师批改后出总分</template>
          <template v-else>提交后老师会批改照片 / 语音并给评语</template>
        </span>
        <button class="btn primary lg" :disabled="submitting || uploadingPhotos" @click="submit()">
          <Icon name="check" :size="17" /> {{ submitting ? '提交中…' : '提交作业' }}
        </button>
      </div>
    </template>
  </div>
</template>
