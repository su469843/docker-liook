<script setup>
import { computed } from 'vue'
import { stickerById } from '../stickers'

const props = defineProps({
  id: { type: String, required: true },
  size: { type: [Number, String], default: 32 }
})

const sticker = computed(() => stickerById(props.id))
</script>

<template>
  <svg
    v-if="sticker"
    class="sticker-ico"
    :width="size"
    :height="size"
    viewBox="0 0 48 48"
    fill="none"
    aria-hidden="true"
    v-html="sticker.body"
  ></svg>
</template>
