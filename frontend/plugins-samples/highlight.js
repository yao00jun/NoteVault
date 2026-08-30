/// <reference path="./notevault.d.ts" />
/*---
id: highlight
name: 高亮与特效
version: 1.0.0
description: 通过插件系统向编辑器工具栏注入按钮（UI 注册协议示例）
permissions: ui
---*/
// 该插件演示 NoteVault 的“UI 注册协议”：
// 插件运行在 Worker 沙箱里，不能直接操作编辑器 DOM，
// 只能声明工具栏按钮 + 文本变换，由宿主在真实编辑器上执行。
// 这样就做到了类似 Obsidian 的“插件往编辑器注入按钮”的效果，同时保留沙箱隔离。

notevault.registerToolbarButton({
  id: 'highlight',
  title: '高亮',
  icon: '==',
  tooltip: '用 == 包裹选区，实现 Obsidian 风格高亮',
  transform: { prefix: '==', suffix: '==', placeholder: '高亮文本' },
})

notevault.registerToolbarButton({
  id: 'spoiler',
  title: '模糊',
  icon: '🫥',
  tooltip: '用 <span class="spoiler"> 包裹选区，实现模糊遮挡',
  transform: { prefix: '<span class="spoiler">', suffix: '</span>', placeholder: '隐藏内容' },
})
