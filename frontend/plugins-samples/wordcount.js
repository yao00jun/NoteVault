/// <reference path="./notevault.d.ts" />
/*---
id: wordcount
name: 笔记统计
version: 1.0.0
description: 统计工作区笔记数量与标签分布（演示文件/元数据 API、设置面板与数据持久化）
author: NoteVault
permissions: workspace.read, commands, notifications
---*/

// 这个插件演示 NoteVault 的「数据处理类」能力，也是补齐 #27/#28/#29 后的典型用法：
//   1. listFiles / getAllTags —— 遍历笔记、读标签（不需要 full-trust）
//   2. registerSettings       —— 声明设置项，由宿主渲染界面
//   3. loadData / saveData    —— 插件私有数据持久化
//   4. onFileChange           —— 感知文件变更（可选，用来做自动刷新）
//
// 整套流程都跑在 Worker 沙箱里：不声明 trust=full 也能完成，
// 因为这三类需求（遍历、配置、存储）本来就不该需要主进程权限。

let stored = {}

notevault.onLoad(async () => {
  // 插件私有数据：不存在时返回空串（首次运行）
  const raw = await notevault.loadData()
  if (raw) {
    try {
      stored = JSON.parse(raw)
    } catch {
      stored = {}
    }
  }

  if (stored.lastRun) notevault.notify(`上次统计：${stored.lastRun}`)

  notevault.registerSettings({
    title: '笔记统计',
    items: [
      {
        default: 1,
        description: '低于该出现次数的标签会被忽略',
        key: 'minTagCount',
        label: '标签最小出现次数',
        type: 'number',
      },
    ],
    values: stored.settings ?? {},
  })

  // 用户在宿主界面改动设置时，整份值回传到这里，由插件自己持久化
  notevault.onSettingsChange(async values => {
    stored.settings = values
    await notevault.saveData(JSON.stringify(stored))
  })
})

notevault.registerCommand({
  description: '统计笔记数量与标签分布',
  id: 'stats',
  label: '统计笔记',
  run: async () => {
    const files = await notevault.listFiles()
    const tags = await notevault.getAllTags()
    const minCount = Number(stored?.settings?.minTagCount ?? 1)

    const top = tags
      .filter(tag => tag.count >= minCount)
      .slice(0, 5)
      .map(tag => `${tag.name}(${tag.count})`)
      .join('、')

    const summary = `共 ${files.length} 篇笔记，${tags.length} 个标签。高频：${top || '无'}`
    notevault.notify(summary)

    // 记下这次统计的时间，下次启动时提示
    stored.lastRun = new Date().toLocaleString()
    await notevault.saveData(JSON.stringify(stored))
  },
})

// 可选：文件有变动时提示一下（真正的用途是触发重新统计之类的自动化）
notevault.onFileChange(event => {
  if (event.type === 'create') notevault.notify(`新增了 ${event.path}`)
})
