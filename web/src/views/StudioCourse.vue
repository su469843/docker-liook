<script setup>
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { createFFmpeg, fetchFile } from '@ffmpeg/ffmpeg'
import { api, postForm, rawPut } from '../api'
import { store } from '../store'
import { coverGradient, courseArt, fmtDur, fmtSize, kindIconName, kindLabel } from '../format'
import { toastOk, toastErr, confirmDialog } from '../ui'
import Modal from '../components/Modal.vue'
import Icon from '../components/Icon.vue'
import RichEditor from '../components/RichEditor.vue'

const route = useRoute()
const router = useRouter()

const course = ref(null)
const lessons = ref([])
const students = ref([])
const users = ref([])
const loading = ref(true)
const err = ref('')
const tab = ref('content')

const isAdmin = computed(() => store.user?.role === 'admin')
const teachers = computed(() => users.value.filter((u) => u.role === 'teacher'))
const coverUrl = computed(() => course.value?.cover
  ? `/api/courses/${course.value.id}/cover?v=${encodeURIComponent(course.value.updated_at || '')}` : '')

const groups = computed(() => {
  const out = []
  const map = new Map()
  for (const l of lessons.value) {
    const key = l.chapter || ''
    if (!map.has(key)) {
      const g = { chapter: key, items: [] }
      map.set(key, g)
      out.push(g)
    }
    map.get(key).items.push(l)
  }
  return out
})

async function load() {
  loading.value = true
  try {
    course.value = await api(`/api/courses/${route.params.courseId}`)
    if (!course.value.can_manage) {
      err.value = '你没有管理该课程的权限'
      loading.value = false
      return
    }
    lessons.value = await api(`/api/courses/${course.value.id}/lessons`)
    students.value = await api(`/api/courses/${course.value.id}/students`)
    if (isAdmin.value) users.value = await api('/api/users')
    setting.value = {
      title: course.value.title,
      description: course.value.description,
      category: course.value.category
    }
  } catch (e) {
    err.value = e.message
  }
  loading.value = false
}
onMounted(load)

// ---------------- 课时弹窗 ----------------
const showLesson = ref(false)
const lessonKind = ref('video')
const editing = ref(null) // 正在编辑的课时（null=新建）
const lessonForm = ref({ title: '', chapter: '', content: '' })
const videoFile = ref(null)
const videoMode = ref('copy')
const docFile = ref(null)
const uploading = ref(false)
const uploadStage = ref('idle')
const uploadPct = ref(0)
const uploadPctMax = ref(1)
const uploadText = ref('')

const ffmpeg = createFFmpeg({ corePath: '/ffmpeg/ffmpeg-core.js', log: false })

function openAdd(kind) {
  editing.value = null
  lessonKind.value = kind
  lessonForm.value = { title: '', chapter: '', content: '' }
  videoFile.value = null
  docFile.value = null
  videoMode.value = 'copy'
  uploadStage.value = 'idle'
  uploadPct.value = 0
  uploadText.value = ''
  showLesson.value = true
}

function openEdit(l) {
  editing.value = l
  lessonKind.value = l.kind
  lessonForm.value = { title: l.title, chapter: l.chapter, content: l.content || '' }
  videoFile.value = null
  docFile.value = null
  uploadStage.value = 'idle'
  showLesson.value = true
}

function pickVideo(e) {
  videoFile.value = e.target.files[0] || null
  if (videoFile.value && !lessonForm.value.title.trim()) {
    lessonForm.value.title = videoFile.value.name.replace(/\.[^.]+$/, '')
  }
}
function pickDoc(e) {
  docFile.value = e.target.files[0] || null
  if (docFile.value && !lessonForm.value.title.trim()) {
    lessonForm.value.title = docFile.value.name.replace(/\.[^.]+$/, '')
  }
}

function checkBase() {
  if (!lessonForm.value.title.trim()) { toastErr('请填写课时标题'); return false }
  return true
}

