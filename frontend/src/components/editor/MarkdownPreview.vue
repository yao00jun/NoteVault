<script setup lang="ts">
import { computed, watch, nextTick, ref, onMounted, onBeforeUnmount } from 'vue'
import { marked } from 'marked'
import { sanitizeHtml } from '@/utils/sanitize'
import { useI18n } from 'vue-i18n'
import { useSettingsStore } from '@/stores/settings'
import { FileService } from '@bindings/github.com/notevault/notevault/index.js'

const props = defineProps<{
  content: string
  /**
   * 工作区根目录绝对路径。用于：
   *   - 嵌入 ![[note]] 异步读取目标笔记内容
   *   - 嵌入 ![[image.png]] 拼接图片 src
   * 不传时嵌入功能降级为占位提示，wiki-link 仍可解析。
   */
  workspacePath?: string
  /**
   * 当前笔记文件名（不含目录）。用于「复制块链接 / 标题链接」时拼 [[文件名^id]]。
   * 不传时复制的是同文件锚点形式 [[^id]] / [[#标题]]，插入别的笔记需手动补文件名。
   */
  currentFileName?: string
}>()

/**
 * wiki-link 点击事件载荷。
 * - file 为空表示同文件锚点 / 块引用（不需要切换文件）
 * - anchor 非空时，父组件应在文件加载后滚动到对应 heading
 * - block 非空时，父组件应滚动到 [data-block-id="..."] 元素
 * - raw 始终是 [[...]] 内部串原文，用于创建新文档场景的回退
 */
interface WikiLinkTarget {
  file: string
  anchor: string
  block: string
  raw: string
}

const emit = defineEmits<{
  'wiki-link-click': [target: WikiLinkTarget]
  /** 嵌入内容加载失败时触发，父组件可用来打日志或上报 */
  'embed-error': [info: { file: string; reason: string }]
  /** 复制块 / 标题链接后触发；ok=false 表示剪贴板写入失败，父组件可提示手动复制 */
  'anchor-copy': [info: { text: string; ok: boolean }]
}>()

const previewRef = ref<HTMLElement | null>(null)
const settingsStore = useSettingsStore()
const { t } = useI18n()
const previewStyle = computed(() => ({
  fontSize: `${settingsStore.settings.editor.previewFontSize}px`,
  lineHeight: String(settingsStore.settings.editor.lineHeight),
}))

// 配置 marked
marked.setOptions({
  breaks: true,
  gfm: true,
})

// ===== 链接解析工具 =====

/**
 * 把 [[...]] 内部串拆成 { file, anchor, block, alias } 四元组。
 * 与后端 internal/service/graphservice.go 的 parseWikiLinkTarget 保持同一口径。
 *
 * 顺序约定（与 Obsidian 一致）：
 *   1. 先剥别名 |xxx（别名内可能含 #、^ 字符，先剥避免误解析）
 *   2. 再剥块 ID ^xxx（取最后一个 ^，块 ID 仅在末尾）
 *   3. 最后剥锚点 #xxx（取第一个 #，标题可能含 # 但锚点引用取整段）
 *   4. 剩下的就是 file
 *
 * 例：
 *   "note#标题^blk|别名" → file=note, anchor=标题, block=blk, alias=别名
 *   "#标题"             → file="", anchor=标题, block="", alias=""
 *   "^blk"              → file="", anchor="", block=blk, alias=""
 */
function parseWikiLinkTarget(target: string): WikiLinkTarget & { alias: string } {
  let rest = target
  let alias = ''
  let block = ''
  let anchor = ''
  let file = ''

  const barIdx = rest.indexOf('|')
  if (barIdx >= 0) {
    alias = rest.slice(barIdx + 1).trim()
    rest = rest.slice(0, barIdx).trim()
  }
  const caretIdx = rest.lastIndexOf('^')
  if (caretIdx >= 0) {
    block = rest.slice(caretIdx + 1).trim()
    rest = rest.slice(0, caretIdx).trim()
  }
  const hashIdx = rest.indexOf('#')
  if (hashIdx >= 0) {
    anchor = rest.slice(hashIdx + 1).trim()
    rest = rest.slice(0, hashIdx).trim()
  }
  file = rest.trim()

  return { file, anchor, block, alias, raw: target }
}

