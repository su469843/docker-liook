<script setup>
import { toasts } from '../ui'
</script>

<template>
  <Teleport to="body">
    <div class="toast-wrap">
      <div v-for="t in toasts" :key="t.id" class="toast" :class="t.type">{{ t.text }}</div>
    </div>
  </Teleport>
</template>