async function createLesson(kind, extra = {}) {
  return api(`/api/courses/${course.value.id}/lessons`, {
    method: 'POST',
    json: {
      title: lessonForm.value.title.trim(),
      chapter: lessonForm.value.chapter.trim(),
      kind,
      ...extra
    }
  })
}

async function saveLessonMeta() {
  if (!checkBase()) return
  uploading.value = true
  try {
    if (editing.value) {
      const body = { title: lessonForm.value.title.trim(), chapter: lessonForm.value.chapter.trim() }
      if (lessonKind.value === 'rich') body.content = lessonForm.value.content
      await api(`/api/lessons/${editing.value.id}`, { method: 'PUT', json: body })
      toastOk('课时已保存')
    } else if (lessonKind.value === 'rich') {
      await createLesson('rich', { content: lessonForm.value.content })
      toastOk('图文课时已创建')
    }
    showLesson.value = false
    await load()
  } catch (e) {
    toastErr(e)
  } finally {
    uploading.value = false
  }
}

// 浏览器内切片 + 分片上传
async function uploadVideo() {
  if (!checkBase()) return
  if (!videoFile.value) { toastErr('请选择视频文件'); return }
  uploading.value = true
  uploadStage.value = 'loading'
  uploadPct.value = 0
  uploadText.value = '正在加载切片引擎（首次约 24MB，请稍候）…'
  try {
    const created = await createLesson('video')
    if (!ffmpeg.isLoaded()) await ffmpeg.load()

    uploadStage.value = 'slice'
    uploadText.value = '正在浏览器内切片（视频越大耗时越久，请保持页面打开）…'
    const ext = (videoFile.value.name.match(/\.[^.]+$/) || ['.mp4'])[0]
    const inputName = 'input' + ext
    ffmpeg.FS('writeFile', inputName, await fetchFile(videoFile.value))
    const args = videoMode.value === 'copy'
      ? ['-i', inputName, '-c', 'copy', '-hls_time', '10', '-hls_list_size', '0', '-f', 'hls', 'out.m3u8']
      : ['-i', inputName, '-c:v', 'libx264', '-preset', 'veryfast', '-c:a', 'aac', '-hls_time', '10', '-hls_list_size', '0', '-f', 'hls', 'out.m3u8']
    await ffmpeg.run(...args)

    const names = ffmpeg.FS('readdir', '/').filter((n) => n === 'out.m3u8' || /^out\d+\.ts$/.test(n))
    if (!names.includes('out.m3u8')) {
      throw new Error('切片失败：未生成 m3u8。可能是视频编码不受支持，请改用「兼容转码」模式重试')
    }
    const segs = names.filter((n) => n.endsWith('.ts'))
      .sort((a, b) => a.localeCompare(b, undefined, { numeric: true }))

    uploadStage.value = 'upload'
    uploadPctMax.value = segs.length + 1
    uploadPct.value = 0
    await rawPut(`/api/lessons/${created.id}/m3u8`, ffmpeg.FS('readFile', 'out.m3u8'), 'application/vnd.apple.mpegurl')
    uploadPct.value = 1
    for (const n of segs) {
      uploadText.value = `正在上传分片 ${uploadPct.value}/${uploadPctMax.value}（${n}）`
      await rawPut(`/api/lessons/${created.id}/segments/${n}`, ffmpeg.FS('readFile', n), 'video/mp2t')
      uploadPct.value++
    }
    await api(`/api/lessons/${created.id}/complete`, { method: 'POST', json: { size: videoFile.value.size } })

    try { ffmpeg.FS('unlink', inputName) } catch {}
    for (const n of segs) { try { ffmpeg.FS('unlink', n) } catch {} }
    try { ffmpeg.FS('unlink', 'out.m3u8') } catch {}

    uploadStage.value = 'done'
    uploadText.value = '视频上传完成'
    toastOk('视频课时已上传')
    setTimeout(async () => {
      showLesson.value = false
      uploading.value = false
      uploadStage.value = 'idle'
      await load()
    }, 600)
  } catch (e) {
    toastErr(e)
    uploadStage.value = 'idle'
    uploading.value = false
  }
}

