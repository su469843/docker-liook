<script setup>
import { computed } from 'vue'
import { coverStyle, courseArt } from '../format'
import Icon from './Icon.vue'

// 默认封面：渐变底色 + 手绘图标 + 课程标题，等于一张"自动生成的封面图"。
// 老师选了颜色就用选的颜色，没选就按课程 ID 稳定派生一个。
const props = defineProps({
  course: { type: Object, required: true },
  size: { type: Number, default: 46 },
  compact: { type: Boolean, default: false },
  showTitle: { type: Boolean, default: true },
})

const style = computed(() => coverStyle(props.course.cover_color || props.course.id))
const art = computed(() => courseArt(props.course.id))
</script>

<template>
  <div class="cover-art" :class="{ compact }" :style="style">
    <span class="ca-ring"></span>
    <span class="ca-blob"></span>
    <span class="ca-ico"><Icon :name="art" :size="size" :stroke="1.5" /></span>
    <span v-if="showTitle" class="ca-title">{{ course.title }}</span>
    <span v-if="course.category && !compact" class="ca-cat">{{ course.category }}</span>
  </div>
</template>
