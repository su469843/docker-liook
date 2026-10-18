// 手绘 SVG 表情（替代 emoji）——消息里以 [[st:id]] 标记存储，渲染时替换成矢量图。
export const STICKERS = [
  {
    id: 'smile',
    label: '微笑',
    body: '<circle cx="24" cy="24" r="19" fill="#FFE9A8"/><circle cx="17.6" cy="21" r="2.4" fill="#7A5C1E"/><circle cx="30.4" cy="21" r="2.4" fill="#7A5C1E"/><circle cx="14" cy="27.5" r="2.6" fill="#FFB199" opacity=".75"/><circle cx="34" cy="27.5" r="2.6" fill="#FFB199" opacity=".75"/><path d="M15.8 28.2c2 3.3 5 5 8.2 5s6.2-1.7 8.2-5" stroke="#7A5C1E" stroke-width="2.6" stroke-linecap="round" fill="none"/>'
  },
  {
    id: 'laugh',
    label: '大笑',
    body: '<circle cx="24" cy="24" r="19" fill="#FFD166"/><path d="M14 20.8c1.8-2.2 4.8-2.2 6.6 0" stroke="#7A5C1E" stroke-width="2.6" stroke-linecap="round" fill="none"/><path d="M27.4 20.8c1.8-2.2 4.8-2.2 6.6 0" stroke="#7A5C1E" stroke-width="2.6" stroke-linecap="round" fill="none"/><path d="M13.5 26.5h21c0 6.4-4.7 11.5-10.5 11.5s-10.5-5.1-10.5-11.5z" fill="#8A4A3C"/><path d="M17.5 31.5c1.6 2.8 3.9 4.3 6.5 4.3s4.9-1.5 6.5-4.3z" fill="#FF8FA3"/>'
  },
  {
    id: 'wink',
    label: '眨眼',
    body: '<circle cx="24" cy="24" r="19" fill="#FFE9A8"/><path d="M14 21c1.8-2.2 4.8-2.2 6.6 0" stroke="#7A5C1E" stroke-width="2.6" stroke-linecap="round" fill="none"/><circle cx="30.4" cy="21" r="2.4" fill="#7A5C1E"/><path d="M16.5 28.5c2.2 2.9 4.8 4.3 7.6 4.3 2.9 0 5.5-1.4 7.7-4.3" stroke="#7A5C1E" stroke-width="2.6" stroke-linecap="round" fill="none"/><circle cx="13.4" cy="27" r="2.4" fill="#FFB199" opacity=".75"/>'
  },
  {
    id: 'think',
    label: '思考',
    body: '<circle cx="24" cy="24" r="19" fill="#DCE9FF"/><circle cx="18.4" cy="21.4" r="2.2" fill="#3B5B7A"/><circle cx="29.6" cy="21.4" r="2.2" fill="#3B5B7A"/><path d="M18.5 29.6c1.9 1.3 3.8 1.9 5.8 1.9" stroke="#3B5B7A" stroke-width="2.4" stroke-linecap="round" fill="none"/><circle cx="37.5" cy="11.5" r="6.4" fill="#fff" stroke="#7FA6D9" stroke-width="2"/><path d="M37.5 9v3" stroke="#7FA6D9" stroke-width="2.2" stroke-linecap="round"/><circle cx="37.5" cy="14.8" r="1.1" fill="#7FA6D9"/>'
  },
  {
    id: 'like',
    label: '点赞',
    body: '<circle cx="24" cy="24" r="19" fill="#DDF7EC"/><rect x="13.5" y="22.5" width="6" height="12.5" rx="1.6" fill="#12B886"/><path d="M19.5 24.2l5.3-9.6c.7-1.2 2.1-1.7 3.3-1.1 1.2.6 1.7 2 1.1 3.2l-2.1 5.6h5.7c1.8 0 3.1 1.6 2.7 3.4l-1.7 8.2c-.3 1.3-1.4 2.2-2.7 2.2H19.5z" fill="#12B886"/>'
  },
  {
    id: 'heart',
    label: '爱心',
    body: '<path d="M24 39S9.5 30.4 9.5 20.9C9.5 15.2 13.8 11.5 18.8 11.5c2.6 0 4.5 1.2 5.2 2.9.7-1.7 2.6-2.9 5.2-2.9 5 0 9.3 3.7 9.3 9.4C38.5 30.4 24 39 24 39z" fill="#FF7185"/><path d="M16.5 19.5c0-2 1.5-3.5 3.4-3.7" stroke="#FFB3BE" stroke-width="2.6" stroke-linecap="round" fill="none"/>'
  },
  {
    id: 'star',
    label: '好看',
    body: '<path d="M24 7.5l4.9 10.2 11.1 1.5-8.1 7.9 2 11-10-5.4-10 5.4 2-11-8.1-7.9 11.1-1.5z" fill="#FFC93D"/><path d="M20.5 15.8l2.2 4.5" stroke="#FFE59B" stroke-width="2.4" stroke-linecap="round"/>'
  },
  {
    id: 'fire',
    label: '火爆',
    body: '<path d="M24 5.5c7 7.3 10.5 12.5 10.5 18A10.5 10.5 0 0 1 24 34a10.5 10.5 0 0 1-10.5-15c1.3-3.3 3.7-6 6.2-7.9.5 2.9 2 4.7 3.9 4.7 1.8 0 3-2 3-4.9 0-1.7-.6-3.3-2.6-5.4z" fill="#FF8C3B"/><path d="M24 21.5c3 3.2 4.5 5.6 4.5 8a4.5 4.5 0 0 1-9 0c0-2.2 1.5-4.6 4.5-8z" fill="#FFD166"/>'
  },
  {
    id: 'party',
    label: '庆祝',
    body: '<path d="M9 40l7.5-18 10.5 10.5z" fill="#FFB199"/><path d="M13 30.5l6 6" stroke="#F08C6B" stroke-width="2" stroke-linecap="round"/><circle cx="33" cy="12.5" r="3.4" fill="#12B886"/><circle cx="41" cy="23" r="2.6" fill="#FFC93D"/><circle cx="30" cy="7.5" r="2" fill="#7FA6D9"/><path d="M31 21l4.5-4.5" stroke="#FFC93D" stroke-width="2.6" stroke-linecap="round"/><path d="M37.5 16.5l2.5-2.5" stroke="#FF7185" stroke-width="2.6" stroke-linecap="round"/>'
  }
]

const TOKEN_RE = /\[\[st:([a-z]+)\]\]/g

export const STICKER_MAP = Object.fromEntries(STICKERS.map((s) => [s.id, s]))

export function stickerToken(id) {
  return `[[st:${id}]]`
}

export function stickerById(id) {
  return STICKER_MAP[id] || null
}

// 把消息正文切成 文本 / 表情 片段，供渲染
export function splitStickers(text) {
  const src = String(text ?? '')
  const out = []
  let last = 0
  for (const m of src.matchAll(TOKEN_RE)) {
    if (m.index > last) out.push({ text: src.slice(last, m.index) })
    out.push({ sticker: m[1] })
    last = m.index + m[0].length
  }
  if (last < src.length) out.push({ text: src.slice(last) })
  if (!out.length) out.push({ text: '' })
  return out
}

// 纯文本预览（列表里显示最后一条消息时用）
export function plainText(text) {
  return String(text ?? '').replace(TOKEN_RE, (_, id) => `[${STICKER_MAP[id]?.label || '表情'}]`)
}
