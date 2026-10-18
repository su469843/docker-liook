<script setup>
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '../api'
import { store } from '../store'
import { fmtDur, kindIconName, kindLabel, fmtDate } from '../format'
import { toastOk, toastErr } from '../ui'
import Modal from '../components/Modal.vue'
import Icon from '../components/Icon.vue'
import CoverArt from '../components/CoverArt.vue'

const route = useRoute()
const router = useRouter()
const course = ref(null)
const lessons = ref([])
const loading = ref(true)
const err = ref('')
const tab = ref('catalog')
const showJoin = ref(false)
const code = ref('')
const joining = ref(false)

const canManage = computed(() => !!course.value?.can_manage)
const enrolled = computed(() => !!course.value?.enrolled)
const isStudent = computed(() => store.user?.role === 'student')
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
    course.value = await api(`/api/courses/${route.params.id}`)
    lessons.value = await api(`/api/courses/${route.params.id}/lessons`)
  } catch (e) {
    err.value = e.message
  } finally {
    loading.value = false
  }
}
onMounted(load)

function firstLesson() {
  return lessons.value.find((l) => l.status === 'ready') || lessons.value[0] || null
}

function startLearning() {
  const l = firstLesson()
  if (!l) { toastErr('老师还没有上传课时，敬请期待'); return }
  router.push(`/learn/${course.value.id}/${l.id}`)
}

function openLesson(l) {
  if (enrolled.value || canManage.value) {
    router.push(`/learn/${course.value.id}/${l.id}`)
  } else {
    showJoin.value = true
  }
}

async function enrollMarket() {
  try {
    await api(`/api/market/${course.value.id}/enroll`, { method: 'POST' })
    toastOk('已加入课程')
    await load()
    startLearning()
  } catch (e) {
    toastErr(e)
  }
}

async function joinByCode() {
  const c = code.value.trim().toUpperCase()
  if (!c) return
  joining.value = true
  try {
    await api('/api/join', { method: 'POST', json: { code: c } })
    showJoin.value = false
    code.value = ''
    toastOk('已加入课程')
    await load()
  } catch (e) {
    toastErr(e)
  } finally {
    joining.value = false
  }
}

function copyCode() {
  if (!course.value?.invite_code) return
  navigator.clipboard?.writeText(course.value.invite_code)
  toastOk('邀请码已复制，发给学员即可加入')
}

async function togglePublish() {
  const to = !course.value.published
  try {
    await api(`/api/courses/${course.value.id}/publish`, { method: 'POST', json: { published: to } })
    course.value.published = to
    toastOk(to ? '已上架到课程市场' : '已从市场下架')
  } catch (e) {
    toastErr(e)
  }
}
</script>