/** 转义 HTML 特殊字符，防止 data-* 注入 */
function escapeHtml(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')
}

/**
 * 把标题文本转成 DOM id（GitHub 风格 slugify）。
 * marked 默认不生成 heading id，我们在渲染后用 DOM 操作补。
 */
function slugifyHeading(text: string): string {
  return text
    .trim()
    .toLowerCase()
    // 去掉所有标点（保留字母、数字、CJK、空格、横线）
    .replace(/[^\p{L}\p{N}\s-]/gu, '')
    .replace(/\s+/g, '-')
    .replace(/-+/g, '-')
}

// ===== 预处理 =====

// 嵌入 ![[...]] 必须在 wiki-link 之前处理，否则 [[...]] 会先被 wiki-link 正则吃掉
const embedRegex = /!\[\[([^\]\n]+)\]\]/g
// wiki-link [[...]]（不含 ! 前缀）
const wikiLinkRegex = /\[\[([^\]\n]+)\]\]/g
// 块 ID：独立成行的 ^id（前面是空行或文本行，紧跟行尾）
const blockIdRegex = /(^|\n)[ \t]*\^([a-zA-Z][\w-]*)[ \t]*(?=\n|$)/g
// ==高亮==
const highlightRegex = /==([^=\n][^\n]*?[^=\n])==/g

// Callout 类型 → 图标 + 颜色变体（对齐 Obsidian 别名体系）
interface CalloutMeta {
  icon: string
  /** CSS 颜色变体 class 后缀（.nv-callout-{variant}） */
  variant: string
}
const CALLOUT_META: Record<string, CalloutMeta> = {
  note: { icon: '📝', variant: 'blue' },
  abstract: { icon: '📄', variant: 'blue' },
  summary: { icon: '📄', variant: 'blue' },
  tldr: { icon: '📄', variant: 'blue' },
  info: { icon: 'ℹ️', variant: 'blue' },
  todo: { icon: '☑️', variant: 'blue' },
  tip: { icon: '💡', variant: 'green' },
  hint: { icon: '💡', variant: 'green' },
  important: { icon: '❗', variant: 'green' },
  success: { icon: '✅', variant: 'green' },
  check: { icon: '✅', variant: 'green' },
  done: { icon: '✅', variant: 'green' },
  question: { icon: '❓', variant: 'purple' },
  help: { icon: '🆘', variant: 'purple' },
  faq: { icon: '💬', variant: 'purple' },
  example: { icon: '📖', variant: 'purple' },
  warning: { icon: '⚠️', variant: 'amber' },
  caution: { icon: '⚠️', variant: 'amber' },
  attention: { icon: '⚠️', variant: 'amber' },
  failure: { icon: '❌', variant: 'red' },
  fail: { icon: '❌', variant: 'red' },
  missing: { icon: '❌', variant: 'red' },
  danger: { icon: '🔥', variant: 'red' },
  error: { icon: '⚠️', variant: 'red' },
  bug: { icon: '🐛', variant: 'red' },
  quote: { icon: '💬', variant: 'gray' },
  cite: { icon: '💬', variant: 'gray' },
}
// callout 标记正则：首行 `[!type]` / `[!type]-` 折叠 / `[!type]+` 展开 / `[!type] 自定义标题`
const calloutMarkerRegex = /^\[!([a-zA-Z]+)\]\s*([+-])?\s*(.*)$/

