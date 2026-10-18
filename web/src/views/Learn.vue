<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '../api'
import { store } from '../store'
import { fmtDur, fmtSize, kindIconName, kindLabel } from '../format'
import { toastErr } from '../ui'
import VideoPlayer from '../components/VideoPlayer.vue'
import GroupChat from '../components/GroupChat.vue'
import Icon from '../components/Icon.vue'

const route = useRoute()
const router = useRouter()
const course = ref(null)
const lessons = ref([])
const loading = ref(true)
const err = ref('')
const sideTab = ref('catalog')
const player = ref(null)
const currentTime = ref(0)
const startAt = ref(0)

const currentId = computed(() => Number(route.params.lessonId || 0))
const current = computed(() => lessons.value.find((l) => l.id === currentId.value) || null)
const currentIndex = computed(() => lessons.value.findIndex((l) => l.id === currentId.value))
const prev = computed(() => (currentIndex.value > 0 ? lessons.value[currentIndex.value - 1] : null))
const next = computed(() => (currentIndex.value >= 0 && currentIndex.value < lessons.value.length - 1 ? lessons.value[currentIndex.value + 1] : null))
const isPdf = computed(() => {
  const l = current.value
  if (!l) return false
  return (l.mime || '').includes('pdf') || (l.filename || '').toLowerCase().endsWith('.pdf')
})
const fileUrl = computed(() => (current.value ? `/api/lessons/${current.value.id}/file` : ''))

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
  err.value = ''
  try {
    course.value = await api(`/api/courses/${route.params.courseId}`)
    if (!course.value.enrolled && !course.value.can_manage) {
      router.replace(`/course/${course.value.id}`)
      return
    }
    lessons.value = await api(`/api/courses/${course.value.id}/lessons`)
    if (!route.params.lessonId && lessons.value.length) {
      const first = lessons.value.find((l) => l.status === 'ready') || lessons.value[0]
      router.replace({ path: `/learn/${course.value.id}/${first.id}`, query: route.query })
      return
    }
  } catch (e) {
    err.value = e.message
  } finally {
    loading.value = false
  }
}
onMounted(load)

watch(() => route.params.lessonId, () => {
  startAt.value = Number(route.query.t || 0)
  currentTime.value = 0
})

function goLesson(l) {
  if (!l) return
  router.push(`/learn/${course.value.id}/${l.id}`)
}

function onSeek({ lesson_id, time }) {
  if (current.value && lesson_id === current.value.id) {
    player.value?.seek(time)
  } else {
    router.push({ path: `/learn/${course.value.id}/${lesson_id}`, query: { t: String(time || 0) } })
  }
}
</script>

