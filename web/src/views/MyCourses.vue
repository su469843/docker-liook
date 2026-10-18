<script setup>
import { onMounted, ref } from 'vue'
import { api } from '../api'
import { toastOk, toastErr } from '../ui'
import CourseCard from '../components/CourseCard.vue'
import Icon from '../components/Icon.vue'

const courses = ref([])
const loading = ref(true)
const code = ref('')
const joining = ref(false)

async function load() {
  loading.value = true
  try {
    courses.value = await api('/api/courses')
  } catch (e) {
    toastErr(e)
  }
  loading.value = false
}
onMounted(load)

async function joinByCode() {
  const c = code.value.trim().toUpperCase()
  if (!c) return
  joining.value = true
  try {
    const course = await api('/api/join', { method: 'POST', json: { code: c } })
    toastOk(`已加入《${course.title}》`)
    code.value = ''
    await load()
  } catch (e) {
    toastErr(e)
  } finally {
    joining.value = false
  }
}
</script>

<template>
  <div class="container">
    <div class="page-head">
      <div>
        <h1 class="page-title">我的课程</h1>
        <div class="page-sub">共 {{ courses.length }} 门课程</div>
      </div>
      <div class="row">
        <input v-model="code" class="input" style="width:230px" placeholder="输入老师给的邀请码"
          @keyup.enter="joinByCode" />
        <button class="btn primary" :disabled="joining" @click="joinByCode">加入课程</button>
      </div>
    </div>

    <div v-if="loading" class="course-grid">
      <div v-for="i in 4" :key="i" class="skeleton" style="height:240px"></div>
    </div>
    <div v-else-if="courses.length" class="course-grid">
      <CourseCard v-for="c in courses" :key="c.id" :course="c" />
    </div>
    <div v-else class="empty">
      <span class="empty-ico"><Icon name="plant" :size="44" /></span>
      还没有课程，去<a href="/" style="color:var(--brand-700)">课程市场</a>逛逛，或输入老师给的邀请码加入。
    </div>
  </div>
</template>