/** 预处理嵌入语法 ![[...]] */
function preprocessEmbeds(content: string): string {
  return content.replace(embedRegex, (_match, raw: string) => {
    const { file, anchor, block, alias } = parseWikiLinkTarget(raw)
    const imageExts = ['png', 'jpg', 'jpeg', 'gif', 'svg', 'webp', 'bmp', 'avif']
    const ext = (file.split('.').pop() ?? '').toLowerCase()

    // 图片嵌入：渲染为 <img>，src 由 DOM 阶段补（需要 workspacePath）
    if (imageExts.includes(ext) && file) {
      const alt = alias || file
      return `<img class="nv-embed nv-embed-image" data-embed-kind="image" data-embed-file="${escapeHtml(file)}" alt="${escapeHtml(alt)}" />`
    }

    // 非 markdown 嵌入（pdf 等）：暂不渲染内容，给个友好提示
    const otherExts = ['pdf']
    if (otherExts.includes(ext) && file) {
      return `<div class="nv-embed nv-embed-unsupported" data-embed-kind="unsupported" data-embed-file="${escapeHtml(file)}"><span class="nv-embed-label">${escapeHtml(t('editor.embedUnsupported'))}${escapeHtml(file)}</span></div>`
    }

    // markdown 嵌入：渲染占位 div，DOM 阶段异步拉取内容
    const placeholder = alias || anchor || block || file || raw.trim() || '?'
    const dataFile = file ? `data-embed-file="${escapeHtml(file)}"` : ''
    const dataAnchor = anchor ? `data-embed-anchor="${escapeHtml(anchor)}"` : ''
    const dataBlock = block ? `data-embed-block="${escapeHtml(block)}"` : ''
    return `<div class="nv-embed nv-embed-loading" data-embed-kind="markdown" ${dataFile} ${dataAnchor} ${dataBlock}><span class="nv-embed-label">${escapeHtml(t('editor.embedLoading'))}${escapeHtml(placeholder)}</span></div>`
  })
}

/** 预处理 wiki-link [[...]] */
function preprocessWikiLinks(content: string): string {
  return content.replace(wikiLinkRegex, (_match, raw: string) => {
    const { file, anchor, block, alias } = parseWikiLinkTarget(raw)
    // 显示文本优先级：alias > anchor > block > file > raw
    let display = alias || anchor || block || file || raw.trim()
    if (!display) display = '?'
    const dataFile = file ? `data-file="${escapeHtml(file)}"` : ''
    const dataAnchor = anchor ? `data-anchor="${escapeHtml(anchor)}"` : ''
    const dataBlock = block ? `data-block="${escapeHtml(block)}"` : ''
    return `<a class="wiki-link" href="#" ${dataFile} ${dataAnchor} ${dataBlock} data-raw="${escapeHtml(raw)}">${escapeHtml(display)}</a>`
  })
}

/** 预处理 ==高亮== 语法（Obsidian 风格）→ <mark>text</mark> */
function preprocessHighlights(content: string): string {
  return content.replace(highlightRegex, (_m, t) => `<mark class="nv-highlight">${t}</mark>`)
}

/**
 * 预处理块 ID：独立成行的 ^id → <a id="^id" class="nv-block-anchor" data-block-id="id"></a>
 *
 * 块 ID 在 Obsidian 里挂在它前一个 block 上（段落/列表/引用等）。
 * 我们用空 a 元素占位，跳转时 getElementById('^id') 即可滚动到对应位置。
 */
function preprocessBlockIDs(content: string): string {
  return content.replace(blockIdRegex, (_m, lead: string, id: string) => {
    // lead 是匹配的 ^ 或 \n，必须保留以免破坏后续行
    const title = escapeHtml(t('editor.copyBlockLink'))
    return `${lead}<a id="^${escapeHtml(id)}" class="nv-block-anchor" data-block-id="${escapeHtml(id)}" data-copy-block="${escapeHtml(id)}" title="${title}" aria-label="${title}">¶</a>`
  })
}

const html = computed(() => {
  try {
    let s = props.content || ''
    // 顺序：嵌入 → wiki-link → 高亮 → 块ID
    // 嵌入必须先于 wiki-link（同后端口径）
    s = preprocessEmbeds(s)
    s = preprocessWikiLinks(s)
    s = preprocessHighlights(s)
    s = preprocessBlockIDs(s)
    // XSS 防线：marked 透传原始 HTML，笔记里的 onerror 等会在此执行
    return sanitizeHtml(marked.parse(s) as string)
  } catch (e) {
    return '<p style="color:red">Markdown 解析错误</p>'
  }
})

// ===== DOM 后处理 =====

/**
 * 给所有 h1-h6 加 id（slugified），用于 [[note#heading]] 锚点跳转。
 * marked 默认不生成 heading id。
 */
function annotateHeadingIds(root: HTMLElement) {
  const headings = root.querySelectorAll('h1, h2, h3, h4, h5, h6')
  headings.forEach((h) => {
    const el = h as HTMLElement
    // 原文优先读 data-heading-text：注入 ¶ 按钮后 textContent 会带上 "¶"
    const text = ((el.dataset.headingText ?? el.textContent) || '').trim()
    if (!text) return
    const slug = slugifyHeading(text)
    if (slug && !el.id) {
      el.id = slug
    }
    el.dataset.headingText = text
    if (!el.querySelector('.nv-heading-anchor')) {
      const btn = document.createElement('span')
      btn.className = 'nv-heading-anchor'
      btn.setAttribute('data-copy-anchor', text)
      btn.textContent = '¶'
      btn.title = t('editor.copyHeadingLink')
      el.appendChild(btn)
    }
  })
}

