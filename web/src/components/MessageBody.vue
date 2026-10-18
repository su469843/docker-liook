<script setup>
import { computed } from 'vue'
import { splitStickers } from '../stickers'
import Sticker from './Sticker.vue'

const props = defineProps({
  content: { type: String, default: '' }
})

const parts = computed(() => splitStickers(props.content))
</script>

<template>
  <span class="msg-body">
    <template v-for="(p, i) in parts" :key="i">
      <Sticker v-if="p.sticker" :id="p.sticker" :size="34" />
      <span v-else class="msg-text">{{ p.text }}</span>
    </template>
  </span>
</template>
