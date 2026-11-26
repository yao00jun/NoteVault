/**
 * useEditorAnchorNavigation - 锚点跳转（[[note#标题]] / ^块 / QnA 引用 / 大纲）
 *
 * 这组逻辑的共同点：**跳转目标要等预览渲染完才存在**，所以不能同步做完。
 * 三条入口殊途同归到同一个 pendingAnchor：
 *   - wiki-link 点击（可能跨文件，也可能目标标签页已打开）
 *   - 路由 ?anchor=（QnA / 全局检索的引用跳转）
 *   - 大纲里点标题（同文件，不换标签）
 *
 * 时序契约（由 EditorView.anchorTiming.test.ts 锁定）：
 *   1. 挂上 pendingAnchor → 切文件时由 watch(activePath) 在 nextTick 后等一帧渲染；
 *   2. 第一帧没找到目标（DOM 还没出来）→ 再等一帧；
 *   3. 第二帧仍没有 → 清空 pending 并提示「锚点不存在」，绝不无限重试；
 *   4. 成功后立刻清空 pending，否则下次切标签会被「粘」回去。
 *
 * 为什么 pendingAnchor 必须清空：它挂在组件实例上（keep-alive 下长期存活），
 * 残留会导致用户在别处切标签时莫名滚动。
 */
import { nextTick, ref, watch, type ComputedRef, type Ref } from 'vue'
import type { RouteLocationNormalizedLoaded, Router } from 'vue-router'
import { i18n } from '@/i18n'
import { useToast } from '@/composables/useToast'
import { confirmDialog } from '@/composables/useConfirm'
import type { FileNode } from '@/components/editor/FileTree.vue'
import { findFileByName } from '@/utils/wikiLinkFiles'
import { normalizeNotePath } from '@/utils/navigation'
import { cssEscape, slugifyHeading } from '@/utils/markdownText'

export interface WikiLinkClickPayload {
  file: string
  anchor: string
  block: string
  raw: string
}