/**
 * 把 Markdown 渲染后的 blockquote 中的 Obsidian Callout 语法
 * （`> [!type]` / `> [!type] 标题` / `> [!type]-` 折叠 / `> [!type]+` 展开）
 * 转换为原生 <details> 结构，无需额外 JS 即可折叠。
 *
 * 为什么用 DOM 后处理而非预处理：marked 会把 `>` 渲染成 <blockquote>，
 * 直接在源码层替换成 <div> 会被 marked 的 HTML 块规则破坏（遇到空行断块）。
 * 后处理直接改已生成的 DOM，最稳。
 */
function transformCallouts(root: HTMLElement) {
  // 静态 NodeList：处理外层时会把内层 blockquote 移入 details，但引用仍有效
  const blockquotes = Array.from(root.querySelectorAll('blockquote'))
  for (const bq of blockquotes) {
    const first = bq.firstElementChild as HTMLElement | null
    if (!first) continue
    // marked 会把连续的 `>` 行合并成同一 <p>（软换行 → <br>）。
    // <br> 不会进入 textContent，所以改从 innerHTML 解析标记：
    // 标记一定在段首，自定义标题在第一个 <br> 之前结束。
    const inner = first.innerHTML || ''
    const m = /^\[!([a-zA-Z]+)\]([-+]?)\s*([\s\S]*?)(?=<br\s*\/?>|$)/i.exec(inner)
    if (!m) continue // 普通引用，保持原样

    const type = m[1].toLowerCase()
    const fold = m[2] || ''
    const customTitle = (m[3] || '').trim()
    const meta = CALLOUT_META[type] ?? { icon: '📝', variant: 'blue' }
    const expanded = fold !== '-' // 默认 / + 都是展开，- 折叠

    // 标题：自定义优先 → i18n → type 首字母大写兜底
    let title = customTitle
    if (!title) {
      const key = `editor.calloutTitle.${type}`
      const translated = t(key)
      title = translated && translated !== key ? translated : type.charAt(0).toUpperCase() + type.slice(1)
    }

    // 去掉首段的标记文本（含其后的 <br>），剩余作为正文
    const bodyHtml = inner.slice(m[0].length).replace(/^<br\s*\/?>/i, '')
    if (bodyHtml.trim() === '') {
      bq.removeChild(first)
    } else {
      first.innerHTML = bodyHtml
    }

    const details = document.createElement('details')
    details.className = `nv-callout nv-callout-${meta.variant}`
    if (expanded) details.open = true

    const summary = document.createElement('summary')
    summary.className = 'nv-callout-summary'
    const icon = document.createElement('span')
    icon.className = 'nv-callout-icon'
    icon.textContent = meta.icon
    const titleEl = document.createElement('span')
    titleEl.className = 'nv-callout-title'
    titleEl.textContent = title
    summary.appendChild(icon)
    summary.appendChild(titleEl)

    const body = document.createElement('div')
    body.className = 'nv-callout-body'
    while (bq.firstChild) body.appendChild(bq.firstChild)

    details.appendChild(summary)
    details.appendChild(body)
    bq.replaceWith(details)
  }
}

/**
 * 点击事件委托：绑在根容器上一次，永久捕获所有 .wiki-link 点击（含嵌入内嵌套的）。
 * 比每次 html 变化时重新 addEventListener 更稳，且测试 mount 完即可触发。
 */
function handleClickDelegation(e: MouseEvent) {
  const root = previewRef.value
  if (!root) return
  // ¶ 复制按钮优先：它嵌在 heading 内部，但不属于 wiki-link
  const copyBtn = (e.target as HTMLElement | null)?.closest<HTMLElement>('[data-copy-block],[data-copy-anchor]')
  if (copyBtn && root.contains(copyBtn)) {
    e.preventDefault()
    e.stopPropagation()
    void handleCopyAnchorClick(copyBtn)
    return
  }
  const target = (e.target as HTMLElement | null)?.closest<HTMLElement>('.wiki-link')
  if (!target || !root.contains(target)) return
  e.preventDefault()
  const file = target.getAttribute('data-file') || ''
  const anchor = target.getAttribute('data-anchor') || ''
  const block = target.getAttribute('data-block') || ''
  const raw = target.getAttribute('data-raw') || ''
  // 同文件锚点跳转：file 为空时直接滚动
  if (!file && (anchor || block)) {
    scrollToAnchor(root, anchor, block)
    return
  }
  emit('wiki-link-click', { file, anchor, block, raw })
}

