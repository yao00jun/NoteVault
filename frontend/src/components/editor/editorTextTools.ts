// ============================================================================
// editorTextTools —— “更多”下拉里的文本处理工具（纯字符串变换）。
//
// 不依赖 CodeMirror 与 Vue：输入原文、输出变换结果；
// 选区定位与 dispatch 由 MarkdownEditor.vue 负责。
// 工具清单（id + i18n）见 toolbarButtons.ts 的 TEXT_TOOLS，两边 id 必须对齐。
// ============================================================================

/** 按 id 对文本执行变换；未知 id 原样返回（防御脏数据）。 */
export function transformText(id: string, text: string): string {
  const lines = text.split('\n')
  switch (id) {
    case 'removeBlankLines':
      return lines.filter((l) => l.trim() !== '').join('\n')
    case 'insertBlankLines':
      return lines.join('\n\n')
    case 'splitLines':
      return text
        .split(/[。！？!?；;\n]+/)
        .map((s) => s.trim())
        .filter(Boolean)
        .join('\n')
    case 'mergeLines':
      return lines
        .map((l) => l.trim())
        .filter(Boolean)
        .join(' ')
    case 'dedupeLines': {
      const seen = new Set<string>()
      return lines
        .filter((l) => {
          const k = l.trim()
          if (seen.has(k)) return false
          seen.add(k)
          return true
        })
        .join('\n')
    }
    case 'sortLines':
      return [...lines].sort((a, b) => a.localeCompare(b, 'zh')).join('\n')
    case 'fullHalfConvert':
      return text
        .replace(/[！-～]/g, (ch) => String.fromCharCode(ch.charCodeAt(0) - 0xfee0))
        // 全角空格用转义写法而非字面量：字面 U+3000 会触发
        // eslint no-irregular-whitespace，且混在一堆半角空格里肉眼根本分不出来
        .replace(/\u3000/g, ' ')
    case 'numberLines':
      return lines.map((l, i) => `${i + 1}. ${l}`).join('\n')
    case 'trimLineEnds':
      return lines.map((l) => l.replace(/\s+$/, '')).join('\n')
    case 'shrinkSpaces':
      return text.replace(/[ \t]+/g, ' ')
    case 'removeAllWhitespace':
      return text.replace(/\s+/g, '')
    case 'listToTable': {
      const items = lines
        .map((l) => l.replace(/^([-*+]\s+|\d+\.\s+)/, ''))
        .filter(Boolean)
      if (!items.length) return text
      return `| 项 |\n| --- |\n${items.map((it) => `| ${it} |`).join('\n')}`
    }
    case 'tableToList': {
      const rows = lines
        .map((l) => l.trim())
        .filter((l) => l.startsWith('|') && !/^\|[\s:|-]+\|$/.test(l))
        .map((l) =>
          l
            .split('|')
            .slice(1, -1)
            .map((c) => c.trim())
            .join(' / '),
        )
        .filter(Boolean)
      return rows.map((it) => `- ${it}`).join('\n')
    }
    default:
      return text
  }
}
