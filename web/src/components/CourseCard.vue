<script setup>
import { computed } from 'vue'
import { courseArt, coverGradient } from '../format'
import { store } from '../store'
import Icon from './Icon.vue'

const props = defineProps({
  course: { type: Object, required: true },
  showBuy: { type: Boolean, default: false },
  joinedLabel: { type: String, default: '已加入' }
})

const coverUrl = computed(() => {
  if (!props.course.cover) return ''
  return `/api/courses/${props.course.id}/cover?v=${encodeURIComponent(props.course.updated_at || props.course.id)}`
})
const isStudent = computed(() => store.user?.role === 'student')
const art = computed(() => courseArt(props.course.id))
</script>

<template>
  <RouterLink :to="'/course/' + course.id" class="course-card card-hover">
    <div class="course-cover" :style="!coverUrl ? { background: coverGradient(course.id) } : null">
      <img v-if="coverUrl" :src="coverUrl" :alt="course.title" loading="lazy" />
      <div v-else class="cover-fallback"><Icon :name="art" :size="62" /></div>
      <span v-if="course.enrolled && (isStudent || showBuy)" class="tag joined">{{ joinedLabel }}</span>
      <span v-else-if="course.published" class="tag">市场公开</span>
    </div>
    <div class="course-body">
      <div class="course-title">{{ course.title }}</div>
      <div class="course-desc">{{ course.description || '暂无课程简介' }}</div>
      <div class="course-foot">
        <span class="course-teacher">
          <span class="avatar sm teacher">{{ (course.teacher_name || '?')[0] }}</span>
          {{ course.teacher_name || '未知老师' }}
        </span>
        <span>{{ course.lesson_count }} 课时 · {{ course.student_count }} 人</span>
      </div>
    </div>
  </RouterLink>
</template>
