import { createRouter, createWebHistory } from 'vue-router'
import Login from './views/Login.vue'
import Courses from './views/Courses.vue'
import CourseDetail from './views/CourseDetail.vue'
import Upload from './views/Upload.vue'
import Chat from './views/Chat.vue'
import Users from './views/Users.vue'
import { store, loadMe } from './store'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', component: Login },
    { path: '/', component: Courses },
    { path: '/course/:id', component: CourseDetail },
    { path: '/upload', component: Upload, meta: { roles: ['teacher', 'admin'] } },
    { path: '/chat', component: Chat },
    { path: '/users', component: Users, meta: { roles: ['admin'] } }
  ]
})

router.beforeEach(async (to) => {
  if (!store.loaded) await loadMe()
  if (to.path === '/login') {
    return store.user ? '/' : true
  }
  if (!store.user) return '/login'
  if (to.meta.roles && !to.meta.roles.includes(store.user.role)) return '/'
  return true
})
