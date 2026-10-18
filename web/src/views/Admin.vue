<script setup>
import { computed, onMounted, ref } from 'vue'
import { api } from '../api'
import { store, loadConfig } from '../store'
import { fmtDate } from '../format'
import { toastOk, toastErr, confirmDialog } from '../ui'
import Modal from '../components/Modal.vue'
import Icon from '../components/Icon.vue'

const tab = ref('users')
const users = ref([])
const courses = ref([])
const settings = ref({
  site_name: '课程中心', register_mode: 'open', org_invite_code: '',
  storage_type: '', s3_endpoint: '', s3_bucket: '', s3_access_key: '', s3_secret_key: '', s3_use_ssl: false,
  webdav_url: '', webdav_username: '', webdav_password: ''
})
const loading = ref(true)

const teachers = computed(() => users.value.filter((u) => u.role === 'teacher'))
const modeInfo = {
  open: { title: '开放注册', desc: '任何人都可以自己注册学员账号，注册后即可登录学习。', icon: 'globe' },
  invite: { title: '邀请码注册', desc: '必须输入机构邀请码才能注册，适合只对自己人开放的机构。', icon: 'ticket' },
  closed: { title: '仅机构成员登录', desc: '完全关闭注册，只能由管理员在后台创建账号；已有账号正常登录。', icon: 'lock' }
}

async function loadAll() {
  loading.value = true
  try {
    const [us, cs, st] = await Promise.all([
      api('/api/users'),
      api('/api/courses'),
      api('/api/settings')
    ])
    users.value = us
    courses.value = cs
    settings.value = st
  } catch (e) {
    toastErr(e)
  }
  loading.value = false
}
onMounted(loadAll)

// ---------------- 用户 ----------------
const showCreate = ref(false)
const creating = ref(false)
const form = ref({ username: '', password: '', display_name: '', role: 'student' })
const pwdUser = ref(null)
const newPwd = ref('')

async function createUser() {
  if (!form.value.username.trim() || form.value.password.length < 6) {
    toastErr('用户名必填，密码至少 6 位')
    return
  }
  creating.value = true
  try {
    await api('/api/users', { method: 'POST', json: form.value })
    toastOk('账号已创建')
    showCreate.value = false
    form.value = { username: '', password: '', display_name: '', role: 'student' }
    await loadAll()
  } catch (e) {
    toastErr(e)
  } finally {
    creating.value = false
  }
}

async function toggleStatus(u) {
  const to = u.status === 'active' ? 'disabled' : 'active'
  const ok = await confirmDialog(`确定${to === 'disabled' ? '禁用' : '启用'}账号「${u.display_name}」？`,
    { danger: to === 'disabled', okText: to === 'disabled' ? '禁用' : '启用' })
  if (!ok) return
  try {
    await api(`/api/users/${u.id}/status`, { method: 'POST', json: { status: to } })
    toastOk('操作成功')
    await loadAll()
  } catch (e) {
    toastErr(e)
  }
}

function openPwd(u) {
  pwdUser.value = u
  newPwd.value = ''
}

async function savePwd() {
  if (newPwd.value.length < 6) { toastErr('密码至少 6 位'); return }
  try {
    await api(`/api/users/${pwdUser.value.id}/password`, { method: 'POST', json: { password: newPwd.value } })
    toastOk('密码已重置')
    pwdUser.value = null
  } catch (e) {
    toastErr(e)
  }
}

// ---------------- 注册设置 ----------------
const savingSettings = ref(false)

async function saveSettings() {
  savingSettings.value = true
  try {
    settings.value = await api('/api/settings', {
      method: 'PUT',
      json: { site_name: settings.value.site_name, register_mode: settings.value.register_mode }
    })
    await loadConfig()
    toastOk('设置已保存')
    await loadAll()
  } catch (e) {
    toastErr(e)
  } finally {
    savingSettings.value = false
  }
}

async function regenOrgCode() {
  const ok = await confirmDialog('重新生成机构邀请码？旧邀请码将无法用于注册。')
  if (!ok) return
  try {
    settings.value = await api('/api/settings/invite', { method: 'POST' })
    toastOk('新的机构邀请码已生成')
  } catch (e) {
    toastErr(e)
  }
}

