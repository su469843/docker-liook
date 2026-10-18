<script setup>
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api'
import { store } from '../store'
import { toastOk, toastErr } from '../ui'
import CourseCard from '../components/CourseCard.vue'
import Icon from '../components/Icon.vue'

const router = useRouter()
const market = ref([])
const mine = ref([])
const loading = ref(true)
const category = ref('')
const code = ref('')
const joining = ref(false)

const isStudent = computed(() => store.user?.role === 'student')
const isStaff = computed(() => ['teacher', 'admin'].includes(store.user?.role))
const categories = computed(() => {
  const set = new Set()
  market.value.forEach((c) => { if (c.category) set.add(c.category) })
  return Array.from(set)
})
const filtered = computed(() => (category.value ? market.value.filter((c) => c.category === category.value) : market.value))
const totalLessons = computed(() => mine.value.reduce((n, c) => n + (c.lesson_count || 0), 0))

async function load() {
  loading.value = true
  try {
    market.value = await api('/api/market')
    if (isStudent.value) mine.value = await api('/api/courses')
    else mine.value = await api('/api/courses')
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
    <section class="hero">
      <h1>{{ isStudent ? '发现好课，随时开始学习' : '把课程放到市场上，让学员自己找上门' }}</h1>
      <p>
        {{ isStudent
          ? '浏览市场里的公开课程，一键加入；也可以输入老师给你的邀请码直接听课。'
          : '在老师工作台创建课程、上传视频/图文/文档，上架后学员即可在市场中自助加入。' }}
      </p>
      <div class="hero-actions">
        <template v-if="isStudent">
          <a class="btn solid" href="#market">浏览课程市场</a>
          <RouterLink v-if="mine.length" to="/my" class="btn">继续学习</RouterLink>
        </template>
        <template v-else>
          <RouterLink to="/studio" class="btn solid">进入老师工作台</RouterLink>
          <RouterLink v-if="store.user?.role === 'admin'" to="/admin" class="btn">管理后台</RouterLink>
        </template>
      </div>
      <div class="hero-stat">
        <div><b>{{ market.length }}</b><span>市场公开课程</span></div>
        <div><b>{{ mine.length }}</b><span>{{ isStudent ? '我加入的课程' : '我管理的课程' }}</span></div>
        <div><b>{{ totalLessons }}</b><span>总课时</span></div>
      </div>
    </section>

    <template v-if="isStudent">
      <div class="section-head">
        <h2><Icon name="book" :size="20" /> 我的课程</h2>
        <div class="row">
          <input v-model="code" class="input" style="width:210px" placeholder="输入邀请码加入课程"
            @keyup.enter="joinByCode" />
          <button class="btn" :disabled="joining" @click="joinByCode">加入</button>
        </div>
      </div>
      <div v-if="mine.length" class="course-grid">
        <CourseCard v-for="c in mine" :key="c.id" :course="c" />
      </div>
      <div v-else class="empty">
        <span class="empty-ico"><Icon name="plant" :size="44" /></span>
        还没有课程。用老师给的邀请码加入，或者到下面市场里挑一门感兴趣的课程。
      </div>
    </template>

    <div id="market" class="section-head">
      <h2><Icon name="grid" :size="20" /> 课程市场</h2>
      <div class="chips">
        <span class="chip" :class="{ active: category === '' }" @click="category = ''">全部</span>
        <span v-for="c in categories" :key="c" class="chip" :class="{ active: category === c }" @click="category = c">{{ c }}</span>
      </div>
    </div>

    <div v-if="loading" class="course-grid">
      <div v-for="i in 6" :key="i" class="card" style="padding:0;overflow:hidden">
        <div class="skeleton" style="aspect-ratio:16/9"></div>
        <div style="padding:14px"><div class="skeleton" style="height:18px;width:80%"></div></div>
      </div>
    </div>
    <div v-else-if="filtered.length" class="course-grid">
      <CourseCard v-for="c in filtered" :key="c.id" :course="c" />
    </div>
    <div v-else class="empty">
      <span class="empty-ico"><Icon name="search" :size="44" /></span>
      市场里还没有公开课程。老师可以在工作台把课程「上架到市场」。
    </div>
  </div>
</template>
