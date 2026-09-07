import { describe, expect, it } from 'vitest'
import type { WorkbenchProject, WorkbenchTask } from '@/api/workbench'
import { matchesTaskPeriod, parseTechnologyRadar, projectSummaryContext, tasksForProject } from './workbenchCollections'

function task(values: Partial<WorkbenchTask> = {}): WorkbenchTask {
  return {
    id: 'task-1',
    filePath: 'Projects/商城/Tasks.md',
    fileName: 'Tasks.md',
    lineIndex: 0,
    sourceLine: '- [ ] [US] 接口联调',
    content: '接口联调',
    title: '接口联调',
    type: 'US',
    project: '商城',
    projectPath: 'Projects/商城/project.md',
    completed: false,
    priority: '',
    due: '',
    date: '',
    completedAt: '',
    status: '',
    blocker: '',
    progress: [],
    ...values,
  }
}

const project: WorkbenchProject = {
  name: '商城',
  path: 'Projects/商城/project.md',
  folder: 'Projects/商城',
  status: '进行中',
  nextStep: '',
  techStack: [],
  taskIds: ['linked-daily'],
  notes: [],
  modifiedAt: '',
}

describe('project collection filters', () => {
  it('keeps Monday through Sunday in the local calendar week and excludes adjacent weeks', () => {
    const tasks = ['2026-09-06', '2026-09-07', '2026-09-13', '2026-09-14'].map((due) =>
      task({ id: due, due })
    )
    expect(
      tasks.filter((item) => matchesTaskPeriod(item, 'week', '2026-09-08')).map((item) => item.id)
    ).toEqual(['2026-09-07', '2026-09-13'])
  })

  it('never marks completed or undated work overdue and includes the scheduled day in today', () => {
    expect(matchesTaskPeriod(task({ due: '2026-09-07' }), 'overdue', '2026-09-08')).toBe(true)
    expect(
      matchesTaskPeriod(task({ due: '2026-09-07', completed: true }), 'overdue', '2026-09-08')
    ).toBe(false)
    expect(matchesTaskPeriod(task(), 'overdue', '2026-09-08')).toBe(false)
    expect(
      matchesTaskPeriod(task({ date: '2026-09-08', due: '2026-09-11' }), 'today', '2026-09-08')
    ).toBe(true)
    expect(matchesTaskPeriod(task({ due: '2026-09-08' }), 'today', '2026-09-08')).toBe(true)
    expect(matchesTaskPeriod(task(), 'today', '2026-09-08')).toBe(false)
  })

  it('includes explicitly linked daily tasks without mixing similarly named project folders', () => {
    const tasks = [
      task(),
      task({ id: 'linked-daily', filePath: 'Daily/2026-09-08.md', project: '', projectPath: '' }),
      task({
        id: 'other',
        filePath: 'Projects/商城二期/Tasks.md',
        project: '商城二期',
        projectPath: 'Projects/商城二期/project.md',
      }),
    ]
    expect(tasksForProject(project, tasks).map((item) => item.id)).toEqual([
      'task-1',
      'linked-daily',
    ])
  })
})

describe('project weekly Copilot context', () => {
  it('includes only recent completions and progress while preserving the dates of future plans', () => {
    const progress = (note: string, at: string) => ({
      id: note, taskId: 'task-1', filePath: 'Projects/商城/Tasks.md', taskTitle: '接口联调',
      project: '商城', note, at,
    })
    const context = projectSummaryContext(project, [
      task({ title: '本周完成接口', completed: true, completedAt: '2026-09-07' }),
      task({ title: '八月已归档任务', completed: true, completedAt: '2026-08-28' }),
      task({ title: '日期未知的历史完成', completed: true }),
      task({ title: '下周部署计划', due: '2026-09-15', progress: [
        progress('本周记录可见', '2026-09-08T10:00:00'),
        progress('旧进展不进入周报', '2026-08-31T09:00:00'),
        progress('未来记录不当作本周成果', '2026-09-10T09:00:00'),
      ] }),
    ], [], '2026-09-08')
    expect(context).toContain('本周完成接口')
    expect(context).toContain('本周记录可见')
    expect(context).toContain('下周部署计划')
    expect(context).toContain('2026-09-15')
    expect(context).not.toContain('八月已归档任务')
    expect(context).not.toContain('日期未知的历史完成')
    expect(context).not.toContain('旧进展不进入周报')
    expect(context).not.toContain('未来记录不当作本周成果')
  })
})

describe('technology radar Markdown', () => {
  it('reads decision reasons from category lists and leaves absent rings empty', () => {
    const groups = parseTechnologyRadar(
      [
        '# 技术雷达',
        '',
        '## 评估中',
        '- **Bun**：验证现有插件兼容性',
        '## 采用推荐',
        '- [Go](https://go.dev) — 团队熟悉且部署简单',
        '## 放弃废弃',
        '- 旧 ORM：迁移维护成本过高',
      ].join('\n')
    )
    expect(groups.map((group) => ({ id: group.id, entries: group.entries }))).toEqual([
      { id: 'assess', entries: [{ name: 'Bun', reason: '验证现有插件兼容性' }] },
      { id: 'trial', entries: [] },
      { id: 'adopt', entries: [{ name: 'Go', reason: '团队熟悉且部署简单' }] },
      { id: 'hold', entries: [{ name: '旧 ORM', reason: '迁移维护成本过高' }] },
    ])
  })

  it('supports table decisions and ignores sample entries inside fenced code', () => {
    const groups = parseTechnologyRadar(
      [
        '## 尝试中',
        '~~~md',
        '## 采用推荐',
        '- 假技术：仅示例',
        '~~~',
        '- Temporal：先验证补偿流程',
        '',
        '| 技术 | 状态 | 决策理由 |',
        '| --- | --- | --- |',
        '| Redis | Adopt | 已验证热点缓存收益 |',
        '| ClickHouse | 评估中 | 评估分析成本 |',
      ].join('\n')
    )
    expect(groups[0]?.entries).toEqual([{ name: 'ClickHouse', reason: '评估分析成本' }])
    expect(groups[1]?.entries).toEqual([{ name: 'Temporal', reason: '先验证补偿流程' }])
    expect(groups[2]?.entries).toEqual([{ name: 'Redis', reason: '已验证热点缓存收益' }])
    expect(groups[3]?.entries).toEqual([])
  })
})
