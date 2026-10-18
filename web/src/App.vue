<script setup>
import { useRouter } from 'vue-router'
import { store, roleLabel } from './store'
import { api } from './api'

const router = useRouter()

async function logout() {
  try { await api('/api/logout', { method: 'POST' }) } catch {}
  store.user = null
  router.push('/login')
}
</script>

<template>
  <div class="app">
    <nav class="nav">
      <div class="nav-inner">
        <RouterLink to="/" class="brand">📚 CourseHub</RouterLink>
        <div class="nav-links">
          <RouterLink to="/">课程</RouterLink>
          <RouterLink v-if="store.user && store.user.role !== 'student'" to="/upload">上传</RouterLink>
          <RouterLink to="/chat">聊天</RouterLink>
          <RouterLink v-if="store.user && store.user.role === 'admin'" to="/users">用户管理</RouterLink>
        </div>
        <div class="nav-user" v-if="store.user">
          <span class="role-badge" :class="store.user.role">{{ roleLabel(store.user.role) }}</span>
          <span>{{ store.user.display_name }}</span>
          <a href="#" class="muted" @click.prevent="logout">退出</a>
        </div>
      </div>
    </nav>
    <main class="main">
      <RouterView />
    </main>
  </div>
</template>
