/*---
id: editing-toolbar
name: 编辑工具栏
version: 1.0.0
description: Markdown 格式化工具栏（对齐 Obsidian 社区主流的 Editing Toolbar：宿主不内置工具栏，按钮全部由插件提供）
author: NoteVault
permissions: ui
---*/

// 这是 NoteVault 的**预装插件**：源码经 go:embed 编进二进制，
// 首次启动时写入插件目录并默认启用，之后用户可以自由修改或删除。
//
// 它把「编辑器上方那一排格式化按钮」整体搬进插件系统，
// 用法对齐 Obsidian 的 Editing Toolbar（pkm-er，由 cMenu 改来）：
// 宿主本身不内置工具栏，按钮全部由这个插件注册。
//
// 三种按钮形态，对应 P14 协议的三个字段：
//   1. transform    —— 选区包裹 / 插入（加粗、斜体、链接…）
//   2. linePrefix   —— 整行操作（标题、引用、列表…）
//   3. command      —— 宿主内置命令（撤销、缩进…插件碰不到编辑器历史栈）
//
// icon 字段目前按纯文本渲染，所以这里沿用与旧内置工具栏相同的字符，视觉上保持一致。
// 想增删按钮直接改下面这个数组即可。

const BUTTONS = [
  // —— 需要宿主能力的按钮：走 command ——
  { id: 'undo', title: '撤销', icon: '↶', command: 'editor:undo' },
  { id: 'redo', title: '重做', icon: '↷', command: 'editor:redo' },

  // —— 选区包裹 ——
  { id: 'bold', title: '加粗', icon: 'B', transform: { prefix: '**', suffix: '**', placeholder: '粗体' } },
  { id: 'italic', title: '斜体', icon: 'I', transform: { prefix: '*', suffix: '*', placeholder: '斜体' } },
  { id: 'underline', title: '下划线', icon: 'U', transform: { prefix: '<u>', suffix: '</u>', placeholder: '下划线' } },
  { id: 'strike', title: '删除线', icon: 'S', transform: { prefix: '~~', suffix: '~~', placeholder: '删除线' } },
  { id: 'code', title: '行内代码', icon: '</>', transform: { prefix: '`', suffix: '`', placeholder: '代码' } },

  // —— 整行操作：linePrefix 会先剥掉已有的前缀，相同则取消 ——
  { id: 'h1', title: '一级标题', icon: 'H1', transform: { linePrefix: '# ' } },
  { id: 'h2', title: '二级标题', icon: 'H2', transform: { linePrefix: '## ' } },
  { id: 'h3', title: '三级标题', icon: 'H3', transform: { linePrefix: '### ' } },
  { id: 'h4', title: '四级标题', icon: 'H4', transform: { linePrefix: '#### ' } },
  { id: 'h5', title: '五级标题', icon: 'H5', transform: { linePrefix: '##### ' } },
  { id: 'h6', title: '六级标题', icon: 'H6', transform: { linePrefix: '###### ' } },
  { id: 'quote', title: '引用', icon: '❝', transform: { linePrefix: '> ' } },
  { id: 'ul', title: '无序列表', icon: '•', transform: { linePrefix: '- ' } },
  { id: 'ol', title: '有序列表', icon: '1.', transform: { linePrefix: '1. ' } },

  // —— 缩进需要编辑器的缩进命令 ——
  { id: 'indent', title: '增加缩进', icon: '⇥', command: 'editor:indent' },
  { id: 'undent', title: '减少缩进', icon: '⇤', command: 'editor:undent' },

  // —— 插入型 ——
  { id: 'link', title: '链接', icon: '🔗', transform: { prefix: '[', suffix: '](url)', placeholder: '链接文字' } },
  { id: 'image', title: '图片', icon: '🖼', transform: { prefix: '![', suffix: '](url)', placeholder: '图片说明' } },
  { id: 'codeblock', title: '代码块', icon: '```', transform: { prefix: '```\n', suffix: '\n```', placeholder: '代码' } },
  { id: 'table', title: '表格', icon: '表格', transform: { insert: '| 列 1 | 列 2 |\n| --- | --- |\n| 内容 | 内容 |\n' } },
  { id: 'hr', title: '分隔线', icon: '―', transform: { insert: '\n---\n' } },
]

for (const button of BUTTONS) {
  notevault.registerToolbarButton(button)
}

// —— 宿主 UI（取色器 / 格式刷）——
// 这三个需要打开隐藏的 input 或维护格式刷状态，插件在沙箱里做不到，
// 所以走 command：由 MarkdownEditor 在挂载时把回调注册给宿主。
notevault.registerToolbarButton({ id: 'color', title: '字体颜色', icon: 'A', command: 'editor:pickColor' })
notevault.registerToolbarButton({ id: 'bg', title: '背景色', icon: 'A', command: 'editor:pickBackground' })
notevault.registerToolbarButton({ id: 'brush', title: '格式刷', icon: '🖌', command: 'editor:brush' })

// —— 段落对齐 ——
// Markdown 没有原生对齐语法，所以沿用 HTML 标签包裹（与 Obsidian 一致）。
// 已是该对齐时再点一次会取消，其它对齐会直接替换。
notevault.registerToolbarButton({ id: 'align-left', title: '左对齐', icon: '⬅', command: 'editor:alignLeft' })
notevault.registerToolbarButton({ id: 'align-center', title: '居中', icon: '↔', command: 'editor:alignCenter' })
notevault.registerToolbarButton({ id: 'align-right', title: '右对齐', icon: '➡', command: 'editor:alignRight' })
notevault.registerToolbarButton({ id: 'align-justify', title: '两端对齐', icon: '☰', command: 'editor:alignJustify' })
