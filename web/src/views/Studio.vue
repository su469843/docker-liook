<script setup>
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api'
import { store } from '../store'
import { COVER_COLORS, fmtDate } from '../format'
import { toastOk, toastErr, confirmDialog } from '../ui'
import Modal from '../components/Modal.vue'
import Icon from '../components/Icon.vue'
import CoverArt from '../components/CoverArt.vue'

const router = useRouter()
const courses = ref([])
const users = ref([])
const loading = ref(true)
const showCreate = ref(false)
const creating = ref(false)
const form = ref({ title: '', description: '', category: '', cover_color: '', teacher_id: '' })

const isAdmin = computed(() => store.user?.role === 'admin')
const teachers = computed(() => users.value.filter((u) => u.role === 'teacher'))
const stats = computed(() => ({
  courses: courses.value.length,
  students: courses.value.reduce((n, c) => n + (c.student_count || 0), 0),
  lessons: courses.value.reduce((n, c) => n + (c.lesson_count || 0), 0),
  published: courses.value.filter((c) => c.published).length
}))

async function load() {
  loading.value = true
  try {
    courses.value = await api('/api/courses')
    if (isAdmin.value) users.value = await api('/api/users')
  } catch (e) {
    toastErr(e)
  }
  loading.value = false
}
onMounted(load)

async function create() {
  if (!form.value.title.trim()) { toastErr('请填写课程名称'); return }
  creating.value = true
  try {
    const body = {
      title: form.value.title.trim(),
      description: form.value.description.trim(),
      category: form.value.category.trim(),
      cover_color: form.value.cover_color || ''
    }
    if (isAdmin.value && form.value.teacher_id) body.teacher_id = Number(form.value.teacher_id)
    const course = await api('/api/courses', { method: 'POST', json: body })
    toastOk('课程已创建，去添加课时吧')
    showCreate.value = false
    form.value = { title: '', description: '', category: '', cover_color: '', teacher_id: '' }
    router.push('/studio/' + course.id)
  } catch (e) {
    toastErr(e)
  } finally {
    creating.value = false
  }
}

async function togglePublish(c) {
  const to = !c.published
  try {
    await api(`/api/courses/${c.id}/publish`, { method: 'POST', json: { published: to } })
    c.published = to
    toastOk(to ? '已上架到课程市场' : '已从市场下架')
  } catch (e) {
    toastErr(e)
  }
}

function copyCode(c) {
  if (!c.invite_code) return
  navigator.clipboard?.writeText(c.invite_code)
  toastOk('邀请码已复制，可以发给学员了')
}

async function remove(c) {
  const ok = await confirmDialog(`删除课程《${c.title}》及其全部课时？此操作不可恢复。`, { danger: true, okText: '删除' })
  if (!ok) return
  try {
    await api(`/api/courses/${c.id}`, { method: 'DELETE' })
    toastOk('课程已删除')
    await load()
  } catch (e) {
    toastErr(e)
  }
}
</script>

