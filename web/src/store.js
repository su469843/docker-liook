import { reactive } from 'vue'
import { api } from './api'

// 全局用户状态（单一数据源，路由守卫与导航栏共用）
export const store = reactive({
  user: null,
  loaded: false
})

export async function loadMe() {
  try {
    store.user = await api('/api/me')
  } catch {
    store.user = null
  }
  store.loaded = true
}

export function roleLabel(role) {
  return { admin: '管理员', teacher: '老师', student: '学员' }[role] || role
}