<template>
  <div class="container">
    <div v-if="loading" class="card"><div class="skeleton" style="height:220px"></div></div>
    <div v-else-if="err" class="empty">
      <span class="empty-ico"><Icon name="lock" :size="44" /></span>{{ err }}
      <div class="mt"><RouterLink to="/" class="btn">回到首页</RouterLink></div>
    </div>

    <template v-else-if="course">
      <div class="detail-hero">
        <div class="detail-cover">
          <img v-if="coverUrl" :src="coverUrl" :alt="course.title" />
          <CoverArt v-else :course="course" :size="70" />
        </div>
        <div class="detail-info">
          <div class="row">
            <span v-if="course.category" class="pill blue">{{ course.category }}</span>
            <span v-if="course.published" class="pill green">市场上架中</span>
            <span v-else-if="canManage" class="pill gray">未上架</span>
            <span v-if="enrolled" class="pill green">已加入</span>
          </div>
          <h1 class="detail-title">{{ course.title }}</h1>
          <div class="detail-meta">
            <span class="row" style="gap:6px">
              <span class="avatar sm teacher">{{ (course.teacher_name || '?')[0] }}</span>
              {{ course.teacher_name }}
            </span>
            <span>共 {{ course.lesson_count }} 课时</span>
            <span>{{ course.student_count }} 人在学</span>
            <span>创建于 {{ fmtDate(course.created_at) }}</span>
          </div>
          <p class="detail-desc">{{ course.description || '老师还没有填写课程介绍。' }}</p>
          <div class="row">
            <template v-if="canManage">
              <RouterLink :to="'/studio/' + course.id" class="btn primary">管理课程内容</RouterLink>
              <button class="btn" @click="togglePublish">{{ course.published ? '下架市场' : '上架到市场' }}</button>
              <button class="btn" @click="startLearning">预览学习页</button>
            </template>
            <template v-else>
              <button class="btn primary lg" @click="enrolled ? startLearning() : (course.published ? enrollMarket() : (showJoin = true))">
                {{ enrolled ? '继续学习' : (course.published ? '加入学习' : '输入邀请码加入') }}
              </button>
              <button v-if="!enrolled" class="btn lg" @click="showJoin = true">我有邀请码</button>
            </template>
          </div>
        </div>
      </div>

      <div class="detail-layout">
        <div class="card">
          <div class="tabs">
            <button class="tab" :class="{ active: tab === 'catalog' }" @click="tab = 'catalog'">
              课程目录<span v-if="lessons.length" class="muted small">（{{ lessons.length }}）</span>
            </button>
            <button class="tab" :class="{ active: tab === 'intro' }" @click="tab = 'intro'">课程介绍</button>
          </div>

          <div v-if="tab === 'catalog'">
            <div v-if="lessons.length">
              <div v-for="(g, gi) in groups" :key="gi" class="chapter-block">
                <div v-if="g.chapter" class="chapter-name"><Icon name="folder" :size="15" /> {{ g.chapter }}</div>
                <div v-for="l in g.items" :key="l.id" class="lesson-row" @click="openLesson(l)">
                  <span class="lesson-modal"><Icon :name="kindIconName(l.kind)" :size="16" /></span>
                  <div class="lesson-main">
                    <div class="lesson-title">{{ l.title }}</div>
                    <div class="lesson-sub">
                      <span>{{ kindLabel(l.kind) }}</span>
                      <span v-if="l.duration">时长 {{ fmtDur(l.duration) }}</span>
                      <span v-if="l.status !== 'ready'" style="color:var(--warn)">上传中</span>
                      <span v-if="!enrolled && !canManage" class="row" style="gap:4px;color:var(--text-3)"><Icon name="lock" :size="13" /> 加入后可学习</span>
                    </div>
                  </div>
                </div>
              </div>
            </div>
            <div v-else class="empty">
              <span class="empty-ico"><Icon name="video-file" :size="44" /></span>老师还没有添加课时
            </div>
          </div>

          <div v-else class="rich-view">
            <p style="white-space:pre-wrap">{{ course.description || '暂无课程介绍' }}</p>
          </div>
        </div>

        <aside class="side-card">
          <div class="card">
            <div class="teacher-box">
              <span class="avatar teacher" style="width:44px;height:44px;font-size:18px">{{ (course.teacher_name || '?')[0] }}</span>
              <div>
                <div style="font-weight:600">{{ course.teacher_name }}</div>
                <div class="muted small">任课老师</div>
              </div>
              <span class="spacer" style="flex:1"></span>
              <RouterLink v-if="!canManage && enrolled" to="/messages" class="btn-link">私聊</RouterLink>
            </div>
            <div class="row" style="justify-content:space-between;margin-top:14px">
              <span class="muted small">课时</span><b>{{ course.lesson_count }}</b>
            </div>
            <div class="row" style="justify-content:space-between;margin-top:6px">
              <span class="muted small">学员</span><b>{{ course.student_count }}</b>
            </div>
            <div class="divider"></div>
            <template v-if="canManage">
              <div v-if="course.invite_code" class="invite-box">
                <div>
                  <div class="muted small">课程邀请码（点击复制）</div>
                  <div class="invite-code" @click="copyCode">{{ course.invite_code }}</div>
                </div>
                <span style="cursor:pointer;display:inline-flex" @click="copyCode"><Icon name="copy" :size="16" /></span>
              </div>
              <RouterLink :to="'/studio/' + course.id" class="btn primary block mt">进入课程管理</RouterLink>
            </template>
            <template v-else-if="!enrolled">
              <button class="btn primary block" @click="course.published ? enrollMarket() : (showJoin = true)">
                {{ course.published ? '免费加入学习' : '输入邀请码加入' }}
              </button>
            </template>
            <template v-else>
              <button class="btn primary block" @click="startLearning">继续学习</button>
            </template>
          </div>
        </aside>
      </div>
    </template>

    <Modal v-if="showJoin" title="输入课程邀请码" @close="showJoin = false">
      <p class="muted small" style="margin:0 0 14px">邀请码由老师在课程设置里生成，通常是一串 8 位字母数字。</p>
      <input v-model="code" class="input" placeholder="例如 ABCDEFGH" style="text-transform:uppercase"
        @keyup.enter="joinByCode" />
      <template #foot>
        <button class="btn" @click="showJoin = false">取消</button>
        <button class="btn primary" :disabled="joining" @click="joinByCode">加入课程</button>
      </template>
    </Modal>
  </div>
</template>
