<script setup>
import { computed } from 'vue'

// 手绘线条图标集（24×24，stroke 跟随 currentColor）
const PATHS = {
  logo: '<path d="M4 6.2A2.2 2.2 0 0 1 6.2 4H11v16H6.2A2.2 2.2 0 0 1 4 17.8z"/><path d="M20 6.2A2.2 2.2 0 0 0 17.8 4H13v16h4.8A2.2 2.2 0 0 0 20 17.8z"/>',
  home: '<path d="M4 10.5 12 4l8 6.5"/><path d="M6.5 9.5V20h11V9.5"/><path d="M10 20v-5h4v5"/>',
  book: '<path d="M4 5.5A2.5 2.5 0 0 1 6.5 3H11v18H6.5A2.5 2.5 0 0 1 4 18.5z"/><path d="M20 5.5A2.5 2.5 0 0 0 17.5 3H13v18h4.5A2.5 2.5 0 0 0 20 18.5z"/>',
  class: '<path d="M12 3.2 21 8H3z"/><path d="M5.5 8v9M9.8 8v9M14.2 8v9M18.5 8v9"/><path d="M3.2 20.5h17.6"/>',
  play: '<circle cx="12" cy="12" r="9"/><path d="M10.2 8.4 16 12l-5.8 3.6z"/>',
  'video-file': '<rect x="3" y="5" width="18" height="14" rx="2.6"/><path d="M10 9.4 15.6 12 10 14.6z"/>',
  text: '<rect x="4" y="3" width="16" height="18" rx="2.6"/><path d="M8 8h8M8 12h8M8 16h5"/>',
  doc: '<path d="M14 3H7.5A2.5 2.5 0 0 0 5 5.5v13A2.5 2.5 0 0 0 7.5 21h9a2.5 2.5 0 0 0 2.5-2.5V8z"/><path d="M14 3v5h5"/><path d="M9 13h6M9 16.5h4"/>',
  folder: '<path d="M3 7.5A2.5 2.5 0 0 1 5.5 5H9l2 2.2h7.5A2.5 2.5 0 0 1 21 9.7v7.8A2.5 2.5 0 0 1 18.5 20h-13A2.5 2.5 0 0 1 3 17.5z"/>',
  lock: '<rect x="5" y="10.5" width="14" height="9.5" rx="2.5"/><path d="M8.2 10.5V8a3.8 3.8 0 0 1 7.6 0v2.5"/><path d="M12 14.4v2.2"/>',
  copy: '<rect x="9" y="9" width="11" height="11" rx="2.5"/><path d="M15 6.5A2.5 2.5 0 0 0 12.5 4H6.5A2.5 2.5 0 0 0 4 6.5v6A2.5 2.5 0 0 0 6.5 15"/>',
  search: '<circle cx="11" cy="11" r="6.5"/><path d="M20 20l-3.8-3.8"/>',
  chat: '<path d="M4 6.5A2.5 2.5 0 0 1 6.5 4h11A2.5 2.5 0 0 1 20 6.5v7A2.5 2.5 0 0 1 17.5 16H9.5L4.5 20V6.5z"/><path d="M8.5 10h7"/>',
  send: '<path d="M4.5 12 20 5l-6.8 14.5-2.6-6z"/><path d="M10.6 13.5 20 5"/>',
  phone: '<path d="M21.6 16.9v2.9a2 2 0 0 1-2.2 2 19.6 19.6 0 0 1-8.5-3 19.3 19.3 0 0 1-6-6 19.6 19.6 0 0 1-3-8.6 2 2 0 0 1 2-2.2h2.9a2 2 0 0 1 2 1.7c.1 1 .4 1.9.7 2.8a2 2 0 0 1-.5 2.1l-1.2 1.2a15.8 15.8 0 0 0 6 6l1.2-1.2a2 2 0 0 1 2.1-.5c.9.3 1.8.6 2.8.7a2 2 0 0 1 1.7 2z"/>',
  'phone-off': '<path d="M21.6 16.9v2.9a2 2 0 0 1-2.2 2 19.6 19.6 0 0 1-8.5-3 19.3 19.3 0 0 1-6-6 19.6 19.6 0 0 1-3-8.6 2 2 0 0 1 2-2.2h2.9a2 2 0 0 1 2 1.7c.1 1 .4 1.9.7 2.8a2 2 0 0 1-.5 2.1l-1.2 1.2a15.8 15.8 0 0 0 6 6l1.2-1.2a2 2 0 0 1 2.1-.5c.9.3 1.8.6 2.8.7a2 2 0 0 1 1.7 2z"/><path d="M3.5 3.5l17 17"/>',
  camcorder: '<rect x="2.5" y="6.5" width="12.5" height="11" rx="2.6"/><path d="M15 11.2 21.5 7.5v9L15 12.8z"/>',
  camera: '<rect x="3" y="7" width="18" height="13" rx="2.6"/><circle cx="12" cy="13.6" r="3.4"/><path d="M8.5 7l1.3-2.2h4.4L15.5 7"/>',
  'camera-off': '<rect x="3" y="7" width="18" height="13" rx="2.6"/><circle cx="12" cy="13.6" r="3.4"/><path d="M8.5 7l1.3-2.2h4.4L15.5 7"/><path d="M4 4l16 16"/>',
  mic: '<rect x="9" y="3" width="6" height="10.5" rx="3"/><path d="M5.5 11.5a6.5 6.5 0 0 0 13 0"/><path d="M12 18v3"/>',
  'mic-off': '<rect x="9" y="3" width="6" height="10.5" rx="3"/><path d="M5.5 11.5a6.5 6.5 0 0 0 13 0"/><path d="M12 18v3"/><path d="M4 4l16 16"/>',
  hangup: '<path d="M3.5 14.2c0-.9.8-1.6 1.7-1.3l2.8 1c.6.2 1 .8 1 1.4v1.6c1.9 1 4.1 1 6 0v-1.6c0-.6.4-1.2 1-1.4l2.8-1c.9-.3 1.7.4 1.7 1.3v1.5c0 .6-.3 1.2-.9 1.5-4.4 2-9.8 2-14.2 0a1.7 1.7 0 0 1-.9-1.5z"/>',
  settings: '<circle cx="12" cy="12" r="3.1"/><path d="M12 2.8v2.4M12 18.8v2.4M21.2 12h-2.4M5.2 12H2.8M18.5 5.5l-1.7 1.7M7.2 16.8l-1.7 1.7M18.5 18.5l-1.7-1.7M7.2 7.2 5.5 5.5"/>',
  logout: '<path d="M15 12H4"/><path d="M8.5 7.5 4 12l4.5 4.5"/><path d="M13 4.5h5A2.5 2.5 0 0 1 20.5 7v10A2.5 2.5 0 0 1 18 19.5h-5"/>',
  image: '<rect x="3" y="4.5" width="18" height="15" rx="2.6"/><circle cx="8.6" cy="10" r="1.7"/><path d="M20.5 16 16 11.5 9.5 19"/>',
  link: '<path d="M10.5 13.5a4.2 4.2 0 0 0 6 0l2.6-2.6a4.2 4.2 0 1 0-6-6l-1.4 1.4"/><path d="M13.5 10.5a4.2 4.2 0 0 0-6 0l-2.6 2.6a4.2 4.2 0 1 0 6 6l1.4-1.4"/>',
  bulb: '<path d="M9.3 18.3h5.4"/><path d="M10.4 21.2h3.2"/><path d="M12 2.8c-3.3 0-6 2.6-6 5.9 0 2 .9 3.6 2.2 4.7.7.6 1.2 1.5 1.2 2.4v.3h5.2v-.3c0-.9.5-1.8 1.2-2.4a5.9 5.9 0 0 0 2.2-4.7c0-3.3-2.7-5.9-6-5.9z"/>',
  globe: '<circle cx="12" cy="12" r="8.5"/><path d="M3.5 12h17"/><path d="M12 3.5c2.4 2.3 3.6 5.3 3.6 8.5S14.4 18.2 12 20.5c-2.4-2.3-3.6-5.3-3.6-8.5S9.6 5.8 12 3.5z"/>',
  ticket: '<path d="M3.5 9.5V7A1.5 1.5 0 0 1 5 5.5h14A1.5 1.5 0 0 1 20.5 7v2.5a2.7 2.7 0 0 0 0 5V17A1.5 1.5 0 0 1 19 18.5H5A1.5 1.5 0 0 1 3.5 17v-2.6a2.7 2.7 0 0 0 0-4.9z"/><path d="M12 6v2M12 11v2M12 16v2"/>',
  close: '<path d="M6 6l12 12M18 6 6 18"/>',
  check: '<path d="M5 12.5l4.5 4.5L19 7"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  minus: '<path d="M5 12h14"/>',
  edit: '<path d="M4 20h4L19 9a2.1 2.1 0 0 0-3-3L5 17z"/><path d="M14.5 6.5l3 3"/>',
  trash: '<path d="M4 7h16"/><path d="M9.5 7V5A1 1 0 0 1 10.5 4h3A1 1 0 0 1 14.5 5v2"/><path d="M6 7l1 12a2.5 2.5 0 0 0 2.5 2.3h5A2.5 2.5 0 0 0 17 19l1-12"/>',
  upload: '<path d="M12 16V4"/><path d="M7.5 8.5 12 4l4.5 4.5"/><path d="M4.5 16v1.5A3.5 3.5 0 0 0 8 21h8a3.5 3.5 0 0 0 3.5-3.5V16"/>',
  download: '<path d="M12 4v12"/><path d="M7.5 11.5 12 16l4.5-4.5"/><path d="M4.5 16v1.5A3.5 3.5 0 0 0 8 21h8a3.5 3.5 0 0 0 3.5-3.5V16"/>',
  'arrow-left': '<path d="M19 12H5"/><path d="M11 6l-6 6 6 6"/>',
  'arrow-right': '<path d="M5 12h14"/><path d="M13 6l6 6-6 6"/>',
  'chevron-right': '<path d="M9 5l7 7-7 7"/>',
  'chevron-down': '<path d="M6 9l6 6 6-6"/>',
  'chevron-left': '<path d="M15 5l-7 7 7 7"/>',
  users: '<circle cx="9.5" cy="8" r="3.6"/><path d="M3.5 20a6 6 0 0 1 12 0"/><path d="M16.2 4.9a3.6 3.6 0 0 1 0 6.2"/><path d="M17.5 14.4a6 6 0 0 1 3.2 5.1"/>',
  user: '<circle cx="12" cy="8" r="3.8"/><path d="M4.8 20.5a7.2 7.2 0 0 1 14.4 0"/>',
  grid: '<rect x="4" y="4" width="6.5" height="6.5" rx="1.8"/><rect x="13.5" y="4" width="6.5" height="6.5" rx="1.8"/><rect x="4" y="13.5" width="6.5" height="6.5" rx="1.8"/><rect x="13.5" y="13.5" width="6.5" height="6.5" rx="1.8"/>',
  chart: '<path d="M4 20.5h16"/><path d="M7 20V11M12 20V4.5M17 20v-6.5"/>',
  clock: '<circle cx="12" cy="12" r="8.5"/><path d="M12 7.5V12l3.3 2"/>',
  eye: '<path d="M2.8 12S6.3 5.8 12 5.8 21.2 12 21.2 12 17.7 18.2 12 18.2 2.8 12 2.8 12z"/><circle cx="12" cy="12" r="2.8"/>',
  quote: '<path d="M9.5 14.5 4.8 9.8l4.7-4.7"/><path d="M4.8 9.8h9a5.7 5.7 0 0 1 5.7 5.7v3.8"/>',
  refresh: '<path d="M20.5 12a8.5 8.5 0 1 1-2.6-6.1"/><path d="M20.5 4.2v4.6h-4.6"/>',
  megaphone: '<path d="M4 10.5 17 5v14L4 13.5z"/><path d="M4 10.5H2.8a1 1 0 0 0-1 1v1a1 1 0 0 0 1 1H4"/><path d="M7.5 14.2V18a2 2 0 0 0 4 0v-2.2"/>',
  target: '<circle cx="12" cy="12" r="8.5"/><circle cx="12" cy="12" r="4.5"/><circle cx="12" cy="12" r="1"/>',
  spark: '<path d="M12 3.5l1.9 5.3 5.3 1.9-5.3 1.9L12 18l-1.9-5.4L4.8 10.7l5.3-1.9z"/>',
  sticker: '<circle cx="12" cy="12" r="8.5"/><circle cx="9" cy="10" r="1" fill="currentColor" stroke="none"/><circle cx="15" cy="10" r="1" fill="currentColor" stroke="none"/><path d="M8.8 14.3a4.2 4.2 0 0 0 6.4 0"/>',
  star: '<path d="M12 3.6l2.6 5.3 5.8.8-4.2 4.1 1 5.8-5.2-2.8-5.2 2.8 1-5.8L3.6 9.7l5.8-.8z"/>',
  heart: '<path d="M12 20.5s-8-4.7-8-10A4.6 4.6 0 0 1 12 7.6a4.6 4.6 0 0 1 8 2.9c0 5.3-8 10-8 10z"/>',
  fire: '<path d="M12 3.2c3.6 3.8 5.6 6.9 5.6 10a5.6 5.6 0 0 1-11.2 0c0-1.9.9-3.6 2.2-5.1.4 1.7 1.3 2.7 2.4 2.7 1.1 0 1.8-1.2 1.8-2.8 0-1.6-.4-3.1-.8-4.8z"/>',
  plant: '<path d="M7 14h10l-1.2 6.5H8.2z"/><path d="M12 14V8.5"/><path d="M12 11c-2.4 0-4-1.6-4-4 2.4 0 4 1.6 4 4z"/><path d="M12 9.5c2 0 3.4-1.4 3.4-3.4-2 0-3.4 1.4-3.4 3.4z"/>',
  'file-plus': '<path d="M14 3H7.5A2.5 2.5 0 0 0 5 5.5v13A2.5 2.5 0 0 0 7.5 21h9a2.5 2.5 0 0 0 2.5-2.5V8z"/><path d="M14 3v5h5"/><path d="M12 11.5v6M9 14.5h6"/>',
  layers: '<path d="M12 3.5 21 8l-9 4.5L3 8z"/><path d="M4.5 12.2 12 15.8l7.5-3.6"/><path d="M4.5 16 12 19.6l7.5-3.6"/>'
}

const props = defineProps({
  name: { type: String, required: true },
  size: { type: [Number, String], default: 20 },
  stroke: { type: [Number, String], default: 1.7 }
})

const inner = computed(() => PATHS[props.name] || PATHS.spark)
</script>

<template>
  <svg
    class="ico"
    :width="size"
    :height="size"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    :stroke-width="stroke"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
    v-html="inner"
  ></svg>
</template>
