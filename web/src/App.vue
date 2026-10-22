<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from './api'
import { store, loadConfig, refreshUnread, roleLabel } from './store'
import { connectWS, disconnectWS, onWS } from './ws'
import ToastHost from './components/ToastHost.vue'
import ConfirmHost from './components/ConfirmHost.vue'
import Icon from './components/Icon.vue'

const route = useRoute()
const router = useRouter()
const menuOpen = ref(false)
let off = null

const isLogged = computed(() => !!store.user)
const isStaff = computed(() => ['teacher', 'admin'].includes(store.user?.role))
const isAdmin = computed(() => store.user?.role === 'admin')
const unread = computed(() => (store.unreadPrivate || 0) + (store.unreadGroup || 0))
const hideChrome = computed(() => route.meta.chrome === false)
const siteName = computed(() => store.config?.site_name || '课程中心')

function bindWS() {
  if (!store.user) return
  connectWS()
  refreshUnread()
}

watch(() => store.user, (u) => {
  if (u) bindWS()
  else { disconnectWS(); store.unreadPrivate = 0; store.unreadGroup = 0 }
})

onMounted(async () => {
  await loadConfig()
  document.title = siteName.value
  if (store.user) bindWS()
  off = onWS((m) => {
    if (m.type === 'presence') store.online = m.ids || []
    else if (m.type === 'chat.new' || m.type === 'group.new' || m.type === 'unread') refreshUnread()
  })
  window.addEventListener('click', closeMenu)
})

onBeforeUnmount(() => {
  if (off) off()
  window.removeEventListener('click', closeMenu)
  disconnectWS()
})

function closeMenu() { menuOpen.value = false }

function toggleMenu(e) {
  e.stopPropagation()
  menuOpen.value = !menuOpen.value
}

async function logout() {
  menuOpen.value = false
  try { await api('/api/logout', { method: 'POST' }) } catch {}
  store.user = null
  disconnectWS()
  router.push('/login')
}
</script>

<template>
  <div class="app">
    <header v-if="isLogged && !hideChrome" class="app-header">
      <div class="header-inner">
        <RouterLink to="/" class="brand">
          <span class="brand-logo"><Icon name="logo" :size="20" /></span>
          <span>{{ siteName }}</span>
        </RouterLink>

        <nav class="nav-links">
          <RouterLink to="/" class="nav-link" :class="{ active: route.path === '/' || route.path.startsWith('/course/') }">课程市场</RouterLink>
          <RouterLink v-if="store.user?.role === 'student'" to="/my" class="nav-link">我的课程</RouterLink>
          <RouterLink to="/practice" class="nav-link">练习中心</RouterLink>
          <RouterLink v-if="isStaff" to="/studio" class="nav-link">老师工作台</RouterLink>
          <RouterLink v-if="isAdmin" to="/admin" class="nav-link">管理后台</RouterLink>
          <RouterLink to="/messages" class="nav-link">消息</RouterLink>
        </nav>

        <div class="header-right">
          <RouterLink to="/messages" class="icon-btn" title="消息">
            <Icon name="chat" :size="21" />
            <span v-if="unread" class="badge-dot">{{ unread > 99 ? '99+' : unread }}</span>
          </RouterLink>
          <div class="dropdown">
            <div class="user-chip" @click="toggleMenu">
              <span class="avatar" :class="store.user?.role">{{ (store.user?.display_name || '?')[0] }}</span>
              <span class="user-name">{{ store.user?.display_name }}</span>
              <span class="role-tag" :class="store.user?.role">{{ roleLabel(store.user?.role) }}</span>
            </div>
            <div v-if="menuOpen" class="dropdown-menu" @click.stop>
              <RouterLink to="/messages" @click="closeMenu"><Icon name="chat" :size="17" /> 我的消息</RouterLink>
              <RouterLink to="/practice" @click="closeMenu"><Icon name="target" :size="17" /> 练习中心</RouterLink>
              <RouterLink v-if="isStaff" to="/studio" @click="closeMenu"><Icon name="class" :size="17" /> 老师工作台</RouterLink>
              <RouterLink v-if="isAdmin" to="/admin" @click="closeMenu"><Icon name="settings" :size="17" /> 管理后台</RouterLink>
              <div class="divider"></div>
              <button type="button" @click="logout"><Icon name="logout" :size="17" /> 退出登录</button>
            </div>
          </div>
        </div>
      </div>
    </header>

    <main class="main">
      <RouterView />
    </main>

    <ToastHost />
    <ConfirmHost />
  </div>
</template>
