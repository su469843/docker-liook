import { reactive } from 'vue'
import { api } from './api'

// 全局用户状态（单一数据源，路由守卫与导航栏共用）
export const store = reactive({
  user: null,
  loaded: false,
  config: { site_name: '课程中心', register_mode: 'open', allow_register: true, need_invite: false, version: '', commit: '' },
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

// 标记私聊已读：先让后端落库，再刷新角标（不能用 WS 发读包再立刻拉接口，会抢时序导致红点不灭）
export async function markChatRead(peerId) {
  if (!peerId) return
  try {
    await api(`/api/chat/${peerId}/read`, { method: 'POST' })
  } catch {}
  await refreshUnread()
}

// 标记课程群已读（last_id=0 表示读到最新一条）
export async function markGroupRead(courseId) {
  if (!courseId) return
  try {
    await api(`/api/groups/${courseId}/read`, { method: 'POST', json: { last_id: 0 } })
  } catch {}
  await refreshUnread()
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
