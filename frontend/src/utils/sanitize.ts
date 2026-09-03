/**
 * Markdown HTML 清洗（XSS 防线）
 *
 * marked 默认透传原始 HTML：笔记里的 <img onerror=...> / <script> 会在
 * 预览、Canvas 节点、QnA 回答和导出的 HTML 中执行，而本应用是带 Go
 * 系统绑定的 Wails WebView，注入脚本等价于拿到后端能力。
 * 所有 marked.parse 的产出必须经本函数清洗后再 v-html / 落盘。
 */
import DOMPurify from 'dompurify'

// 放行所有 data-* 属性：wiki 链接、嵌入块等扩展语法依赖它传递目标路径
DOMPurify.addHook('uponSanitizeAttribute', (_node, data) => {
  if (data.attrName.startsWith('data-')) {
    data.allowedAttributes[data.attrName] = true
  }
})

/** 额外放行的属性（与 USE_PROFILES 的 html/svg 基线合并） */
const ADD_ATTR = ['target', 'align', 'colspan', 'rowspan']

export function sanitizeHtml(dirty: string): string {
  // 注意：不要覆盖 ALLOWED_URI_REGEXP——DOMPurify 对所有非 URI-safe 属性的
  // "值"也套用该正则，收紧它会把 type="checkbox" 这类普通值一并删掉；
  // 默认正则已拦截 javascript: / data:text/html 等危险 scheme。
  return DOMPurify.sanitize(dirty, {
    USE_PROFILES: { html: true, svg: true },
    ADD_ATTR,
    FORBID_TAGS: ['style', 'form', 'iframe', 'object', 'embed'],
    FORBID_ATTR: ['srcset', 'background'],
  })
}