export function useEditorAnchorNavigation(options: {
  workspacePath: ComputedRef<string | undefined>
  fileTree: Ref<FileNode[]>
  /** 当前活动标签页的相对路径（无标签为 undefined） */
  activePath: ComputedRef<string | undefined>
  findTabIndex: (path: string) => number
  openFile: (node: FileNode) => Promise<void>
  openFileByPath: (path: string, fromRoute?: boolean) => Promise<void>
  /** 目标文档不存在时，按 [[链接名]] 新建文件的副作用 */
  createLinkedNote: (fileName: string) => Promise<void>
  route: RouteLocationNormalizedLoaded
  router: Router
}) {
  const t = i18n.global.t
  const toast = useToast()

  const pendingAnchor = ref<{ anchor: string; block: string } | null>(null)

  /** 当前可见的预览容器（编辑器模式下预览是 v-show 隐藏，仍在 DOM 里） */
  function previewRoot(): HTMLElement | null {
    return document.querySelector('.markdown-preview') as HTMLElement | null
  }

  /**
   * 尝试滚动到 pendingAnchor。成功会清空 pending，失败保留（交给下一帧重试）。
   * 返回值表示「这一次是否找到并滚动了」。
   */
  function tryScrollToPendingAnchor(): boolean {
    if (!pendingAnchor.value) return false
    const { anchor, block } = pendingAnchor.value
    const root = previewRoot()
    if (!root) return false
    let ok = false
    if (block) {
      const el = root.querySelector(`[data-block-id="${cssEscape(block)}"]`) as HTMLElement | null
      if (el) {
        el.scrollIntoView({ behavior: 'smooth', block: 'start' })
        ok = true
      }
    }
    if (!ok && anchor) {
      const slug = slugifyHeading(anchor)
      let el: HTMLElement | null = slug ? (root.querySelector(`#${cssEscape(slug)}`) as HTMLElement | null) : null
      if (!el) {
        const headings = Array.from(root.querySelectorAll('h1, h2, h3, h4, h5, h6'))
        for (const h of headings) {
          // 优先读 data-heading-text（预览注入 ¶ 后 textContent 会带 "¶"）
          const raw = (h as HTMLElement).dataset.headingText ?? (h.textContent || '').replace(/¶$/, '')
          if (raw.trim() === anchor.trim()) {
            el = h as HTMLElement
            break
          }
        }
      }
      if (el) {
        el.scrollIntoView({ behavior: 'smooth', block: 'start' })
        ok = true
      }
    }
    if (ok) pendingAnchor.value = null
    return ok
  }

  /**
   * 放弃重试：清空 pending 并告诉用户目标不存在。
   * 必须清空——否则它会一直挂着，下次切标签页时误触发跳转。
   */
  function resolvePendingAnchor() {
    if (!pendingAnchor.value) return
    if (tryScrollToPendingAnchor()) return
    const { anchor, block } = pendingAnchor.value
    pendingAnchor.value = null
    const label = block ? `^${block}` : anchor
    if (label) toast.warning(t('editor.anchorNotFound', { anchor: label }))
  }

  /** 等 DOM 更新后重试两帧；两帧都不中即放弃（见文件头时序契约） */
  async function settlePendingAnchor() {
    await nextTick()
    requestAnimationFrame(() => {
      if (tryScrollToPendingAnchor()) return
      requestAnimationFrame(() => resolvePendingAnchor())
    })
  }

  async function handleWikiLinkClick(target: WikiLinkClickPayload) {
    if (!options.workspacePath.value) return
    const { file, anchor, block } = target

    // 同文件锚点：MarkdownPreview 自己已处理滚动，正常不会传到这层。
    // 保险起见仍记下 pending，让两帧重试兜底。
    if (!file) {
      if (anchor || block) {
        pendingAnchor.value = { anchor, block }
        tryScrollToPendingAnchor()
      }
      return
    }

    const targetFile = findFileByName(options.fileTree.value, file)
    if (!targetFile) {
      // 找不到：问用户要不要按链接名建一篇（新建不带锚点）
      if (await confirmDialog({ message: t('editor.createLinkDoc', { name: file }) })) {
        await options.createLinkedNote(file.endsWith('.md') ? file : file + '.md')
      }
      return
    }

    // 已在标签页 → 切换后预览 DOM 早已渲染，同步尝试即可，失败就是锚点真不存在
    const alreadyOpen = options.findTabIndex(targetFile.path) >= 0
    if (anchor || block) pendingAnchor.value = { anchor, block }
    await options.openFile(targetFile)
    if (alreadyOpen && (anchor || block)) resolvePendingAnchor()
  }

  /**
   * ¶ 复制块 / 标题链接后的回调。
   * 剪贴板不可用（非安全上下文或权限被拒）时也要有反馈，
   * 否则用户点了 ¶ 什么都没发生会以为是 bug。
   */
  function handleAnchorCopy(info: { text: string; ok: boolean }) {
    if (info.ok) {
      toast.success(t('editor.linkCopied', { text: info.text }))
      return
    }
    // 写不进剪贴板：文本打进 console 兜底，避免内容彻底丢失
    console.warn('[anchor-copy] clipboard unavailable, link text:', info.text)
    toast.error(t('editor.copyFallbackPrompt'), 6000)
  }

  /**
   * QnA / 全局检索的引用跳转。route.query.anchor 是目标标题文本，
   * 转成 pendingAnchor 复用同一套滚动机制；anchor 缺省时等价 openFileByPath。
   */
  async function openFileWithAnchor(filePath: string, fromRoute = false) {
    const raw = options.route.query.anchor
    const anchor = typeof raw === 'string' ? raw.trim() : ''
    if (!anchor) {
      await options.openFileByPath(filePath, fromRoute)
      return
    }
    // 目标本来就是活动标签时 openFileByPath 不会改变 activeTabIndex，
    // watch(activePath) 不触发 → 必须同步兜底，否则跳转静默失效。
    const alreadyActive = options.activePath.value === normalizeNotePath(filePath)
    pendingAnchor.value = { anchor, block: '' }
    await options.openFileByPath(filePath, fromRoute)
    // 摘除 anchor 放在打开之后：一次跳转只生效一次，避免用户滚开后又被「粘」回锚点；
    // 且此时重入的 watcher 会因「文件已打开且无锚点」提前返回，不会重复打开
    if (options.route.query.anchor !== undefined) {
      await options.router.replace({ path: options.route.path, query: { ...options.route.query, anchor: undefined } })
    }
    if (alreadyActive) await settlePendingAnchor()
  }

  /** 已在目标文档上、只带 anchor（同一文件内换章节）时不必重开文件，直接滚 */
  async function scrollToAnchor(heading: string) {
    pendingAnchor.value = { anchor: heading, block: '' }
    if (options.route.query.anchor !== undefined) {
      await options.router.replace({ path: options.route.path, query: { ...options.route.query, anchor: undefined } })
    }
    await settlePendingAnchor()
  }

  // 换文件后：预览按 :key 重挂载，等两帧再滚（契约见文件头）
  watch(options.activePath, () => {
    if (pendingAnchor.value && options.activePath.value) void settlePendingAnchor()
  })

  return {
    tryScrollToPendingAnchor,
    handleWikiLinkClick,
    handleAnchorCopy,
    openFileWithAnchor,
    scrollToAnchor,
  }
}