<template>
  <div class="container">
    <div class="page-head">
      <div>
        <h1 class="page-title">老师工作台</h1>
        <div class="page-sub">创建课程、管理课时内容，把课程分享给学员</div>
      </div>
      <button class="btn primary" @click="showCreate = true"><Icon name="plus" :size="16" /> 新建课程</button>
    </div>

    <div class="stat-grid">
      <div class="stat-card">
        <div class="stat-label">我的课程</div>
        <div class="stat-value">{{ stats.courses }}<small>门</small></div>
      </div>
      <div class="stat-card">
        <div class="stat-label">累计学员</div>
        <div class="stat-value">{{ stats.students }}<small>人</small></div>
      </div>
      <div class="stat-card">
        <div class="stat-label">课时总数</div>
        <div class="stat-value">{{ stats.lessons }}<small>节</small></div>
      </div>
      <div class="stat-card">
        <div class="stat-label">市场上架中</div>
        <div class="stat-value">{{ stats.published }}<small>门</small></div>
      </div>
    </div>

    <div v-if="loading" class="skeleton" style="height:200px"></div>
    <div v-else-if="courses.length" class="manage-list">
      <div v-for="c in courses" :key="c.id" class="manage-item">
        <div class="manage-cover" @click="router.push('/studio/' + c.id)">
          <img v-if="c.cover" :src="`/api/courses/${c.id}/cover?v=${encodeURIComponent(c.updated_at || '')}`" :alt="c.title" />
          <CoverArt v-else :course="c" compact :size="26" />
        </div>
        <div class="manage-main">
          <div class="row" style="gap:8px">
            <span class="course-title" style="min-height:0;cursor:pointer" @click="router.push('/studio/' + c.id)">{{ c.title }}</span>
            <span v-if="c.published" class="pill green">市场上架中</span>
            <span v-else class="pill gray">未上架</span>
          </div>
          <div class="muted small">
            {{ c.lesson_count }} 课时 · {{ c.student_count }} 名学员 · 创建于 {{ fmtDate(c.created_at) }}
            <template v-if="isAdmin"> · 老师：{{ c.teacher_name }}</template>
          </div>
          <div class="row" style="gap:10px">
            <span class="muted small">邀请码</span>
            <span class="invite-code" style="font-size:13px" @click="copyCode(c)">{{ c.invite_code || '—' }} <Icon name="copy" :size="13" /></span>
          </div>
        </div>
        <div class="row">
          <button class="btn small" @click="togglePublish(c)">{{ c.published ? '下架' : '上架市场' }}</button>
          <button class="btn small primary" @click="router.push('/studio/' + c.id)">管理内容</button>
          <button class="btn small danger" @click="remove(c)">删除</button>
        </div>
      </div>
    </div>
    <div v-else class="empty">
      <span class="empty-ico"><Icon name="class" :size="44" /></span>
      还没有课程，点右上角「新建课程」创建第一门课吧。<br />
      <span class="muted small">创建后可以上传视频、图文、文档课时，然后把邀请码发给学员，或上架到课程市场。</span>
    </div>

    <Modal v-if="showCreate" title="新建课程" @close="showCreate = false">
      <div class="field">
        <label>课程名称 *</label>
        <input v-model="form.title" class="input" placeholder="例如：Go 语言从入门到实战" @keyup.enter="create" />
      </div>
      <div class="field">
        <label>课程简介</label>
        <textarea v-model="form.description" class="textarea" placeholder="简单介绍课程内容和适合人群"></textarea>
      </div>
      <div class="form-grid">
        <div class="field">
          <label>分类（选填）</label>
          <input v-model="form.category" class="input" placeholder="例如：编程 / 英语 / 职场" />
        </div>
        <div v-if="isAdmin" class="field">
          <label>任课老师（选填）</label>
          <select v-model="form.teacher_id" class="select">
            <option value="">归到自己名下</option>
            <option v-for="t in teachers" :key="t.id" :value="t.id">{{ t.display_name }}（{{ t.username }}）</option>
          </select>
        </div>
      </div>
      <div class="field">
        <label>默认封面底色（不传图也能有好看封面）</label>
        <div class="cover-colors">
          <button v-for="c in COVER_COLORS" :key="c.id" type="button" class="cover-color"
            :class="{ active: form.cover_color === c.id }" :title="c.name"
            :style="{ background: `linear-gradient(135deg, ${c.from}, ${c.to})` }"
            @click="form.cover_color = c.id"></button>
          <button type="button" class="cover-color auto" :class="{ active: !form.cover_color }" title="按课程自动配色"
            @click="form.cover_color = ''">自动</button>
        </div>
        <div class="cover-preview">
          <CoverArt :course="{ id: 1, title: form.title || '课程标题预览', category: form.category, cover_color: form.cover_color }" :size="34" />
        </div>
      </div>
      <template #foot>
        <button class="btn" @click="showCreate = false">取消</button>
        <button class="btn primary" :disabled="creating" @click="create">{{ creating ? '创建中…' : '创建课程' }}</button>
      </template>
    </Modal>
  </div>
</template>
