<script setup>
import { ref, onMounted, computed } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api'
import { store } from '../store'

const router = useRouter()
const courses = ref([])
const users = ref([])
const title = ref('')
const desc = ref('')
const teacherId = ref('')
const code = ref('')
const showCreate = ref(false)
const err = ref('')
const joinMsg = ref('')

const role = computed(() => store.user?.role)
const isStudent = computed(() => role.value === 'student')
const teachers = computed(() => users.value.filter((u) => u.role === 'teacher'))

async function load() {
  courses.value = await api('/api/courses')
  if (role.value === 'admin') {
    users.value = await api('/api/users')
  }
}
onMounted(load)

async function create() {
  err.value = ''
  if (!title.value.trim()) return
  try {
    const body = { title: title.value.trim(), description: desc.value.trim() }
    if (role.value === 'admin' && teacherId.value) body.teacher_id = Number(teacherId.value)
    await api('/api/courses', { method: 'POST', json: body })
    title.value = ''
    desc.value = ''
    teacherId.value = ''
    showCreate.value = false
    await load()
  } catch (e) {
    err.value = e.message
  }
}

async function joinByCode() {
  err.value = ''
  joinMsg.value = ''
  if (!code.value.trim()) return
  try {
    const c = await api('/api/join', { method: 'POST', json: { code: code.value.trim() } })
    joinMsg.value = `✅ 已加入课程「${c.title}」`
    code.value = ''
    await load()
  } catch (e) {
    err.value = e.message
  }
}

async function remove(c) {
  if (!confirm(`删除课程「${c.title}」及其全部视频？此操作不可恢复。`)) return
  await api('/api/courses/' + c.id, { method: 'DELETE' })
  await load()
}

async function assignTeacher(c, event) {
  const tid = Number(event.target.value)
  if (!tid) return
  try {
    await api(`/api/courses/${c.id}/teacher`, { method: 'POST', json: { teacher_id: tid } })
    await load()
  } catch (e) {
    alert(e.message)
    event.target.value = c.teacher_id
  }
}

async function regenerateCode(c) {
  if (!confirm(`重新生成「${c.title}」的邀请码？旧邀请码将失效。`)) return
  const res = await api(`/api/courses/${c.id}/invite`, { method: 'POST' })
  c.invite_code = res.invite_code
}

function copyCode(code) {
  navigator.clipboard?.writeText(code)
}
</script>

<template>
  <div class="page">
    <div class="page-head">
      <h1>{{ isStudent ? '我的课程' : '课程管理' }}</h1>
      <button v-if="!isStudent" class="btn primary" @click="showCreate = !showCreate">+ 新建课程</button>
    </div>

    <!-- 学员：邀请码加入 -->
    <form v-if="isStudent" class="card join-form" @submit.prevent="joinByCode">
      <input v-model="code" placeholder="输入老师提供的邀请码加入课程" />
      <button class="btn primary" type="submit">加入课程</button>
      <span v-if="joinMsg" class="muted">{{ joinMsg }}</span>
    </form>

    <!-- 老师/管理员：新建课程 -->
    <form v-if="showCreate && !isStudent" class="card create-form" @submit.prevent="create">
      <input v-model="title" placeholder="课程名称（必填）" required />
      <input v-model="desc" placeholder="课程简介（选填）" />
      <select v-if="role === 'admin'" v-model="teacherId">
        <option value="">任课老师（不选则归自己）</option>
        <option v-for="t in teachers" :key="t.id" :value="t.id">{{ t.display_name }}（{{ t.username }}）</option>
      </select>
      <div class="row">
        <button class="btn primary" type="submit">创建</button>
        <button class="btn" type="button" @click="showCreate = false">取消</button>
      </div>
      <p v-if="err" class="error">{{ err }}</p>
    </form>

    <div v-if="courses.length" class="grid">
      <div v-for="c in courses" :key="c.id" class="card course-card" @click="router.push('/course/' + c.id)">
        <h3>{{ c.title }}</h3>
        <p class="muted clamp">{{ c.description || '暂无简介' }}</p>
        <p class="muted small-note" v-if="!isStudent">👨‍🏫 {{ c.teacher_name }} · {{ c.student_count }} 名学员</p>
        <p class="muted small-note" v-else>👨‍🏫 {{ c.teacher_name }}</p>
        <div class="course-foot">
          <span class="muted">{{ c.video_count }} 个视频</span>
          <div class="row" @click.stop>
            <button v-if="!isStudent" class="btn small" @click="router.push('/upload?course=' + c.id)">上传视频</button>
            <button class="btn small danger" @click="remove(c)">删除</button>
          </div>
        </div>
        <div v-if="!isStudent && c.invite_code" class="invite-row" @click.stop>
          <span class="invite-code" @click="copyCode(c.invite_code)" title="点击复制">邀请码 <b>{{ c.invite_code }}</b> 📋</span>
          <a href="#" class="muted small-note" @click.prevent="regenerateCode(c)">换一个</a>
        </div>
        <div v-if="role === 'admin'" class="invite-row" @click.stop>
          <select :value="c.teacher_id" @change="assignTeacher(c, $event)" class="small-select">
            <option v-for="t in teachers" :key="t.id" :value="t.id">👨‍🏫 {{ t.display_name }}</option>
            <option :value="c.teacher_id">当前：{{ c.teacher_name }}</option>
          </select>
        </div>
      </div>
    </div>
    <p v-else class="muted center">
      {{ isStudent ? '还没有课程，用老师给的邀请码加入 →' : '点右上角「新建课程」开始 →' }}
    </p>
  </div>
</template>
