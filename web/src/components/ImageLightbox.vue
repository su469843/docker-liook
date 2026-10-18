<script setup>
import { onBeforeUnmount, onMounted } from 'vue'
import Icon from './Icon.vue'

const props = defineProps({
  src: { type: String, default: '' },
  alt: { type: String, default: '图片' },
})
const emit = defineEmits(['close'])

function onKey(e) {
  if (e.key === 'Escape') emit('close')
}
onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))
</script>

<template>
  <div class="lightbox" @click.self="emit('close')">
    <button class="lightbox-close" type="button" title="关闭（Esc）" @click="emit('close')">
      <Icon name="close" :size="20" />
    </button>
    <img :src="src" :alt="alt" />
    <div class="lightbox-tip">点击空白处或按 Esc 关闭</div>
  </div>
</template>
