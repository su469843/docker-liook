import { createRouter, createWebHistory } from 'vue-router'
import Login from './views/Login.vue'
import Market from './views/Market.vue'
import MyCourses from './views/MyCourses.vue'
import CourseDetail from './views/CourseDetail.vue'
import Learn from './views/Learn.vue'
import Messages from './views/Messages.vue'
import Studio from './views/Studio.vue'
import StudioCourse from './views/StudioCourse.vue'
import Admin from './views/Admin.vue'
import { store, loadMe, loadConfig, homeFor } from './store'

export const router = createRouter({
  history: createWebHistory(),
  scrollBehavior: () => ({ top: 0 }),
  routes: [
    { path: '/login', component: Login, meta: { chrome: false, public: true } },
    { path: '/', component: Market },
    { path: '/my', component: MyCourses, meta: { roles: ['student'] } },
    { path: '/course/:id', component: CourseDetail },
    { path: '/learn/:courseId/:lessonId?', component: Learn, meta: { chrome: false } },
    { path: '/messages', component: Messages },
    { path: '/studio', component: Studio, meta: { roles: ['teacher', 'admin'] } },
    { path: '/studio/:courseId', component: StudioCourse, meta: { roles: ['teacher', 'admin'] } },
    { path: '/admin', component: Admin, meta: { roles: ['admin'] } },
    // 兼容旧地址
    { path: '/chat', redirect: '/messages' },
    { path: '/users', redirect: '/admin' },
    { path: '/upload', redirect: '/studio' },
    { path: '/courses', redirect: '/my' },
    { path: '/:pathMatch(.*)*', redirect: '/' }
  ]
})

router.beforeEach(async (to) => {
  if (!store.loaded) {
    await Promise.all([loadMe(), loadConfig()])
  }
  if (to.meta.public) {
    return store.user ? homeFor(store.user.role) : true
  }
  if (!store.user) return '/login'
  if (to.meta.roles && !to.meta.roles.includes(store.user.role)) {
    return homeFor(store.user.role)
  }
  return true
})
