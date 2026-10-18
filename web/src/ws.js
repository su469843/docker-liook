import { reactive } from 'vue'

// 全局单例 WebSocket：私聊 / 课程群聊 / 音视频通话信令共用一条连接

export const wsState = reactive({ connected: false })

let ws = null
let timer = null
const handlers = new Set()

export function onWS(fn) {
  handlers.add(fn)
  return () => handlers.delete(fn)
}

function emit(msg) {
  handlers.forEach((fn) => {
    try { fn(msg) } catch (e) { console.error(e) }
  })
}

export function connectWS() {
  if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) return
  const proto = location.protocol === 'https:' ? 'wss' : 'ws'
  try {
    ws = new WebSocket(`${proto}://${location.host}/ws`)
  } catch {
    return
  }
  ws.onopen = () => { wsState.connected = true }
  ws.onmessage = (ev) => {
    let m
    try { m = JSON.parse(ev.data) } catch { return }
    emit(m)
  }
  ws.onclose = () => {
    wsState.connected = false
    clearTimeout(timer)
    timer = setTimeout(connectWS, 3000)
  }
  ws.onerror = () => {}
}

export function disconnectWS() {
  clearTimeout(timer)
  if (ws) {
    ws.onclose = null
    ws.close()
    ws = null
  }
  wsState.connected = false
}

export function sendWS(obj) {
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify(obj))
    return true
  }
  return false
}
