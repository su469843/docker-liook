import { reactive } from 'vue'
import { api } from './api'

// 全局用户状态（单一数据源，路由守卫与导航栏共用）
export const store = reactive({
  user: null,
  loaded: false,
  config: { site_name: '课程中心', register_mode: 'open', allow_register: true, need_invite: false },
  online: [],
  unreadPrivate: 0,
  unreadGroup: 0
})

export async function loadMe() {
  try {
    store.user = await api('/api/me')
  } catch {
    store.user = null
  }
  store.loaded = true
}

export async function loadConfig() {
  try {
    store.config = await api('/api/config')
    if (store.config?.site_name) document.title = store.config.site_name
  } catch {}
}

export async function refreshUnread() {
  if (!store.user) return
  try {
    const [contacts, groups] = await Promise.all([api('/api/contacts'), api('/api/groups')])
    store.unreadPrivate = contacts.reduce((n, c) => n + (c.unread || 0), 0)
    store.unreadGroup = groups.reduce((n, g) => n + (g.unread || 0), 0)
  } catch {}
}

export function roleLabel(role) {
  return { admin: '管理员', teacher: '老师', student: '学员' }[role] || role
}

export function homeFor(role) {
  if (role === 'admin') return '/admin'
  if (role === 'teacher') return '/studio'
  return '/'
}

export function isOnline(id) {
  return store.online.includes(id)
}
