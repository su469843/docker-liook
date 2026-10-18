import { reactive } from 'vue'

// 轻量全局提示与确认框（零依赖）

export const toasts = reactive([])
let seq = 0

export function toast(text, type = 'info', ms = 2600) {
  const id = ++seq
  toasts.push({ id, text, type })
  setTimeout(() => {
    const i = toasts.findIndex((t) => t.id === id)
    if (i >= 0) toasts.splice(i, 1)
  }, ms)
}

export function toastOk(text) { toast(text, 'success') }
export function toastErr(e) {
  const msg = typeof e === 'string' ? e : (e && e.message) || '操作失败'
  toast(msg, 'error', 3400)
}

export const confirmState = reactive({
  open: false, title: '确认操作', message: '', okText: '确定', danger: false, resolve: null
})

export function confirmDialog(message, opts = {}) {
  return new Promise((resolve) => {
    Object.assign(confirmState, {
      open: true,
      title: opts.title || '确认操作',
      message,
      okText: opts.okText || '确定',
      danger: !!opts.danger,
      resolve
    })
  })
}

export function resolveConfirm(v) {
  const r = confirmState.resolve
  confirmState.open = false
  confirmState.resolve = null
  if (r) r(v)
}