<template>
  <div class="learn-page">
    <div class="learn-top">
      <RouterLink :to="course ? '/course/' + course.id : '/'" class="btn ghost small"><Icon name="arrow-left" :size="16" /> 返回</RouterLink>
      <div class="course-title-mini">{{ course?.title || '加载中…' }}</div>
      <span class="spacer" style="flex:1"></span>
      <div class="row">
        <button class="btn small" :disabled="!prev" @click="goLesson(prev)">上一课</button>
        <button class="btn small primary" :disabled="!next" @click="goLesson(next)">下一课</button>
      </div>
    </div>

    <div v-if="loading" class="container"><div class="skeleton" style="height:320px"></div></div>
    <div v-else-if="err" class="container"><div class="empty">{{ err }}</div></div>

    <div v-else-if="course" class="learn-layout">
      <div class="learn-main">
        <template v-if="current">
          <template v-if="current.kind === 'video'">
            <VideoPlayer v-if="current.status === 'ready'" ref="player" :lesson-id="current.id"
              :start-at="startAt" @time="(t) => (currentTime = t)" @error="toastErr" />
            <div v-else class="learn-content">
              <div class="alert">该视频课时还在上传中，请稍后再来观看。</div>
            </div>
            <div class="learn-content-tab">
              <div class="row" style="justify-content:space-between;align-items:flex-start">
                <div>
                  <h1 style="font-size:18px">{{ current.title }}</h1>
                  <div class="muted small row" style="gap:4px" v-if="current.chapter"><Icon name="folder" :size="14" /> {{ current.chapter }}</div>
                </div>
                <button class="btn small" @click="sideTab = 'discuss'"><Icon name="quote" :size="15" /> 引用进度去讨论</button>
              </div>
              <div class="divider"></div>
              <p class="muted small" style="margin:0">
                提示：播放到不懂的地方，切到右侧「讨论」→「引用当前进度」发出去，
                老师同学点一下引用就能跳到这个时间点。
              </p>
            </div>
          </template>

          <template v-else-if="current.kind === 'rich'">
            <div class="learn-content">
              <h1>{{ current.title }}</h1>
              <div v-if="current.content" class="rich-view" v-html="current.content"></div>
              <div v-else class="empty">老师还没有填写图文内容</div>
            </div>
          </template>

          <template v-else>
            <div class="doc-view">
              <h1 style="font-size:20px">{{ current.title }}</h1>
              <div class="row muted small" style="margin-top:6px">
                <span class="row" style="gap:4px"><Icon name="doc" :size="15" /> {{ current.filename || '文档' }}</span>
                <span>{{ fmtSize(current.size) }}</span>
              </div>
              <div class="divider"></div>
              <iframe v-if="isPdf" class="doc-frame" :src="fileUrl"></iframe>
              <div v-else class="doc-card">
                <div class="card" style="text-align:center;padding:36px">
                  <div style="color:var(--text-3)"><Icon name="doc" :size="50" /></div>
                  <h3 style="margin:12px 0 6px">{{ current.filename || '课程文档' }}</h3>
                  <p class="muted small">该格式无法在线预览，请下载后查看</p>
                  <a class="btn primary" :href="fileUrl"><Icon name="download" :size="16" /> 下载文档</a>
                </div>
              </div>
            </div>
          </template>
        </template>
        <div v-else class="learn-content">
          <div class="empty">该课程还没有课时</div>
        </div>
      </div>

      <aside class="learn-side">
        <div class="learn-side-tabs">
          <button :class="{ active: sideTab === 'catalog' }" @click="sideTab = 'catalog'">课程目录</button>
          <button :class="{ active: sideTab === 'discuss' }" @click="sideTab = 'discuss'">讨论</button>
        </div>

        <div v-if="sideTab === 'catalog'" class="learn-side-body">
          <div v-if="!lessons.length" class="empty" style="padding:24px 12px">还没有课时</div>
          <div v-for="(g, gi) in groups" :key="gi" class="chapter-block">
            <div v-if="g.chapter" class="chapter-name"><Icon name="folder" :size="15" /> {{ g.chapter }}</div>
            <div v-for="l in g.items" :key="l.id" class="lesson-row" :class="{ active: current && current.id === l.id }"
              @click="goLesson(l)">
              <span class="lesson-modal"><Icon :name="kindIconName(l.kind)" :size="16" /></span>
              <div class="lesson-main">
                <div class="lesson-title">{{ l.title }}</div>
                <div class="lesson-sub">
                  <span>{{ kindLabel(l.kind) }}</span>
                  <span v-if="l.duration">{{ fmtDur(l.duration) }}</span>
                  <span v-if="l.status !== 'ready'" style="color:var(--warn)">上传中</span>
                </div>
              </div>
            </div>
          </div>
        </div>

        <div v-else style="flex:1;min-height:0;display:flex;flex-direction:column">
          <GroupChat :course-id="course.id" :course="course"
            :can-quote="current && current.kind === 'video'"
            :lesson="current" :time="currentTime" @seek="onSeek" />
        </div>
      </aside>
    </div>
  </div>
</template>