async function uploadDoc() {
  if (!checkBase()) return
  if (!docFile.value) { toastErr('请选择文档文件（pdf/word/excel/ppt/txt/zip）'); return }
  uploading.value = true
  uploadText.value = '正在上传文档…'
  try {
    const created = await createLesson('doc')
    const fd = new FormData()
    fd.append('file', docFile.value)
    await postForm(`/api/lessons/${created.id}/doc`, fd)
    toastOk('文档课时已上传')
    showLesson.value = false
    await load()
  } catch (e) {
    toastErr(e)
  } finally {
    uploading.value = false
  }
}

async function removeLesson(l) {
  const ok = await confirmDialog(`删除课时《${l.title}》？此操作不可恢复。`, { danger: true, okText: '删除' })
  if (!ok) return
  try {
    await api(`/api/lessons/${l.id}`, { method: 'DELETE' })
    toastOk('课时已删除')
    await load()
  } catch (e) {
    toastErr(e)
  }
}

// ---------------- 课程设置 ----------------
const setting = ref({ title: '', description: '', category: '' })
const savingSetting = ref(false)
const coverInput = ref(null)

async function saveSetting() {
  if (!setting.value.title.trim()) { toastErr('课程名称不能为空'); return }
  savingSetting.value = true
  try {
    const c = await api(`/api/courses/${course.value.id}`, { method: 'PUT', json: setting.value })
    course.value = { ...course.value, ...c }
    toastOk('课程信息已保存')
  } catch (e) {
    toastErr(e)
  } finally {
    savingSetting.value = false
  }
}

async function uploadCover(ev) {
  const f = ev.target.files[0]
  ev.target.value = ''
  if (!f) return
  try {
    const fd = new FormData()
    fd.append('file', f)
    await postForm(`/api/courses/${course.value.id}/cover`, fd)
    toastOk('封面已更新')
    await load()
  } catch (e) {
    toastErr(e)
  }
}

async function togglePublish() {
  const to = !course.value.published
  try {
    await api(`/api/courses/${course.value.id}/publish`, { method: 'POST', json: { published: to } })
    course.value.published = to
    toastOk(to ? '已上架到课程市场，学员可以自助加入' : '已从市场下架')
  } catch (e) {
    toastErr(e)
  }
}

async function regenInvite() {
  const ok = await confirmDialog('重新生成邀请码后旧邀请码会失效，确定吗？')
  if (!ok) return
  try {
    const r = await api(`/api/courses/${course.value.id}/invite`, { method: 'POST' })
    course.value.invite_code = r.invite_code
    toastOk('邀请码已重新生成')
  } catch (e) {
    toastErr(e)
  }
}

function copyInvite() {
  navigator.clipboard?.writeText(course.value.invite_code)
  toastOk('邀请码已复制')
}

async function assignTeacher(ev) {
  const tid = Number(ev.target.value)
  if (!tid) return
  try {
    await api(`/api/courses/${course.value.id}/teacher`, { method: 'POST', json: { teacher_id: tid } })
    toastOk('任课老师已更换')
    await load()
  } catch (e) {
    toastErr(e)
  }
}

async function removeCourse() {
  const ok = await confirmDialog(`删除课程《${course.value.title}》及其全部课时？此操作不可恢复。`, { danger: true, okText: '删除课程' })
  if (!ok) return
  try {
    await api(`/api/courses/${course.value.id}`, { method: 'DELETE' })
    toastOk('课程已删除')
    router.replace('/studio')
  } catch (e) {
    toastErr(e)
  }
}
</script>

