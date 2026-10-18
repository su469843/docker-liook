<script setup>
import { ref, onMounted } from 'vue'
import { api } from '../api'
import { roleLabel } from '../store'

const users = ref([])
const err = ref('')
const showCreate = ref(false)
const form = ref({ username: '', password: '', display_name: '', role: 'student' })

async function load() {
  users.value = await api('/api/users')
}
onMounted(load)

async function create() {
  err.value = ''
  try {
    await api('/api/users', { method: 'POST', json: { ...form.value } })
    form.value = { username: '', password: '', display_name: '', role: 'student' }
    showCreate.value = false
    await load()
  } catch (e) {
    err.value = e.message
  }
}

async function toggleStatus(u) {
  const to = u.status === 'active' ? 'disabled' : 'active'
  if (!confirm(`确定${to === 'disabled' ? '禁用' : '启用'}账号「${u.display_name}」？`)) return
  try {
    await api(`/api/users/${u.id}/status`, { method: 'POST', json: { status: to } })
    await load()
  } catch (e) {
    err.value = e.message
  }
}

async function resetPassword(u) {
  const pwd = prompt(`为「${u.display_name}」设置新密码（至少 6 位）：`)
  if (!pwd) return
  try {
    await api(`/api/users/${u.id}/password`, { method: 'POST', json: { password: pwd } })
    alert('密码已重置')
  } catch (e) {
    alert(e.message)
  }
}
</script>

<template>
  <div class="page">
    <div class="page-head">
      <h1>用户管理</h1>
      <button class="btn primary" @click="showCreate = !showCreate">+ 新建账号</button>
    </div>

    <form v-if="showCreate" class="card create-form" @submit.prevent="create">
      <div class="row">
        <input v-model="form.username" placeholder="用户名（3-32 位字母数字下划线）" required />
        <input v-model="form.password" placeholder="初始密码（≥6 位）" required />
      </div>
      <div class="row">
        <input v-model="form.display_name" placeholder="昵称（选填）" />
        <select v-model="form.role">
          <option value="student">学员</option>
          <option value="teacher">老师</option>
          <option value="admin">管理员</option>
        </select>
      </div>
      <div class="row">
        <button class="btn primary" type="submit">创建</button>
        <button class="btn" type="button" @click="showCreate = false">取消</button>
      </div>
      <p v-if="err" class="error">{{ err }}</p>
    </form>

    <div class="card">
      <table class="user-table">
        <thead>
          <tr><th>ID</th><th>用户名</th><th>昵称</th><th>角色</th><th>状态</th><th>注册时间</th><th>操作</th></tr>
        </thead>
        <tbody>
          <tr v-for="u in users" :key="u.id">
            <td>{{ u.id }}</td>
            <td>{{ u.username }}</td>
            <td>{{ u.display_name }}</td>
            <td><span class="role-badge" :class="u.role">{{ roleLabel(u.role) }}</span></td>
            <td>{{ u.status === 'active' ? '✅ 正常' : '⛔ 已禁用' }}</td>
            <td class="muted">{{ u.created_at?.slice(0, 10) }}</td>
            <td class="row">
              <button class="btn small" @click="resetPassword(u)">改密</button>
              <button v-if="u.id !== 1" class="btn small" :class="{ danger: u.status === 'active' }" @click="toggleStatus(u)">
                {{ u.status === 'active' ? '禁用' : '启用' }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