/**
 * 拼接可粘贴的 wiki-link 引用串：
 *   有文件名 → [[note#标题]] / [[note^blk1]]
 *   无文件名 → [[#标题]] / [[^blk1]]（同文件内引用）
 */
function buildWikiLinkRef(anchor: string, block: string): string {
  const ref = block ? `^${block}` : anchor ? `#${anchor}` : ''
  if (!ref) return ''
  const base = (props.currentFileName || '').trim()
  return base ? `[[${base}${ref}]]` : `[[${ref}]]`
}

/** 写剪贴板：优先 async clipboard API，失败降级 execCommand（http 环境 / 老内核） */
async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    // 权限被拒或非安全上下文，走降级
  }
  try {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.style.position = 'fixed'
    ta.style.top = '-9999px'
    document.body.appendChild(ta)
    ta.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(ta)
    return ok
  } catch {
    return false
  }
}

/** ¶ 点击后的短暂视觉反馈：¶ → ✓，1.2s 后复原 */
function flashCopied(el: HTMLElement) {
  const original = el.textContent
  el.textContent = '✓'
  el.classList.add('nv-anchor-copied')
  window.setTimeout(() => {
    el.textContent = original
    el.classList.remove('nv-anchor-copied')
  }, 1200)
}

/** 处理 ¶ 复制按钮点击（块锚点与标题锚点两种） */
async function handleCopyAnchorClick(el: HTMLElement) {
  const block = el.getAttribute('data-copy-block') || ''
  const anchor = el.getAttribute('data-copy-anchor') || ''
  const text = buildWikiLinkRef(anchor, block)
  if (!text) return
  const ok = await copyText(text)
  emit('anchor-copy', { text, ok })
  flashCopied(el)
}

/** 滚动到当前预览内的锚点 / 块 */
function scrollToAnchor(root: HTMLElement, anchor: string, block: string): boolean {
  if (block) {
    const el = root.querySelector(`[data-block-id="${cssEscape(block)}"]`) as HTMLElement | null
    if (el) {
      el.scrollIntoView({ behavior: 'smooth', block: 'start' })
      return true
    }
  }
  if (anchor) {
    // 先按 slugified id 找
    const slug = slugifyHeading(anchor)
    let el = slug ? (root.querySelector(`#${CSS.escape(slug)}`) as HTMLElement | null) : null
    // 再按原始文本找 heading（data-heading-text 存原文，排除 ¶ 按钮的干扰）
    if (!el) {
      const headings = Array.from(root.querySelectorAll('h1, h2, h3, h4, h5, h6'))
      for (const h of headings) {
        const raw = (h as HTMLElement).dataset.headingText ?? (h.textContent || '').replace(/¶$/, '')
        if (raw.trim() === anchor.trim()) {
          el = h as HTMLElement
          break
        }
      }
    }
    if (el) {
      el.scrollIntoView({ behavior: 'smooth', block: 'start' })
      return true
    }
  }
  return false
}

