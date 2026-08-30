/// <reference path="./notevault.d.ts" />
/*---
id: todo-highlight
name: 待办高亮
version: 1.0.0
description: 高亮 TODO/FIXME、标记引用行、把内部链接渲染成小组件（声明式编辑器扩展示例）
author: NoteVault
permissions: editor.decorate
---*/

// 这个插件演示 P14 的声明式编辑器扩展。
//
// **关键点：不需要 `trust: full`。**
// 插件只描述「想高亮什么」，真正的 CodeMirror 对象由宿主构造。
// 装饰一个关键词不必让用户授予完全信任——这正是选声明式协议而不是
// full-trust 的原因：保住沙箱，也保住「完全信任」这个授权的含金量。

// 字面量匹配：高亮关键词
notevault.registerDecoration({
  class: 'nv-todo',
  id: 'todo',
  pattern: 'TODO',
  style: { color: '#f59e0b', 'font-weight': '600' },
})

notevault.registerDecoration({
  class: 'nv-fixme',
  id: 'fixme',
  pattern: 'FIXME',
  style: { color: '#ef4444', 'font-weight': '600' },
})

// 正则 + 整行装饰：给引用行加左边框
notevault.registerDecoration({
  class: 'nv-quote-line',
  flags: 'gm',
  id: 'quote',
  pattern: '^> .+$',
  regex: true,
  scope: 'line',
  style: { 'border-left': '3px solid #6b7280', opacity: '0.85' },
})

// 行内小组件：把 [[内部链接]] 渲染成带样式的标签。
// text 里的 $1 会被替换成正则的第一个捕获组；
// 内容一律按纯文本渲染，不会被当作 HTML 解析。
notevault.registerWidget({
  class: 'nv-link-pill',
  id: 'wikilink',
  pattern: '\\[\\[([^\\]]+)\\]\\]',
  regex: true,
  text: '$1',
})

// 快捷键：Mod-Shift-t 触发本插件的命令
notevault.registerCommand({
  description: '列出所有待办标记（演示快捷键绑定）',
  id: 'count-todos',
  label: '统计待办标记',
  run: () => {
    notevault.notify('快捷键触发：这里可以打开命令面板查看完整统计')
  },
})

notevault.registerKeymap({
  command: 'count-todos',
  id: 'count-key',
  key: 'Mod-Shift-t',
})

// 注意事项：
// 1. style 的键名受白名单限制（不含 position / z-index，
//    否则插件能用浮层盖住界面做钓鱼），值里不允许 url()（会绕过 CSP 外传数据）；
//    不合规的项会被静默丢弃，不会报错。
// 2. regex 为 true 时必须自己保证正则不会灾难性回溯——宿主不做复杂度分析。
// 3. 每个插件最多注册 50 条扩展，超出部分直接忽略。
