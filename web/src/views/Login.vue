<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api'
import { store, loadMe } from '../store'

const router = useRouter()
const mode = ref('login') // login | register
const username = ref('')
const password = ref('')
const displayName = ref('')
const err = ref('')
const loading = ref(false)
const allowRegister = ref(true)

onMounted(async () => {
  try {
    const cfg = await api('/api/config')
    allowRegister.value = cfg.allow_register !== false
  } catch {}
})

async function submit() {
  err.value = ''
  loading.value = true
  try {
    if (mode.value === 'login') {
      await api('/api/login', { method: 'POST', json: { username: username.value.trim(), password: password.value } })
    } else {
      await api('/api/register', {
        method: 'POST',
        json: { username: username.value.trim(), password: password.value, display_name: displayName.value.trim() }
      })
    }
    await loadMe()
    router.push('/')
  } catch (e) {
    err.value = e.message
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="login-wrap">
    <form class="card login-card" @submit.prevent="submit">
      <h2>📚 CourseHub</h2>
      <div class="login-tabs">
        <button type="button" :class="{ active: mode === 'login' }" @click="mode = 'login'">登录</button>
        <button v-if="allowRegister" type="button" :class="{ active: mode === 'register' }" @click="mode = 'register'">注册</button>
      </div>

      <input v-model="username" placeholder="用户名" required autofocus autocomplete="username" />
      <input v-if="mode === 'register'" v-model="displayName" placeholder="昵称（选填）" />
      <input type="password" v-model="password" placeholder="密码" required
        :autocomplete="mode === 'login' ? 'current-password' : 'new-password'" />

      <button class="btn primary block" :disabled="loading">
        {{ loading ? '请稍候…' : (mode === 'login' ? '登录' : '注册学员账号') }}
      </button>
      <p v-if="mode === 'register'" class="muted small-note">注册默认为学员账号，老师账号请联系管理员开通</p>
      <p v-if="err" class="error">{{ err }}</p>
    </form>
  </div>
</template>