<template>
  <div class="container">
    <div v-if="loading" class="skeleton" style="height:240px"></div>
    <div v-else-if="err" class="empty">{{ err }}</div>

    <template v-else-if="course">
      <div class="page-head">
        <div>
          <RouterLink to="/studio" class="btn ghost small"><Icon name="arrow-left" :size="16" /> 老师工作台</RouterLink>
          <h1 class="page-title" style="margin-top:10px">{{ course.title }}</h1>
          <div class="page-sub">
            {{ course.lesson_count }} 课时 · {{ course.student_count }} 名学员 ·
            <span v-if="course.published" style="color:var(--brand-700)">已上架到市场</span>
            <span v-else class="muted">未上架</span>
          </div>
        </div>
        <div class="row">
          <button class="btn" @click="togglePublish">{{ course.published ? '下架市场' : '上架市场' }}</button>
          <RouterLink :to="'/course/' + course.id" class="btn">预览课程主页</RouterLink>
        </div>
      </div>

      <div class="tabs">
        <button class="tab" :class="{ active: tab === 'content' }" @click="tab = 'content'">内容管理</button>
        <button class="tab" :class="{ active: tab === 'students' }" @click="tab = 'students'">学员（{{ students.length }}）</button>
        <button class="tab" :class="{ active: tab === 'settings' }" @click="tab = 'settings'">课程设置</button>
      </div>

      <!-- 内容管理 -->
      <div v-if="tab === 'content'">
        <div class="row" style="justify-content:space-between;margin-bottom:16px">
          <div class="muted small">共 {{ lessons.length }} 个课时（支持视频 / 图文 / 文档）</div>
          <div class="row">
            <button class="btn small primary" @click="openAdd('video')"><Icon name="video-file" :size="15" /> 视频课时</button>
            <button class="btn small" @click="openAdd('rich')"><Icon name="text" :size="15" /> 图文课时</button>
            <button class="btn small" @click="openAdd('doc')"><Icon name="doc" :size="15" /> 文档课时</button>
          </div>
        </div>

        <div v-if="lessons.length" class="card">
          <div v-for="(g, gi) in groups" :key="gi" class="chapter-block">
            <div v-if="g.chapter" class="chapter-name"><Icon name="folder" :size="15" /> {{ g.chapter }}</div>
            <div v-for="l in g.items" :key="l.id" class="lesson-row">
              <span class="lesson-modal"><Icon :name="kindIconName(l.kind)" :size="16" /></span>
              <div class="lesson-main">
                <div class="lesson-title">{{ l.title }}</div>
                <div class="lesson-sub">
                  <span>{{ kindLabel(l.kind) }}</span>
                  <span v-if="l.duration">{{ fmtDur(l.duration) }}</span>
                  <span v-if="l.size">{{ fmtSize(l.size) }}</span>
                  <span v-if="l.kind === 'doc' && l.filename" class="row" style="gap:4px"><Icon name="doc" :size="13" /> {{ l.filename }}</span>
                  <span v-if="l.status !== 'ready'" style="color:var(--warn)">未完成上传</span>
                </div>
              </div>
              <div class="row">
                <button class="btn small" @click="openEdit(l)">编辑</button>
                <RouterLink :to="'/learn/' + course.id + '/' + l.id" class="btn small">预览</RouterLink>
                <button class="btn small danger" @click="removeLesson(l)">删除</button>
              </div>
            </div>
          </div>
        </div>
        <div v-else class="empty">
          <span class="empty-ico"><Icon name="video-file" :size="44" /></span>
          还没有课时。点击上方「+ 视频课时 / + 图文课时 / + 文档课时」添加第一节课。
        </div>
      </div>

      <!-- 学员 -->
      <div v-else-if="tab === 'students'" class="table-wrap">
        <table class="table">
          <thead>
            <tr><th>学员</th><th>用户名</th><th>角色</th><th>加入方式</th><th>注册时间</th></tr>
          </thead>
          <tbody>
            <tr v-for="s in students" :key="s.id">
              <td>
                <div class="row">
                  <span class="avatar sm">{{ (s.display_name || '?')[0] }}</span>{{ s.display_name }}
                </div>
              </td>
              <td>{{ s.username }}</td>
              <td><span class="pill blue">学员</span></td>
              <td class="muted">邀请码 / 课程市场</td>
              <td class="muted">{{ (s.created_at || '').slice(0, 10) }}</td>
            </tr>
            <tr v-if="!students.length"><td colspan="5" class="muted center" style="padding:32px">还没有学员加入</td></tr>
          </tbody>
        </table>
      </div>

      <!-- 课程设置 -->
      <div v-else-if="tab === 'settings'" style="max-width:760px">
        <div class="card mb">
          <div class="panel-title">基本信息</div>
          <div class="field">
            <label>课程名称</label>
            <input v-model="setting.title" class="input" />
          </div>
          <div class="field">
            <label>课程简介</label>
            <textarea v-model="setting.description" class="textarea" placeholder="介绍课程内容、适合人群、学习收获"></textarea>
          </div>
          <div class="field">
            <label>分类</label>
            <input v-model="setting.category" class="input" placeholder="例如：编程 / 英语 / 职场" />
          </div>
          <button class="btn primary" :disabled="savingSetting" @click="saveSetting">
            {{ savingSetting ? '保存中…' : '保存基本信息' }}
          </button>
        </div>

        <div class="card mb">
          <div class="panel-title">封面图</div>
          <div class="row" style="align-items:flex-start">
            <div class="manage-cover" :style="!coverUrl ? { background: coverGradient(course.id), width: '200px', height: '118px' } : { width: '200px', height: '118px' }">
              <img v-if="coverUrl" :src="coverUrl" alt="封面" />
              <div v-else class="cover-fallback"><Icon :name="courseArt(course.id)" :size="42" /></div>
            </div>
            <div>
              <button class="btn" @click="coverInput.click()">上传封面</button>
              <p class="muted small" style="margin:8px 0 0">建议 16:9 比例，png/jpg，≤5MB</p>
            </div>
          </div>
          <input ref="coverInput" type="file" accept="image/png,image/jpeg,image/gif,image/webp" hidden @change="uploadCover" />
        </div>

        <div class="card mb">
          <div class="panel-title">上架与分享</div>
          <div class="row" style="justify-content:space-between;margin-bottom:14px">
            <div>
              <div style="font-weight:500">上架到课程市场</div>
              <div class="muted small">上架后所有学员都能在「课程市场」看到并自助加入；不上架则只能凭邀请码加入</div>
            </div>
            <span class="switch" :class="{ on: course.published }" @click="togglePublish">
              <span class="switch-track"><span class="switch-dot"></span></span>
            </span>
          </div>
          <div class="invite-box">
            <div>
              <div class="muted small">课程邀请码（学员输入后直接加入）</div>
              <div class="invite-code" @click="copyInvite">{{ course.invite_code }} <Icon name="copy" :size="14" /></div>
            </div>
            <div class="row">
              <button class="btn small" @click="copyInvite">复制</button>
              <button class="btn small" @click="regenInvite">重新生成</button>
            </div>
          </div>
        </div>

        <div v-if="isAdmin" class="card mb">
          <div class="panel-title">任课老师</div>
          <select class="select" :value="course.teacher_id" @change="assignTeacher">
            <option v-for="t in teachers" :key="t.id" :value="t.id">{{ t.display_name }}（{{ t.username }}）</option>
          </select>
        </div>

        <div class="card" style="border-color:#ffc9c9">
          <div class="panel-title" style="color:var(--danger)">危险操作</div>
          <div class="row" style="justify-content:space-between">
            <div class="muted small">删除课程会同时删除全部课时、学员关系与群聊记录，不可恢复</div>
            <button class="btn danger" @click="removeCourse">删除课程</button>
          </div>
        </div>
      </div>
    </template>

    <!-- 课时弹窗 -->
    <Modal v-if="showLesson" :title="(editing ? '编辑' : '添加') + kindLabel(lessonKind) + '课时'" wide
      :close-on-mask="!uploading" @close="!uploading && (showLesson = false)">
      <div class="form-grid">
        <div class="field">
          <label>课时标题 *</label>
          <input v-model="lessonForm.title" class="input" placeholder="例如：第 1 讲 · 变量与类型" :disabled="uploading" />
        </div>
        <div class="field">
          <label>章节（选填）</label>
          <input v-model="lessonForm.chapter" class="input" placeholder="例如：第一章 基础入门" :disabled="uploading" />
        </div>
      </div>

      <!-- 视频 -->
      <template v-if="lessonKind === 'video'">
        <div v-if="editing" class="alert info mb">视频课时只能修改标题和章节；如需替换视频，请删除后重新上传。</div>
        <template v-else>
          <div class="field">
            <label>视频文件</label>
            <input type="file" accept="video/*,.mp4,.mkv,.mov,.flv,.avi,.webm,.ts" :disabled="uploading" @change="pickVideo" />
            <span v-if="videoFile" class="muted small">{{ videoFile.name }} · {{ fmtSize(videoFile.size) }}</span>
          </div>
          <div class="field">
            <label>切片模式</label>
            <div class="row">
              <label class="row" style="gap:6px;cursor:pointer">
                <input v-model="videoMode" type="radio" value="copy" :disabled="uploading" />
                <Icon name="spark" :size="14" /> 快速切片（推荐，要求 H.264/AAC）
              </label>
              <label class="row" style="gap:6px;cursor:pointer">
                <input v-model="videoMode" type="radio" value="transcode" :disabled="uploading" />
                <Icon name="refresh" :size="14" /> 兼容转码（慢但兼容性好）
              </label>
            </div>
          </div>
          <div v-if="uploadStage !== 'idle'" class="card" style="background:#fafbfc">
            <p style="margin:0 0 10px">{{ uploadText }}</p>
            <div v-if="uploadStage === 'upload' || uploadStage === 'done'" class="progress">
              <i :style="{ width: Math.round((uploadPct / uploadPctMax) * 100) + '%' }"></i>
            </div>
            <div v-else class="row"><span class="spinner"></span><span class="muted small">处理中，请勿关闭页面…</span></div>
          </div>
        </template>
      </template>

      <!-- 图文 -->
      <template v-else-if="lessonKind === 'rich'">
        <div class="field">
          <label>图文内容</label>
          <RichEditor v-model="lessonForm.content" />
        </div>
        <p class="muted small">支持加粗、标题、列表、引用、图片和链接；服务端会过滤不安全的 HTML。</p>
      </template>

      <!-- 文档 -->
      <template v-else>
        <div v-if="editing" class="alert info mb">文档课时只能修改标题和章节；如需替换文档，请删除后重新上传。</div>
        <div v-else class="field">
          <label>文档文件</label>
          <input type="file" accept=".pdf,.doc,.docx,.xls,.xlsx,.ppt,.pptx,.txt,.md,.csv,.zip"
            :disabled="uploading" @change="pickDoc" />
          <span class="muted small">支持 pdf / word / excel / ppt / txt / md / csv / zip，≤200MB</span>
        </div>
      </template>

      <template #foot>
        <button class="btn" :disabled="uploading" @click="showLesson = false">取消</button>
        <button v-if="lessonKind === 'video' && !editing" class="btn primary" :disabled="uploading" @click="uploadVideo">
          {{ uploading ? '上传中…' : '开始切片并上传' }}
        </button>
        <button v-else-if="lessonKind === 'doc' && !editing" class="btn primary" :disabled="uploading" @click="uploadDoc">
          {{ uploading ? '上传中…' : '上传文档' }}
        </button>
        <button v-else class="btn primary" :disabled="uploading" @click="saveLessonMeta">
          {{ uploading ? '保存中…' : '保存' }}
        </button>
      </template>
    </Modal>
  </div>
</template>