function copyOrgCode() {
  if (!settings.value.org_invite_code) return
  navigator.clipboard?.writeText(settings.value.org_invite_code)
  toastOk('机构邀请码已复制')
}

// ---------------- 存储设置 ----------------
const savingStorage = ref(false)
const testingStorage = ref(false)

function storagePayload() {
  const s = settings.value
  return {
    storage_type: s.storage_type || '',
    s3_endpoint: s.s3_endpoint || '',
    s3_bucket: s.s3_bucket || '',
    s3_access_key: s.s3_access_key || '',
    s3_secret_key: s.s3_secret_key || '',
    s3_use_ssl: !!s.s3_use_ssl,
    webdav_url: s.webdav_url || '',
    webdav_username: s.webdav_username || '',
    webdav_password: s.webdav_password || ''
  }
}

async function saveStorage() {
  savingStorage.value = true
  try {
    settings.value = await api('/api/settings', { method: 'PUT', json: storagePayload() })
    toastOk('存储设置已保存，新上传的文件会写入该后端')
    await loadAll()
  } catch (e) {
    toastErr(e)
  } finally {
    savingStorage.value = false
  }
}

async function testStorage() {
  testingStorage.value = true
  try {
    const r = await api('/api/settings/storage/test', { method: 'POST', json: storagePayload() })
    toastOk(r.message || '连接正常')
  } catch (e) {
    toastErr(e)
  } finally {
    testingStorage.value = false
  }
}

// ---------------- 课程管理 ----------------
async function togglePublish(c) {
  const to = !c.published
  try {
    await api(`/api/courses/${c.id}/publish`, { method: 'POST', json: { published: to } })
    c.published = to
    toastOk(to ? '已上架' : '已下架')
  } catch (e) {
    toastErr(e)
  }
}

async function assignTeacher(c, ev) {
  const tid = Number(ev.target.value)
  if (!tid) return
  try {
    await api(`/api/courses/${c.id}/teacher`, { method: 'POST', json: { teacher_id: tid } })
    toastOk('任课老师已更换')
    await loadAll()
  } catch (e) {
    toastErr(e)
  }
}
</script>

