<script setup>
import { confirmState, resolveConfirm } from '../ui'
import Modal from './Modal.vue'
</script>

<template>
  <Modal v-if="confirmState.open" :title="confirmState.title" @close="resolveConfirm(false)">
    <p style="margin:0;line-height:1.8">{{ confirmState.message }}</p>
    <template #foot>
      <button class="btn" @click="resolveConfirm(false)">取消</button>
      <button class="btn" :class="confirmState.danger ? 'danger' : 'primary'" @click="resolveConfirm(true)">
        {{ confirmState.okText }}
      </button>
    </template>
  </Modal>
</template>
