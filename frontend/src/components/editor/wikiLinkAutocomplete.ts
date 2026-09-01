import { type CompletionContext, type CompletionResult } from '@codemirror/autocomplete'
import { GraphService } from '@bindings/github.com/notevault/notevault/index.js'

/**
 * 构造 [[ 自动补全源。
 *
 * @param getWorkspacePath 取当前工作区路径的回调（编辑器组件从 workspace store 注入）。
 *        返回 undefined 时不弹（还没打开工作区）。
 *
 * 行为：
 *   - 仅当光标前形如 `[[...`（未闭合）时触发；
 *   - 把 `[[` 之后的文本作为 query 调后端 GetLinkCandidates，拉取文件 / 标题候选；
 *   - 后端已按 query 做大小写不敏感过滤，故关闭 CM6 客户端二次过滤（filter:false）。
 *
 * 选中后插入：`fileBase]]`（文件）或 `fileBase#heading]]`（标题），
 * 替换范围从 `[[` 之后到光标，因此 `[[query` 会被整体替换成 `[[fileBase]]`。
 */
export function createWikiLinkCompletionSource(getWorkspacePath: () => string | undefined) {
  return async (context: CompletionContext): Promise<CompletionResult | null> => {
    const before = context.matchBefore(/\[\[[^\]\n]*$/)
    if (!before) return null
    const query = before.text.slice(2) // 去掉前导 [[
    const wsPath = getWorkspacePath()
    if (!wsPath) return null
    try {
      const raw = (await GraphService.GetLinkCandidates(wsPath, query)) as any
      const candidates: any[] = raw || []
      if (candidates.length === 0) return null
      return {
        from: before.from + 2,
        filter: false,
        options: candidates.map((c: any) => ({
          label: c.display,
          detail: c.kind === 'heading' ? c.heading : c.file,
          type: c.kind === 'heading' ? 'property' : 'class',
          apply: c.kind === 'heading' ? `${c.fileBase}#${c.heading}]]` : `${c.fileBase}]]`,
        })),
      }
    } catch {
      return null
    }
  }
}
