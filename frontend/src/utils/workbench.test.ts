import { describe, expect, it } from 'vitest'
import type { InterviewCard, WorkbenchTask } from '@/api/workbench'
import { addCalendarDays, buildDailyReport, isCarryoverTask, localDateKey, selectReviewSession, selectTodayTasks } from './workbench'

function task(id: string, fields: Partial<WorkbenchTask> = {}): WorkbenchTask {
  return { id, filePath: 'Projects/商城/Tasks.md', fileName: 'Tasks.md', lineIndex: 0, sourceLine: '- [ ] 任务', content: id, title: id, type: 'US', project: '商城', projectPath: 'Projects/商城', completed: false, priority: 'medium', due: '', date: '', completedAt: '', status: 'pending', blocker: '', progress: [], ...fields }
}

function card(id: string, fields: Partial<InterviewCard> = {}): InterviewCard {
  return { id, filePath: 'Learning/面试宝典/Go.md', lineIndex: 2, comment: '<!-- srs: {} -->', question: id, answer: 'answer', level: '', interval: 0, due: '2026-09-08', reps: 0, failures: 0, lastReviewed: '', weak: false, ...fields }
}

describe('workbench calendar and task selection', () => {
  it('uses local calendar dates and rolls across month/year boundaries', () => {
    expect(localDateKey(new Date(2026, 8, 8, 0, 5))).toBe('2026-09-08')
    expect(addCalendarDays('2026-12-31', 1)).toBe('2027-01-01')
    expect(addCalendarDays('2026-03-01', -1)).toBe('2026-02-28')
  })

  it('carries yesterday forward but excludes historical completions and future plans', () => {
    const items = [task('tomorrow', { due: '2026-09-09' }), task('completed-old', { completed: true, completedAt: '2026-09-07' }), task('today', { due: '2026-09-08' }), task('yesterday', { date: '2026-09-07' }), task('unscheduled'), task('completed-now', { completed: true, completedAt: '2026-09-08' }), task('future-daily', { date: '2026-09-15' }), task('blank', { title: '', content: '' })]
    expect(selectTodayTasks(items, '2026-09-08').map(x => x.id)).toEqual(['yesterday', 'today', 'unscheduled', 'completed-now'])
    expect(isCarryoverTask(items[3], '2026-09-08')).toBe(true)
    expect(isCarryoverTask(items[1], '2026-09-08')).toBe(false)
  })

  it('brings a future-due task into today when progress was recorded today', () => {
    const item = task('active', { due: '2026-09-20', progress: [{ id: 'p', taskId: 'active', filePath: 'Tasks.md', taskTitle: 'active', project: '商城', note: '接口完成', at: '2026-09-08T10:00:00+08:00' }] })
    expect(selectTodayTasks([item], '2026-09-08')).toEqual([item])
  })
})

describe('daily interview quota', () => {
  it('caps the session at five including cards already reviewed after a restart', () => {
    const cards = [card('done1', { lastReviewed: '2026-09-08', due: '2026-09-09' }), card('done2', { lastReviewed: '2026-09-08', due: '2026-09-15' }), card('a'), card('b'), card('c'), card('d'), card('tomorrow', { due: '2026-09-09' })]
    const session = selectReviewSession(cards, '2026-09-08')
    expect(session.reviewed).toBe(2)
    expect(session.target).toBe(5)
    expect(session.queue.map(x => x.id)).toEqual(['a', 'b', 'c'])
    expect(selectReviewSession(cards.map(c => ({ ...c, lastReviewed: '2026-09-08' })), '2026-09-08').queue).toEqual([])
  })
})

describe('daily email report', () => {
  it('separates completed, progressing, blocked and tomorrow without historical or distant tasks', () => {
    const progress = { id: 'p1', taskId: '联调', taskTitle: '联调', filePath: 'Tasks.md', project: '商城', note: '已完成接口联调，待验证', at: '2026-09-08T10:20:00+08:00' }
    const report = buildDailyReport({ date: '2026-09-08', author: '张三', team: '全栈研发组', tasks: [task('登录', { completed: true, completedAt: '2026-09-08' }), task('联调', { progress: [progress] }), task('MQ权限', { type: 'DTS', blocker: '缺少测试环境权限', status: 'blocked' }), task('明日验收', { due: '2026-09-09' }), task('历史完成', { completed: true, completedAt: '2026-09-07' }), task('下月发布', { due: '2026-10-01' })] })
    expect(report).toContain('主题：【日报】张三 - 2026年9月8日 - 全栈研发组')
    expect(report).toContain('今日完成：\n1. [商城][US] 登录')
    expect(report).toContain('进行中：\n1. [商城][US] 联调；已完成接口联调，待验证')
    expect(report).toContain('阻塞与求助：\n1. [商城][DTS] MQ权限；缺少测试环境权限')
    expect(report).toContain('明日计划：')
    expect(report).toContain('明日验收')
    expect(report).not.toContain('历史完成')
    expect(report).not.toContain('下月发布')
    expect(report).not.toContain('<!--')
  })
})
