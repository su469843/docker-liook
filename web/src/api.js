// 统一请求封装：会话走 HttpOnly Cookie（fetch 同源默认携带），401 跳登录
export async function api(path, opts = {}) {
  const headers = { ...(opts.headers || {}) }
  if (opts.json !== undefined) {
    headers['Content-Type'] = 'application/json'
    opts = { ...opts, body: JSON.stringify(opts.json) }
  }
  const res = await fetch(path, { ...opts, headers, credentials: 'same-origin' })
  if (res.status === 401) {
    if (!location.pathname.startsWith('/login')) location.href = '/login'
    throw new Error('未登录')
  }
  const text = await res.text()
  let data = null
  try { data = text ? JSON.parse(text) : null } catch { data = text }
  if (!res.ok) throw new Error((data && data.error) || '请求失败 ' + res.status)
  return data
}

export function rawPut(path, body, contentType) {
  return api(path, { method: 'PUT', headers: { 'Content-Type': contentType }, body })
}