<template>
  <div class="container">
    <div class="page-head">
      <div>
        <h1 class="page-title">管理后台</h1>
        <div class="page-sub">机构设置、账号管理与课程管理</div>
      </div>
    </div>

    <div class="tabs">
      <button class="tab" :class="{ active: tab === 'users' }" @click="tab = 'users'">用户管理</button>
      <button class="tab" :class="{ active: tab === 'settings' }" @click="tab = 'settings'">站点设置</button>
      <button class="tab" :class="{ active: tab === 'courses' }" @click="tab = 'courses'">课程管理</button>
    </div>

    <div v-if="loading" class="skeleton" style="height:200px"></div>

    <!-- 用户管理 -->
    <template v-else-if="tab === 'users'">
      <div class="row" style="justify-content:flex-end;margin-bottom:14px">
        <button class="btn primary" @click="showCreate = true"><Icon name="plus" :size="16" /> 新建账号</button>
      </div>
      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr><th>ID</th><th>昵称</th><th>用户名</th><th>角色</th><th>状态</th><th>创建时间</th><th style="width:200px">操作</th></tr>
          </thead>
          <tbody>
            <tr v-for="u in users" :key="u.id">
              <td class="muted">{{ u.id }}</td>
              <td>
                <div class="row">
                  <span class="avatar sm" :class="u.role">{{ (u.display_name || '?')[0] }}</span>
                  {{ u.display_name }}
                </div>
              </td>
              <td class="muted">{{ u.username }}</td>
              <td>
                <span class="pill" :class="u.role === 'admin' ? 'orange' : (u.role === 'teacher' ? 'green' : 'blue')">
                  {{ u.role === 'admin' ? '管理员' : (u.role === 'teacher' ? '老师' : '学员') }}
                </span>
              </td>
              <td>
                <span class="pill" :class="u.status === 'active' ? 'green' : 'red'">
                  {{ u.status === 'active' ? '正常' : '已禁用' }}
                </span>
              </td>
              <td class="muted">{{ fmtDate(u.created_at) }}</td>
              <td>
                <div class="row">
                  <button class="btn small" @click="openPwd(u)">改密</button>
                  <button v-if="u.id !== 1 && u.id !== store.user?.id" class="btn small" :class="{ danger: u.status === 'active' }"
                    @click="toggleStatus(u)">{{ u.status === 'active' ? '禁用' : '启用' }}</button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <p class="muted small row" style="margin-top:12px;gap:6px">
        <Icon name="bulb" :size="16" />
        老师账号请在这里创建；学员可以通过「注册设置」里的模式自助注册，也可以在这里批量创建。
      </p>
    </template>

    <!-- 站点设置 -->
    <template v-else-if="tab === 'settings'">
      <div class="card mb" style="max-width:860px">
        <div class="panel-title">站点信息</div>
        <div class="field" style="max-width:420px">
          <label>站点名称（显示在顶栏和登录页）</label>
          <input v-model="settings.site_name" class="input" />
        </div>
      </div>

      <div class="card mb" style="max-width:860px">
        <div class="panel-title">注册模式</div>
        <div class="row" style="align-items:stretch;gap:12px">
          <div v-for="(info, key) in modeInfo" :key="key"
            class="card card-hover" style="flex:1;min-width:220px;cursor:pointer;padding:16px"
            :style="settings.register_mode === key ? 'border-color:var(--brand);box-shadow:0 0 0 3px rgba(18,184,134,.12)' : ''"
            @click="settings.register_mode = key">
            <div class="stat-ico" style="margin-bottom:8px"><Icon :name="info.icon" :size="22" /></div>
            <div style="font-weight:600;margin:6px 0 4px">{{ info.title }}</div>
            <div class="muted small">{{ info.desc }}</div>
          </div>
        </div>

        <div v-if="settings.register_mode === 'invite'" class="invite-box" style="margin-top:16px">
          <div>
            <div class="muted small">机构邀请码（注册时必填，可发给机构成员）</div>
            <div class="invite-code" @click="copyOrgCode">
              <span>{{ settings.org_invite_code || '——' }}</span>
              <Icon name="copy" :size="15" />
            </div>
          </div>
          <div class="row">
            <button class="btn small" @click="copyOrgCode">复制</button>
            <button class="btn small" @click="regenOrgCode">重新生成</button>
          </div>
        </div>
        <p v-else-if="settings.register_mode === 'closed'" class="alert info" style="margin-top:16px">
          关闭注册后，新学员只能由管理员在「用户管理 → 新建账号」中创建；已有账号仍可正常登录。
        </p>
        <p v-else class="muted small" style="margin-top:16px">开放注册时，任何人访问登录页都能自己注册学员账号。</p>

        <button class="btn primary mt" :disabled="savingSettings" @click="saveSettings">
          {{ savingSettings ? '保存中…' : '保存设置' }}
        </button>
      </div>

      <div class="card" style="max-width:860px">
        <div class="panel-title">文件存储</div>
        <p class="muted small" style="margin:0 0 14px">
          视频源文件、HLS 分片、图片和文档都会写到这个存储后端。留空「跟随环境变量」时使用服务器上的本地目录
          （Docker 里是挂载的 <code>/data</code>）。也可以接到自建 MinIO / S3 或 WebDAV（坚果云、群晖等），省服务器磁盘。
        </p>
        <div class="field" style="max-width:300px">
          <label>存储后端</label>
          <select v-model="settings.storage_type" class="select">
            <option value="">跟随环境变量（默认本地目录）</option>
            <option value="local">本地目录</option>
            <option value="s3">S3 / MinIO（对象存储）</option>
            <option value="webdav">WebDAV</option>
          </select>
        </div>

        <template v-if="settings.storage_type === 's3'">
          <div class="form-grid">
            <div class="field">
              <label>Endpoint *</label>
              <input v-model="settings.s3_endpoint" class="input" placeholder="例如 minio.example.com:9000" />
            </div>
            <div class="field">
              <label>Bucket *</label>
              <input v-model="settings.s3_bucket" class="input" placeholder="例如 coursehub" />
            </div>
            <div class="field">
              <label>Access Key *</label>
              <input v-model="settings.s3_access_key" class="input" />
            </div>
            <div class="field">
              <label>Secret Key *</label>
              <input v-model="settings.s3_secret_key" class="input" type="password" />
            </div>
          </div>
          <label class="row" style="gap:6px;cursor:pointer;margin-bottom:12px">
            <input v-model="settings.s3_use_ssl" type="checkbox" /> 使用 HTTPS（S3_USE_SSL）
          </label>
        </template>

        <template v-else-if="settings.storage_type === 'webdav'">
          <div class="field">
            <label>WebDAV 地址 *</label>
            <input v-model="settings.webdav_url" class="input" placeholder="例如 https://dav.example.com/coursehub" />
          </div>
          <div class="form-grid">
            <div class="field">
              <label>用户名</label>
              <input v-model="settings.webdav_username" class="input" />
            </div>
            <div class="field">
              <label>密码</label>
              <input v-model="settings.webdav_password" class="input" type="password" />
            </div>
          </div>
        </template>

        <div class="row mt">
          <button class="btn" :disabled="testingStorage" @click="testStorage">
            {{ testingStorage ? '测试中…' : '测试连接' }}
          </button>
          <button class="btn primary" :disabled="savingStorage" @click="saveStorage">
            {{ savingStorage ? '保存中…' : '保存存储设置' }}
          </button>
        </div>
      </div>
    </template>

    <!-- 课程管理 -->
    <template v-else-if="tab === 'courses'">
      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr><th>ID</th><th>课程</th><th>任课老师</th><th>状态</th><th>课时</th><th>学员</th><th style="width:190px">操作</th></tr>
          </thead>
          <tbody>
            <tr v-for="c in courses" :key="c.id">
              <td class="muted">{{ c.id }}</td>
              <td>
                <RouterLink :to="'/course/' + c.id" style="font-weight:500">{{ c.title }}</RouterLink>
                <div class="muted small">{{ c.category || '未分类' }}</div>
              </td>
              <td>
                <select class="select" style="height:32px;width:150px" :value="c.teacher_id" @change="assignTeacher(c, $event)">
                  <option v-for="t in teachers" :key="t.id" :value="t.id">{{ t.display_name }}</option>
                </select>
              </td>
              <td>
                <span class="pill" :class="c.published ? 'green' : 'gray'">{{ c.published ? '已上架' : '未上架' }}</span>
              </td>
              <td>{{ c.lesson_count }}</td>
              <td>{{ c.student_count }}</td>
              <td>
                <div class="row">
                  <button class="btn small" @click="togglePublish(c)">{{ c.published ? '下架' : '上架' }}</button>
                  <RouterLink :to="'/studio/' + c.id" class="btn small">管理</RouterLink>
                </div>
              </td>
            </tr>
            <tr v-if="!courses.length"><td colspan="7" class="muted center" style="padding:32px">还没有课程</td></tr>
          </tbody>
        </table>
      </div>
    </template>

    <!-- 新建账号 -->
    <Modal v-if="showCreate" title="新建账号" @close="showCreate = false">
      <div class="form-grid">
        <div class="field">
          <label>用户名 *</label>
          <input v-model="form.username" class="input" placeholder="3-32 位字母数字下划线" />
        </div>
        <div class="field">
          <label>初始密码 *</label>
          <input v-model="form.password" class="input" placeholder="至少 6 位" />
        </div>
      </div>
      <div class="form-grid">
        <div class="field">
          <label>昵称</label>
          <input v-model="form.display_name" class="input" placeholder="显示名称" />
        </div>
        <div class="field">
          <label>角色</label>
          <select v-model="form.role" class="select">
            <option value="student">学员</option>
            <option value="teacher">老师</option>
            <option value="admin">管理员</option>
          </select>
        </div>
      </div>
      <template #foot>
        <button class="btn" @click="showCreate = false">取消</button>
        <button class="btn primary" :disabled="creating" @click="createUser">{{ creating ? '创建中…' : '创建账号' }}</button>
      </template>
    </Modal>

    <!-- 重置密码 -->
    <Modal v-if="pwdUser" :title="'重置密码 · ' + pwdUser.display_name" @close="pwdUser = null">
      <div class="field">
        <label>新密码（至少 6 位）</label>
        <input v-model="newPwd" class="input" type="text" placeholder="输入新密码" @keyup.enter="savePwd" />
      </div>
      <template #foot>
        <button class="btn" @click="pwdUser = null">取消</button>
        <button class="btn primary" @click="savePwd">确认重置</button>
      </template>
    </Modal>
  </div>
</template>
