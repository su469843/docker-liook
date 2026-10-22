<script setup>
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api'
import { store, loadMe, loadConfig, homeFor } from '../store'
import Icon from '../components/Icon.vue'

const router = useRouter()
const mode = ref('login')
const username = ref('')
const password = ref('')
const displayName = ref('')
const inviteCode = ref('')
const err = ref('')
const loading = ref(false)

const cfg = computed(() => store.config || {})
const canRegister = computed(() => cfg.value.allow_register !== false)
const needInvite = computed(() => cfg.value.need_invite === true)
const siteName = computed(() => cfg.value.site_name || '课程中心')
const version = computed(() => cfg.value.version || '')
const modeLabel = computed(() => ({
  open: '开放注册',
  invite: '邀请码注册',
  closed: '仅机构成员可登录'
}[cfg.value.register_mode] || '开放注册'))

onMounted(async () => {
  await loadConfig()
  if (!canRegister.value) mode.value = 'login'
})

async function submit() {
  err.value = ''
  if (!username.value.trim()) { err.value = '请输入用户名'; return }
  if (password.value.length < 6) { err.value = '密码至少 6 位'; return }
  loading.value = true
  try {
    if (mode.value === 'login') {
      await api('/api/login', { method: 'POST', json: { username: username.value.trim(), password: password.value } })
    } else {
      await api('/api/register', {
        method: 'POST',
        json: {
          username: username.value.trim(),
          password: password.value,
          display_name: displayName.value.trim(),
          invite_code: inviteCode.value.trim().toUpperCase()
        }
      })
    }
    await loadMe()
    router.push(homeFor(store.user?.role))
  } catch (e) {
    err.value = e.message
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="login-page">
    <section class="login-hero">
      <div class="brand"><span class="brand-logo"><Icon name="logo" :size="20" /></span><span>{{ siteName }}</span></div>
      <h1>让每一堂课<br />都被看见</h1>
      <p>老师开课、学员上课、课程群交流，一个属于你自己机构的在线课堂。</p>
      <div class="login-points">
        <div class="login-point"><span class="ico"><Icon name="video-file" :size="18" /></span>视频 / 图文 / 文档，多种课时形式</div>
        <div class="login-point"><span class="ico"><Icon name="class" :size="18" /></span>课程市场 + 邀请码，两种招生方式</div>
        <div class="login-point"><span class="ico"><Icon name="chat" :size="18" /></span>课程群聊，可以引用视频时间点讨论</div>
      </div>
    </section>

    <section class="login-form-wrap">
      <form class="login-form" @submit.prevent="submit">
        <h2>{{ mode === 'login' ? '欢迎回来' : '注册学员账号' }}</h2>
        <p class="sub">{{ mode === 'login' ? '登录后开始学习' : `当前模式：${modeLabel}` }}</p>

        <div v-if="canRegister" class="login-tabs">
          <button type="button" :class="{ active: mode === 'login' }" @click="mode = 'login'">登录</button>
          <button type="button" :class="{ active: mode === 'register' }" @click="mode = 'register'">注册</button>
        </div>

        <div class="field">
          <label>用户名</label>
          <input v-model="username" class="input" autocomplete="username" placeholder="3-32 位字母数字下划线" autofocus />
        </div>

        <div v-if="mode === 'register'" class="field">
          <label>昵称（选填）</label>
          <input v-model="displayName" class="input" placeholder="显示给老师和同学的名字" />
        </div>

        <div class="field">
          <label>密码</label>
          <input v-model="password" class="input" type="password"
            :autocomplete="mode === 'login' ? 'current-password' : 'new-password'" placeholder="至少 6 位" />
        </div>

        <div v-if="mode === 'register' && needInvite" class="field">
          <label>机构邀请码 <span class="muted small">（必填，向管理员索取）</span></label>
          <input v-model="inviteCode" class="input" placeholder="例如 ABCDEFGH" style="text-transform:uppercase" />
        </div>

        <p v-if="err" class="alert error" style="margin:0 0 14px">{{ err }}</p>

        <button class="btn primary block lg" type="submit" :disabled="loading">
          {{ loading ? '请稍候…' : (mode === 'login' ? '登 录' : '注 册') }}
        </button>

        <p class="login-tip">
          <template v-if="mode === 'register'">注册后为学员账号；老师账号请联系机构管理员开通。</template>
          <template v-else-if="!canRegister">本站已关闭自助注册，请联系机构管理员开通账号。</template>
          <template v-else>忘记密码？请联系机构管理员在后台重置。</template>
        </p>

        <p v-if="version" class="login-version">版本 {{ version }}</p>
      </form>
    </section>
  </div>
</template>
