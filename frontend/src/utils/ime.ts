/**
 * 输入法（IME）合成状态守卫 —— Windows WebView2 CJK 输入防线（蓝图专项 2）。
 *
 * 背景：中文拼音输入过程中按 Enter/Space 确认候选字时，keydown/keyup
 * 事件仍会冒泡（keyCode === 229），自定义快捷键若不区分就会误触发
 * （命令面板误执行、对话误提交、拼音被截断）。
 *
 * 统一守卫：所有「自定义键盘语义」的入口（全局快捷键、对话框 Enter、
 * 行内提交）在处理前先调用本函数，合成态一律静默放行原生行为。
 */
export function isImeComposing(event: Event): boolean {
  const e = event as KeyboardEvent
  return e.isComposing === true || e.keyCode === 229
}
