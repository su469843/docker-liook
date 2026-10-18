<script setup>
import { onBeforeUnmount, onMounted } from 'vue'

const props = defineProps({
  title: { type: String, default: '' },
  wide: Boolean,
  closeOnMask: { type: Boolean, default: true }
})
const emit = defineEmits(['close'])

function onKey(e) {
  if (e.key === 'Escape') emit('close')
}
onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))
</script>

<template>
  <Teleport to="body">
    <div class="modal-mask" @click.self="closeOnMask && emit('close')">
      <div class="modal-card" :class="{ wide }">
        <div class="modal-head">
          <h3><slot name="title">{{ props.title }}</slot></h3>
          <button class="close-x" type="button" @click="emit('close')">×</button>
        </div>
        <div class="modal-body"><slot /></div>
        <div v-if="$slots.foot" class="modal-foot"><slot name="foot" /></div>
      </div>
    </div>
  </Teleport>
</template>
