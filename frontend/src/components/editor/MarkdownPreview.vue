<script setup lang="ts">
import { computed, watch, nextTick, ref } from 'vue'
import { marked } from 'marked'
import { useSettingsStore } from '@/stores/settings'

const props = defineProps<{
  content: string
}>()

const emit = defineEmits<{
  'wiki-link-click': [link: string]
}>()

const previewRef = ref<HTMLElement | null>(null)
const settingsStore = useSettingsStore()
const previewStyle = computed(() => ({
  fontSize: `${settingsStore.settings.editor.previewFontSize}px`,
  lineHeight: String(settingsStore.settings.editor.lineHeight),
}))

// 配置 marked
marked.setOptions({
  breaks: true,
  gfm: true,
})

// 预处理 [[wiki-link]] 语法，转换为特殊链接
function preprocessWikiLinks(content: string): string {
  return content.replace(/\[\[([^\]]+)\]\]/g, (match, link) => {
    const displayText = link.includes('|') ? link.split('|')[1] : link
    const target = link.includes('|') ? link.split('|')[0] : link
    return `<a class="wiki-link" href="#" data-target="${target.trim()}">${displayText.trim()}</a>`
  })
}

// 预处理 ==text== 高亮语法（Obsidian 风格）→ <mark>text</mark>，marked 不内置支持
// 排除含 = 或换行的内容，避免误匹配 `a == b` 这类普通文本
function preprocessHighlights(content: string): string {
  return content.replace(/==([^=\n][^\n]*?[^=\n])==/g, (_m, t) => `<mark class="nv-highlight">${t}</mark>`)
}

const html = computed(() => {
  try {
    const processed = preprocessHighlights(preprocessWikiLinks(props.content || ''))
    return marked.parse(processed) as string
  } catch (e) {
    return '<p style="color:red">Markdown 解析错误</p>'
  }
})

// 绑定 wiki-link 点击事件
function bindWikiLinkClicks() {
  if (!previewRef.value) return
  const links = previewRef.value.querySelectorAll('.wiki-link')
  links.forEach((link) => {
    link.addEventListener('click', (e) => {
      e.preventDefault()
      const target = link.getAttribute('data-target')
      if (target) {
        emit('wiki-link-click', target)
      }
    })
  })
}

watch(html, () => {
  nextTick(() => {
    bindWikiLinkClicks()
  })
}, { immediate: true })
</script>

<template>
  <div
    ref="previewRef"
    class="markdown-preview"
    :style="previewStyle"
    v-html="html"
  />
</template>

<style scoped>
.markdown-preview {
  width: 100%;
  height: 100%;
  overflow-y: auto;
  padding: var(--space-6);
  color: var(--text-primary);
  line-height: 1.6;
  font-size: var(--text-base);
}

.markdown-preview :deep(h1) {
  font-size: 1.8em;
  font-weight: 700;
  margin: 0 0 var(--space-4);
  padding-bottom: var(--space-2);
  border-bottom: 2px solid var(--border);
}

.markdown-preview :deep(h2) {
  font-size: 1.5em;
  font-weight: 600;
  margin: var(--space-6) 0 var(--space-3);
  padding-bottom: var(--space-2);
  border-bottom: 1px solid var(--border);
}

.markdown-preview :deep(h3) {
  font-size: 1.25em;
  font-weight: 600;
  margin: var(--space-5) 0 var(--space-2);
}

.markdown-preview :deep(h4),
.markdown-preview :deep(h5),
.markdown-preview :deep(h6) {
  font-weight: 600;
  margin: var(--space-4) 0 var(--space-2);
}

.markdown-preview :deep(p) {
  margin: 0 0 var(--space-3);
}

.markdown-preview :deep(ul),
.markdown-preview :deep(ol) {
  margin: 0 0 var(--space-3);
  padding-left: var(--space-6);
}

.markdown-preview :deep(li) {
  margin-bottom: var(--space-1);
}

.markdown-preview :deep(blockquote) {
  margin: 0 0 var(--space-3);
  padding: var(--space-2) var(--space-4);
  border-left: 4px solid var(--accent);
  background: var(--bg-card);
  color: var(--text-secondary);
  border-radius: 0 var(--radius-sm) var(--radius-sm) 0;
}

.markdown-preview :deep(code) {
  background: var(--bg-card);
  padding: 2px 6px;
  border-radius: var(--radius-sm);
  font-family: var(--font-mono, "JetBrains Mono", Consolas, monospace);
  font-size: 0.9em;
  color: var(--accent);
}

.markdown-preview :deep(pre) {
  background: var(--bg-card);
  padding: var(--space-4);
  border-radius: var(--radius-md);
  overflow-x: auto;
  margin: 0 0 var(--space-3);
  border: 1px solid var(--border);
}

.markdown-preview :deep(pre code) {
  background: transparent;
  padding: 0;
  color: var(--text-primary);
}

.markdown-preview :deep(table) {
  width: 100%;
  border-collapse: collapse;
  margin: 0 0 var(--space-3);
}

.markdown-preview :deep(th),
.markdown-preview :deep(td) {
  border: 1px solid var(--border);
  padding: var(--space-2) var(--space-3);
  text-align: left;
}

.markdown-preview :deep(th) {
  background: var(--bg-card);
  font-weight: 600;
}

.markdown-preview :deep(a) {
  color: var(--accent);
  text-decoration: none;
}

.markdown-preview :deep(a:hover) {
  text-decoration: underline;
}

.markdown-preview :deep(img) {
  max-width: 100%;
  border-radius: var(--radius-md);
}

.markdown-preview :deep(hr) {
  border: none;
  border-top: 1px solid var(--border);
  margin: var(--space-6) 0;
}

.markdown-preview :deep(input[type="checkbox"]) {
  margin-right: var(--space-2);
}

.markdown-preview :deep(.wiki-link) {
  color: var(--accent);
  background: var(--accent-alpha);
  padding: 1px 6px;
  border-radius: 4px;
  text-decoration: none;
  cursor: pointer;
  border-bottom: 1px dashed var(--accent);
}

.markdown-preview :deep(.wiki-link:hover) {
  background: var(--accent);
  color: white;
  text-decoration: none;
}

/* ==高亮== 预处理为 <mark class="nv-highlight">，给个黄色背景 */
.markdown-preview :deep(mark.nv-highlight) {
  background: rgba(245, 217, 10, 0.45);
  color: inherit;
  padding: 0 2px;
  border-radius: 3px;
}

/* 插件注入的 <span class="spoiler">：默认模糊，悬停显示 */
.markdown-preview :deep(.spoiler) {
  background: var(--text-primary);
  color: transparent;
  border-radius: 3px;
  padding: 0 2px;
  cursor: pointer;
  transition: background 0.18s ease, color 0.18s ease;
  user-select: none;
}
.markdown-preview :deep(.spoiler:hover) {
  background: transparent;
  color: inherit;
}
</style>