function cssEscape(s: string): string {
  try {
    return CSS.escape(s)
  } catch {
    return s.replace(/["\\]/g, '\\$&')
  }
}

/**
 * 异步加载嵌入：扫描 .nv-embed[data-embed-kind="markdown"]，调 FileService.ReadFile 拉内容，
 * 递归渲染（限制嵌套深度 1 层，防自循环）。
 */
async function loadEmbeds(root: HTMLElement, depth = 0) {
  if (!props.workspacePath) return
  if (depth > 1) return // 嵌套深度限制

  const embeds = Array.from(root.querySelectorAll<HTMLElement>('.nv-embed[data-embed-kind="markdown"]'))
  for (const el of embeds) {
    if (el.dataset.loaded === '1') continue
    const file = el.getAttribute('data-embed-file') || ''
    const anchor = el.getAttribute('data-embed-anchor') || ''
    if (!file) continue
    el.dataset.loaded = '1'
    try {
      const content = await FileService.ReadFile(props.workspacePath, resolveEmbedPath(file))
      if (typeof content !== 'string' || !content) {
        showEmbedError(el, file, 'empty')
        continue
      }
      const sliced = sliceByAnchor(content, anchor)
      // 渲染：复用同样的预处理管道
      let s = preprocessEmbeds(sliced)
      s = preprocessWikiLinks(s)
      s = preprocessHighlights(s)
      s = preprocessBlockIDs(s)
      const inner = sanitizeHtml(marked.parse(s) as string)
      el.classList.remove('nv-embed-loading')
      el.classList.add('nv-embed-loaded')
      el.innerHTML = `<div class="nv-embed-header"><span class="nv-embed-link">${escapeHtml(file)}${anchor ? '#' + escapeHtml(anchor) : ''}</span></div><div class="nv-embed-body">${inner}</div>`
      // 标记嵌套层
      const nested = el.querySelectorAll<HTMLElement>('.nv-embed[data-embed-kind="markdown"]')
      nested.forEach((n) => {
        if (depth + 1 > 1) {
          // 超过深度限制，转为占位提示
          n.classList.remove('nv-embed-loading')
          n.classList.add('nv-embed-skipped')
          const label = n.querySelector('.nv-embed-label')
          const nestedFile = n.getAttribute('data-embed-file') || ''
          if (label) label.textContent = t('editor.embedNestedTooDeep', { file: nestedFile })
        }
      })
      // 继续递归（但下一层会被 depth>1 拦截）
      await loadEmbeds(el, depth + 1)
      // 嵌入内的 wiki-link 不需单独绑事件，根级委托会捕获冒泡
      // 嵌入内的 heading 也要加 id（避免与外层冲突，前缀 embed-）
      annotateEmbedHeadingIds(el, `${file}#`)
      // 嵌入内的 callout 也要渲染
      transformCallouts(el)
    } catch (e) {
      showEmbedError(el, file, (e as Error)?.message || 'unknown')
      emit('embed-error', { file, reason: (e as Error)?.message || 'unknown' })
    }
  }
}

/** 嵌入内的 heading id 加前缀避免与外层冲突 */
function annotateEmbedHeadingIds(root: HTMLElement, prefix: string) {
  const headings = root.querySelectorAll('h1, h2, h3, h4, h5, h6')
  headings.forEach((h) => {
    const text = (h.textContent || '').trim()
    if (!text) return
    const slug = slugifyHeading(text)
    if (slug) {
      h.id = `embed-${slugifyHeading(prefix)}-${slug}`
    }
  })
}

function showEmbedError(el: HTMLElement, file: string, reason: string) {
  el.classList.remove('nv-embed-loading')
  el.classList.add('nv-embed-error')
  el.innerHTML = `<span class="nv-embed-error-msg">${escapeHtml(t('editor.embedFailed', { file, reason }))}</span>`
}

/**
 * 把 [[xxx]] 里的 file 解析为相对路径。
 * 当前实现：xxx 可能是 note / note.md / folder/note，
 * 后端 FileService.ReadFile 接受相对路径，直接透传即可。
 * 若找不到，FileService 会返回错误，由 showEmbedError 处理。
 */
function resolveEmbedPath(file: string): string {
  // 兼容 note 与 note.md 两种写法：ReadFile 会处理路径校验，
  // 这里只确保不传绝对路径（防穿越）
  if (file.startsWith('/') || file.includes('..')) return file // 让后端拒绝
  // 如果没有扩展名，补 .md
  if (!/\.[a-z0-9]+$/i.test(file)) return file + '.md'
  return file
}

/**
 * 按 #anchor 切片嵌入内容：返回对应章节。
 * 简单实现：按 ## / ### 切，包含从该标题到下一同级或更高级标题前的内容。
 */
function sliceByAnchor(content: string, anchor: string): string {
  if (!anchor) return content
  const lines = content.split('\n')
  let startIdx = -1
  let startLevel = 0
  for (let i = 0; i < lines.length; i++) {
    const m = /^(#{1,6})\s+(.+?)\s*$/.exec(lines[i])
    if (m) {
      const level = m[1].length
      const text = m[2].trim()
      if (text === anchor || slugifyHeading(text) === slugifyHeading(anchor)) {
        startIdx = i
        startLevel = level
        break
      }
    }
  }
  if (startIdx < 0) return content // 找不到就嵌入全文
  let endIdx = lines.length
  for (let i = startIdx + 1; i < lines.length; i++) {
    const m = /^(#{1,6})\s+/.exec(lines[i])
    if (m && m[1].length <= startLevel) {
      endIdx = i
      break
    }
  }
  return lines.slice(startIdx, endIdx).join('\n')
}

/** 补全嵌入图片的 src（需要 workspacePath 拼接 file:// 或 wails 协议） */
function hydrateEmbedImages(root: HTMLElement) {
  if (!props.workspacePath) return
  const imgs = root.querySelectorAll<HTMLImageElement>('.nv-embed-image')
  imgs.forEach((img) => {
    if (img.dataset.hydrated === '1') return
    const file = img.getAttribute('data-embed-file') || ''
    if (!file) return
    img.dataset.hydrated = '1'
    // Wails 文件协议：通过 FileService.ReadFile 拿到 base64 太重，
    // 简单做法是用 wails 的 file:// 协议。但跨平台路径分隔符问题，
    // 这里改为把图片当 markdown 嵌入处理：读出来 base64 内联。
    // 临时方案：标记 src 留空，由用户点击 placeholder 触发加载。
    // 更稳的做法：直接拼接 file:// URL（Wails WebView 支持）
    const abs = `${props.workspacePath}/${file}`.replace(/\\/g, '/')
    img.src = `file:///${abs.replace(/^\//, '')}`
  })
}

function refreshDom() {
  const root = previewRef.value
  if (!root) return
  annotateHeadingIds(root)
  transformCallouts(root)
  // wiki-link 点击已用根级委托，无需逐个绑
  hydrateEmbedImages(root)
  // 嵌入是异步的，不阻塞渲染
  void loadEmbeds(root, 0)
}

watch(html, () => {
  nextTick(refreshDom)
}, { immediate: true })

onMounted(() => {
  // 事件委托：根级监听器，永久捕获 .wiki-link 点击
  const root = previewRef.value
  if (root) {
    root.addEventListener('click', handleClickDelegation)
  }
  nextTick(refreshDom)
})

onBeforeUnmount(() => {
  const root = previewRef.value
  if (root) {
    root.removeEventListener('click', handleClickDelegation)
  }
})
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

/* ¶ 复制按钮：默认隐形，hover 标题/块时才浮现，避免干扰阅读 */
.markdown-preview :deep(.nv-heading-anchor),
.markdown-preview :deep(.nv-block-anchor[data-copy-block]) {
  margin-left: 0.35em;
  font-size: 0.8em;
  font-weight: 400;
  color: var(--text-muted);
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.12s ease, color 0.12s ease;
  user-select: none;
  text-decoration: none;
}

.markdown-preview :deep(h1:hover .nv-heading-anchor),
.markdown-preview :deep(h2:hover .nv-heading-anchor),
.markdown-preview :deep(h3:hover .nv-heading-anchor),
.markdown-preview :deep(h4:hover .nv-heading-anchor),
.markdown-preview :deep(h5:hover .nv-heading-anchor),
.markdown-preview :deep(h6:hover .nv-heading-anchor),
.markdown-preview :deep(.nv-block-anchor[data-copy-block]:hover) {
  opacity: 1;
}

.markdown-preview :deep(.nv-heading-anchor:hover),
.markdown-preview :deep(.nv-block-anchor[data-copy-block]:hover) {
  color: var(--accent, var(--text-primary));
}

/* 复制成功反馈 */
.markdown-preview :deep(.nv-anchor-copied) {
  opacity: 1;
  color: var(--success, #4caf50);
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

/* wiki-link */
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

/* ==高亮== */
.markdown-preview :deep(mark.nv-highlight) {
  background: rgba(245, 217, 10, 0.45);
  color: inherit;
  padding: 0 2px;
  border-radius: 3px;
}

/* 块 ID 锚点：不可见的占位元素，仅作为滚动目标 */
.markdown-preview :deep(.nv-block-anchor) {
  display: inline-block;
  width: 0;
  height: 0;
  overflow: hidden;
  visibility: hidden;
}

/* 嵌入通用 */
.markdown-preview :deep(.nv-embed) {
  margin: var(--space-3) 0;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  overflow: hidden;
}

.markdown-preview :deep(.nv-embed-loading) {
  padding: var(--space-3);
  background: var(--bg-card);
  color: var(--text-secondary);
  font-style: italic;
  font-size: 0.92em;
}

.markdown-preview :deep(.nv-embed-loaded) {
  background: var(--bg-card);
}

.markdown-preview :deep(.nv-embed-header) {
  padding: var(--space-1) var(--space-3);
  font-size: 0.85em;
  color: var(--text-secondary);
  background: var(--bg);
  border-bottom: 1px solid var(--border);
  font-family: var(--font-mono, monospace);
}

.markdown-preview :deep(.nv-embed-link) {
  color: var(--accent);
}

.markdown-preview :deep(.nv-embed-body) {
  padding: 0 var(--space-3) var(--space-2);
}

/* 嵌入内段落缩进减半，视觉上区分 */
.markdown-preview :deep(.nv-embed-body p) {
  margin: var(--space-2) 0;
}

.markdown-preview :deep(.nv-embed-error) {
  padding: var(--space-2) var(--space-3);
  background: rgba(220, 38, 38, 0.08);
  color: #b91c1c;
  font-size: 0.9em;
}

.markdown-preview :deep(.nv-embed-unsupported) {
  padding: var(--space-2) var(--space-3);
  background: var(--bg-card);
  color: var(--text-secondary);
  font-size: 0.9em;
  font-style: italic;
}

.markdown-preview :deep(.nv-embed-skipped .nv-embed-label) {
  color: var(--text-secondary);
  font-style: italic;
}

/* 嵌入图片 */
.markdown-preview :deep(.nv-embed-image) {
  display: block;
  margin: var(--space-3) 0;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  padding: var(--space-2);
  background: var(--bg-card);
}

/* 插件注入的 <span class="spoiler"> */
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

/* ===== Callout ===== */
.markdown-preview :deep(.nv-callout) {
  margin: var(--space-3) 0;
  border: 1px solid var(--callout-color, var(--accent));
  border-left-width: 4px;
  border-radius: var(--radius-md);
  background: var(--callout-bg, var(--bg-card));
  overflow: hidden;
}
.markdown-preview :deep(.nv-callout > summary) {
  list-style: none;
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-3);
  font-weight: 600;
  color: var(--callout-color, var(--text-primary));
  background: var(--callout-title-bg, transparent);
  user-select: none;
}
.markdown-preview :deep(.nv-callout > summary::-webkit-details-marker) {
  display: none;
}
.markdown-preview :deep(.nv-callout-icon) {
  font-size: 1.05em;
  line-height: 1;
}
.markdown-preview :deep(.nv-callout-title) {
  flex: 1;
}
.markdown-preview :deep(.nv-callout-body) {
  padding: var(--space-1) var(--space-4) var(--space-3);
}
.markdown-preview :deep(.nv-callout-body > :first-child) {
  margin-top: var(--space-2);
}
.markdown-preview :deep(.nv-callout-body > :last-child) {
  margin-bottom: 0;
}

/* 颜色变体（对齐 Obsidian 语义色） */
.markdown-preview :deep(.nv-callout-note) {
  --callout-color: var(--accent, #3b82f6);
}
.markdown-preview :deep(.nv-callout-blue) {
  --callout-color: #3b82f6;
  --callout-bg: rgba(59, 130, 246, 0.07);
}
.markdown-preview :deep(.nv-callout-green) {
  --callout-color: #16a34a;
  --callout-bg: rgba(22, 163, 74, 0.07);
}
.markdown-preview :deep(.nv-callout-purple) {
  --callout-color: #9333ea;
  --callout-bg: rgba(147, 51, 234, 0.07);
}
.markdown-preview :deep(.nv-callout-amber) {
  --callout-color: #d97706;
  --callout-bg: rgba(217, 119, 6, 0.08);
}
.markdown-preview :deep(.nv-callout-red) {
  --callout-color: #dc2626;
  --callout-bg: rgba(220, 38, 38, 0.07);
}
.markdown-preview :deep(.nv-callout-gray) {
  --callout-color: #6b7280;
  --callout-bg: rgba(107, 114, 128, 0.07);
}
</style>
