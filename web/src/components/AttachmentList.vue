<script setup>
// AttachmentList：作业附件展示（照片带批注图层 / 语音播放）。
// 学员端只读；老师端每张照片右下角有「批注」按钮，点开 AnnotationEditor。
import { reactive, ref } from 'vue'
import { fmtBytes } from '../files'
import { fmtDur } from '../format'
import Icon from './Icon.vue'
import AnnotationLayer from './AnnotationLayer.vue'
import ImageLightbox from './ImageLightbox.vue'

const props = defineProps({
  attachments: { type: Array, default: () => [] },
  annotations: { type: Object, default: () => ({}) },
  editable: { type: Boolean, default: false }
})
const emit = defineEmits(['annotate'])

const sizes = reactive({})
const lightbox = ref(null)

function fileUrl(att) {
  return '/api/practice-files/' + att.key
}

function onImgLoad(att, e) {
  const img = e.target
  if (img?.naturalWidth) sizes[att.key] = { w: img.naturalWidth, h: img.naturalHeight }
}

function shapesOf(att) {
  return (props.annotations && props.annotations[att.key]) || []
}

function preview(att) {
  lightbox.value = { src: fileUrl(att), alt: att.name || '作业照片' }
}
</script>

<template>
  <div v-if="attachments && attachments.length" class="att-list">
    <div v-for="att in attachments" :key="att.key" class="att-item">
      <template v-if="att.kind === 'image'">
        <div class="att-image" @click="preview(att)">
          <img :src="fileUrl(att)" :alt="att.name || '作业照片'" loading="lazy" @load="onImgLoad(att, $event)" />
          <AnnotationLayer v-if="sizes[att.key]" :shapes="shapesOf(att)" :width="sizes[att.key].w" :height="sizes[att.key].h" />
          <span v-if="shapesOf(att).length" class="att-badge">已批注 {{ shapesOf(att).length }} 处</span>
        </div>
        <div class="row" style="justify-content:space-between;gap:6px;margin-top:4px">
          <span class="muted small att-name">{{ att.name || '照片' }} · {{ fmtBytes(att.size) }}</span>
          <button v-if="editable" class="btn small ghost" @click="emit('annotate', att)">
            <Icon name="edit" :size="13" /> 批注
          </button>
        </div>
      </template>
      <template v-else>
        <div class="att-audio">
          <div class="row" style="gap:8px;margin-bottom:6px">
            <Icon name="mic" :size="16" />
            <b class="small">{{ att.name || '语音作业' }}</b>
            <span v-if="att.duration" class="muted small">{{ fmtDur(att.duration) }}</span>
            <span class="muted small">{{ fmtBytes(att.size) }}</span>
          </div>
          <audio :src="fileUrl(att)" controls preload="metadata" style="width:100%"></audio>
        </div>
      </template>
    </div>
    <ImageLightbox v-if="lightbox" :src="lightbox.src" :alt="lightbox.alt" @close="lightbox = null" />
  </div>
</template>
