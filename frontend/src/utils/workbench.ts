import type { InterviewCard, WorkbenchTask } from '@/api/workbench'
import { i18n } from '@/i18n'

/** Use the user's calendar day, never UTC's date (which changes at 08:00 in China). */
export function localDateKey(now = new Date()): string {
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`
}

export function addCalendarDays(day: string, amount: number): string {
  const [year, month, date] = day.split('-').map(Number)
  return localDateKey(new Date(year, month - 1, date + amount, 12))
}

export function hasProgressOn(task: WorkbenchTask, day: string): boolean {
  return task.progress.some(entry => entry.at.slice(0, 10) === day)
}

export function isCarryoverTask(task: WorkbenchTask, day: string): boolean {
  return !task.completed && !!((task.date && task.date < day) || (task.due && task.due < day))
}

export function selectTodayTasks(tasks: WorkbenchTask[], day: string): WorkbenchTask[] {
  const priorities: Record<string, number> = { high: 0, medium: 1, low: 2 }
  return tasks.filter(task => {
    if (!(task.title || task.content).trim()) return false
    if (task.completed) return (task.completedAt || task.date) === day
    if (hasProgressOn(task, day)) return true
    if (task.date && task.date <= day) return true
    if (task.due && task.due <= day) return true
    return !task.date && !task.due
  }).sort((a, b) => {
    return Number(a.completed) - Number(b.completed)
      || Number(isCarryoverTask(b, day)) - Number(isCarryoverTask(a, day))
      || Number(a.type === '额外') - Number(b.type === '额外')
      || (priorities[a.priority] ?? 1) - (priorities[b.priority] ?? 1)
  })
}

export function selectReviewSession(cards: InterviewCard[], day: string, limit = 5) {
  const reviewed = cards.filter(card => card.lastReviewed === day).length
  const due = cards.filter(card => (!card.due || card.due <= day) && card.lastReviewed !== day)
    .sort((a, b) => a.due.localeCompare(b.due) || a.id.localeCompare(b.id))
  return { reviewed, target: Math.min(limit, reviewed + due.length), queue: due.slice(0, Math.max(0, limit - reviewed)), dueCount: due.length }
}

export function plainReportText(text: string): string {
  return text.replace(/<!--[^]*?-->/g, '')
    .replace(/^```[^\n]*\n|^```\s*$/gm, '')
    .replace(/^#{1,6}\s+/gm, '')
    .replace(/\[\[([^\]|]+)(?:\|([^\]]+))?\]\]/g, (_all, path, label) => label || path)
    .replace(/\[([^\]]+)\]\([^)]+\)/g, '$1')
    .replace(/\*\*([^*]+)\*\*|__([^_]+)__|`([^`]+)`/g, (_all, bold, underline, code) => bold || underline || code)
    .trim()
}

interface ReportOptions {
  date: string
  author?: string
  team?: string
  tasks: WorkbenchTask[]
}

export function buildDailyReport({ date, author = '', team = '', tasks }: ReportOptions): string {
  const today = selectTodayTasks(tasks, date)
  const tomorrow = addCalendarDays(date, 1)
  const done = today.filter(task => task.completed)
  const active = today.filter(task => !task.completed && !task.blocker && task.status !== 'blocked')
  const blocked = tasks.filter(task => !task.completed && (task.blocker || task.status === 'blocked'))
  const plan = tasks.filter(task => !task.completed && (task.due === tomorrow || task.date === tomorrow || today.includes(task)))
  const [year, month, day] = date.split('-').map(Number)
  const subject = [author.trim(), i18n.global.t('workbench.report.dateTitle', { year, month, day }), team.trim()].filter(Boolean).join(' - ')

  const label = (task: WorkbenchTask) => {
    const project = task.project ? `[${task.project}]` : ''
    const kind = task.type !== 'todo' ? `[${task.type}]` : ''
    return `${project}${kind}${project || kind ? ' ' : ''}${plainReportText(task.title || task.content)}`
  }
  const withProgress = (task: WorkbenchTask) => {
    const notes = task.progress.filter(entry => entry.at.slice(0, 10) === date).map(entry => plainReportText(entry.note))
    return [label(task), ...notes].join(i18n.global.t('workbench.report.clauseJoiner'))
  }
  const section = (title: string, lines: string[]) => i18n.global.t('workbench.report.sectionFormat', {
    title,
    items: lines.length ? lines.map((line, index) => `${index + 1}. ${line}`).join('\n') : i18n.global.t('workbench.report.none'),
  })
  return [
    i18n.global.t('workbench.report.subjectLine', { subject }),
    section(i18n.global.t('workbench.report.sectionDone'), done.map(withProgress)),
    section(i18n.global.t('workbench.report.sectionActive'), active.map(withProgress)),
    section(i18n.global.t('workbench.report.sectionBlocked'), blocked.map(task => `${label(task)}${i18n.global.t('workbench.report.clauseJoiner')}${plainReportText(task.blocker) || i18n.global.t('workbench.report.noBlockerNote')}`)),
    section(i18n.global.t('workbench.report.sectionPlan'), plan.map(label)),
  ].join('\n\n')
}
