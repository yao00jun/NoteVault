import { describe, expect, it } from 'vitest'
import type { InterviewCard, WorkbenchTask } from '@/api/workbench'
import { addCalendarDays, buildDailyReport, buildMeetingMinutes, buildWeeklyReport, isCarryoverTask, isoWeekWindow, localDateKey, meetingLine, meetingMinutesPath, selectReviewSession, selectTodayTasks, type OutlookMeeting } from './workbench'

function task(id: string, fields: Partial<WorkbenchTask> = {}): WorkbenchTask {
  return { id, filePath: 'Projects/商城/Tasks.md', fileName: 'Tasks.md', lineIndex: 0, sourceLine: '- [ ] 任务', content: id, title: id, type: 'US', project: '商城', projectPath: 'Projects/商城', completed: false, priority: 'medium', due: '', date: '', completedAt: '', status: 'pending', blocker: '', progress: [], ...fields }
}

function card(id: string, fields: Partial<InterviewCard> = {}): InterviewCard {
  return { id, filePath: 'Learning/面试宝典/Go.md', lineIndex: 2, comment: '<!-- srs: {} -->', question: id, answer: 'answer', level: '', interval: 0, due: '2026-09-08', reps: 0, failures: 0, lastReviewed: '', weak: false, source: '', ...fields }
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

  it('appends milestone reminders only when something is overdue or due within seven days', () => {
    const milestones = [
      { name: '已逾期', date: '2026-11-20', done: false },
      { name: '七天内', date: '2026-12-02', done: false },
      { name: '更远', date: '2027-01-01', done: false },
      { name: '已完成', date: '2026-11-26', done: true },
    ]
    const report = buildDailyReport({ date: '2026-11-26', tasks: [], milestones })
    expect(report).toContain('近期里程碑：\n1. 已逾期（2026-11-20，已逾期）\n2. 七天内（2026-12-02）')
    expect(report).not.toContain('更远')
    expect(report).not.toContain('已完成')
    const quiet = buildDailyReport({ date: '2026-11-26', tasks: [], milestones: [milestones[2]] })
    expect(quiet).not.toContain('近期里程碑')
  })
})

describe('weekly work report', () => {
  const window = isoWeekWindow('2026-11-23') // 周一

  it('derives the ISO week window with the Thursday year-anchor rule', () => {
    expect(window.week).toBe('2026-W48')
    expect(window.weekStart).toBe('2026-11-23')
    expect(window.weekEnd).toBe('2026-11-29')
    expect(window.nextWeekStart).toBe('2026-11-30')
    expect(window.nextWeekEnd).toBe('2026-12-06')
    // 跨年：2027-01-01 是周五，属于 2026 年的第 53 周
    expect(isoWeekWindow('2027-01-01').week).toBe('2026-W53')
    // 年首恰逢周四：2026-01-01 自己就是第 1 周
    expect(isoWeekWindow('2026-01-01').week).toBe('2026-W01')
  })

  it('assembles week completion, risks, milestones and next-week plan', () => {
    const report = buildWeeklyReport({
      author: '张三',
      team: '全栈研发组',
      tasks: [
        task('本周完成', { completed: true, completedAt: '2026-11-25' }),
        task('有进展', { progress: [{ id: 'p', taskId: '有进展', filePath: 'Tasks.md', taskTitle: '有进展', project: '商城', note: '联调中', at: '2026-11-26T10:00:00+08:00' }] }),
        task('阻塞任务', { type: 'DTS', blocker: '等待测试环境', status: 'blocked' }),
        task('下周到期', { due: '2026-12-02' }),
        task('上周完成', { completed: true, completedAt: '2026-11-15' }),
        task('远期任务', { due: '2027-01-20' }),
      ],
      milestones: [
        { name: '一期上线', date: '2026-11-20', done: false },
        { name: '需求评审', date: '2026-11-26', done: false },
        { name: '环境搭建', date: '2026-11-24', done: true },
        { name: '远期里程碑', date: '2027-01-15', done: false },
      ],
      window,
    })
    expect(report).toContain('主题：【周报】张三 - 2026 年第 48 周 - 全栈研发组')
    expect(report).toContain('本周完成：\n1. [商城][US] 本周完成')
    expect(report).toContain('本周推进（进行中）：\n1. [商城][US] 有进展；联调中')
    expect(report).toContain('风险与求助：\n1. [商城][DTS] 阻塞任务；等待测试环境\n2. 一期上线（2026-11-20，已逾期）')
    expect(report).toContain('里程碑进展：\n1. ⏳ 需求评审（2026-11-26）\n2. ✅ 环境搭建（2026-11-24，已完成）')
    expect(report).toContain('下周计划：\n1. [商城][US] 下周到期')
    expect(report).not.toContain('上周完成')
    expect(report).not.toContain('远期任务')
    expect(report).not.toContain('远期里程碑')
  })

  it('appends meeting sections when Outlook provides meetings', () => {
    const meeting = (subject: string, start: string, end: string, location = ''): OutlookMeeting =>
      ({ subject, start, end, location, organizer: '张三', allDay: false })
    const report = buildWeeklyReport({
      tasks: [],
      window,
      meetings: [
        meeting('周会', '2026-11-23 09:00', '2026-11-23 09:30', '会议室A'),
        meeting('评审', '2026-11-25 14:00', '2026-11-25 15:00'),
      ],
    })
    expect(report).toContain('本周会议：\n1. 11/23 09:00-09:30 周会（会议室A）\n2. 11/25 14:00-15:00 评审')
    // 无会议时不加段
    const quiet = buildWeeklyReport({ tasks: [], window })
    expect(quiet).not.toContain('本周会议')
    // 日报：当天会议不带日期前缀
    const daily = buildDailyReport({ date: '2026-11-23', tasks: [], meetings: [meeting('周会', '2026-11-23 09:00', '2026-11-23 09:30', '会议室A')] })
    expect(daily).toContain('今日会议：\n1. 09:00-09:30 周会（会议室A）')
  })

  it('folds long meeting lists with a tail note', () => {
    const many = Array.from({ length: 20 }, (_, i) =>
      ({ subject: `会议${i}`, start: `2026-11-23 0${i % 10}:00`, end: `2026-11-23 0${i % 10}:30`, location: '', organizer: '', allDay: false }))
    const report = buildWeeklyReport({ tasks: [], window, meetings: many })
    expect(report).toContain('…另有 5 个会议')
    expect(meetingLine(many[0])).toBe('00:00-00:30 会议0')
  })

  it('builds meeting minutes paths and templates', () => {
    expect(meetingMinutesPath('2026-11-23', ' 周会:迭代/规划 ')).toBe('Daily/Meetings/2026-11-23-周会-迭代-规划.md')
    expect(meetingMinutesPath('2026-11-23', '')).toBe('Daily/Meetings/2026-11-23-会议.md')
    const minutes = buildMeetingMinutes({ subject: '周会', start: '2026-11-23 09:00', end: '2026-11-23 09:30', location: '会议室A', organizer: '李四', attendees: ['王五', '赵六'], allDay: false })
    expect(minutes).toContain('type: meeting')
    expect(minutes).toContain('# 周会 会议纪要')
    expect(minutes).toContain('location: 会议室A')
    expect(minutes).toContain('attendees: [王五, 赵六]')
    const noAttendees = buildMeetingMinutes({ subject: '站会', start: '2026-11-24 10:00', end: '2026-11-24 10:15', location: '', organizer: '', attendees: [], allDay: false })
    expect(noAttendees).not.toContain('attendees:')
  })
})
