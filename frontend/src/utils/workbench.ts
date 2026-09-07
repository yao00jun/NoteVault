import type { InterviewCard, WorkbenchTask } from '@/api/workbench'

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
  const subject = [author.trim(), `${year}年${month}月${day}日`, team.trim()].filter(Boolean).join(' - ')

  const label = (task: WorkbenchTask) => {
    const project = task.project ? `[${task.project}]` : ''
    const kind = task.type !== 'todo' ? `[${task.type}]` : ''
    return `${project}${kind}${project || kind ? ' ' : ''}${plainReportText(task.title || task.content)}`
  }
  const withProgress = (task: WorkbenchTask) => {
    const notes = task.progress.filter(entry => entry.at.slice(0, 10) === date).map(entry => plainReportText(entry.note))
    return [label(task), ...notes].join('；')
  }
  const section = (title: string, lines: string[]) => `${title}：\n${lines.length ? lines.map((line, index) => `${index + 1}. ${line}`).join('\n') : '暂无'}`
  return [
    `主题：【日报】${subject}`,
    section('今日完成', done.map(withProgress)),
    section('进行中', active.map(withProgress)),
    section('阻塞与求助', blocked.map(task => `${label(task)}；${plainReportText(task.blocker) || '待协调解决'}`)),
    section('明日计划', plan.map(label)),
  ].join('\n\n')
}
