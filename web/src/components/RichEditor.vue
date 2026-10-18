<script setup>
import { onMounted, ref, watch } from 'vue'
import { postForm } from '../api'
import { toastErr } from '../ui'
import Icon from './Icon.vue'

const props = defineProps({ modelValue: { type: String, default: '' } })
const emit = defineEmits(['update:modelValue'])

const el = ref(null)
const imgEl = ref(null)
const focused = ref(false)

onMounted(() => { if (el.value) el.value.innerHTML = props.modelValue || '' })

watch(() => props.modelValue, (v) => {
  if (focused.value || !el.value) return
  if (el.value.innerHTML !== (v || '')) el.value.innerHTML = v || ''
})

function onInput() {
  emit('update:modelValue', el.value ? el.value.innerHTML : '')
}

function cmd(name, value = null) {
  if (!el.value) return
  el.value.focus()
  try { document.execCommand(name, false, value) } catch {}
  onInput()
}

async function insertImage(ev) {
  const f = ev.target.files[0]
  ev.target.value = ''
  if (!f) return
  const fd = new FormData()
  fd.append('file', f)
  try {
    const res = await postForm('/api/upload/image', fd)
    cmd('insertHTML', `<img src="${res.url}" alt="" />`)
  } catch (e) {
    toastErr(e)
  }
}

function addLink() {
  const url = window.prompt('链接地址（https:// 开头）')
  if (url) cmd('createLink', url)
}
</script>

<template>
  <div>
    <div class="editor-toolbar">
      <button type="button" title="加粗" @click="cmd('bold')"><b>B</b></button>
      <button type="button" title="斜体" @click="cmd('italic')"><i>I</i></button>
      <button type="button" title="下划线" @click="cmd('underline')"><u>U</u></button>
      <span class="sep"></span>
      <button type="button" title="大标题" @click="cmd('formatBlock', '<h2>')">H2</button>
      <button type="button" title="小标题" @click="cmd('formatBlock', '<h3>')">H3</button>
      <button type="button" title="正文" @click="cmd('formatBlock', '<p>')">正文</button>
      <span class="sep"></span>
      <button type="button" title="无序列表" @click="cmd('insertUnorderedList')">• 列表</button>
      <button type="button" title="有序列表" @click="cmd('insertOrderedList')">1. 列表</button>
      <button type="button" title="引用" @click="cmd('formatBlock', '<blockquote>')">引用</button>
      <span class="sep"></span>
      <button type="button" title="插入图片" @click="imgEl.click()"><Icon name="image" :size="15" /> 图片</button>
      <button type="button" title="插入链接" @click="addLink"><Icon name="link" :size="15" /> 链接</button>
      <button type="button" title="分割线" @click="cmd('insertHorizontalRule')">—</button>
      <span class="sep"></span>
      <button type="button" title="清除格式" @click="cmd('removeFormat')">清除格式</button>
    </div>
    <div ref="el" class="editor-area rich-view" contenteditable="true"
      @input="onInput" @focus="focused = true" @blur="focused = false"></div>
    <input ref="imgEl" type="file" accept="image/png,image/jpeg,image/gif,image/webp" hidden @change="insertImage" />
  </div>
</template>
