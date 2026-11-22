import type { InterviewCard, WorkbenchTask } from '@/api/workbench'
import { OutlookService, type OutlookEmail, type OutlookMeeting } from '@/api'
import { i18n } from '@/i18n'

// 报告构建器的 Outlook 入参类型从这里统一出口（组件不直接 import '@/api' 的绑定模型）
export type { OutlookEmail, OutlookMeeting }

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
  /** 项目里程碑（全项目扁平列表）：配置后日报带「近期里程碑」提醒段 */
  milestones?: MilestoneLite[]
  /** 今日会议（Outlook 摄取，失败时为空列表）：非空才追加「今日会议」段 */
  meetings?: OutlookMeeting[]
}

/** 报告里的任务行：[项目][类型] 标题（Markdown 噪声已剥除）。 */
export function reportTaskLabel(task: WorkbenchTask): string {
  const project = task.project ? `[${task.project}]` : ''
  const kind = task.type !== 'todo' ? `[${task.type}]` : ''
  return `${project}${kind}${project || kind ? ' ' : ''}${plainReportText(task.title || task.content)}`
}

function reportSection(title: string, lines: string[]): string {
  return i18n.global.t('workbench.report.sectionFormat', {
    title,
    items: lines.length ? lines.map((line, index) => `${index + 1}. ${line}`).join('\n') : i18n.global.t('workbench.report.none'),
  })
}

function withProgressNotes(task: WorkbenchTask, days: (day: string) => boolean): string {
  const notes = task.progress.filter(entry => days(entry.at.slice(0, 10))).map(entry => plainReportText(entry.note))
  return [reportTaskLabel(task), ...notes].join(i18n.global.t('workbench.report.clauseJoiner'))
}

function milestoneReminderLines(milestones: MilestoneLite[], date: string): string[] {
  const overdue = milestones.filter(m => !m.done && !!m.date && m.date < date)
    .map(m => i18n.global.t('workbench.report.milestoneLine', { name: plainReportText(m.name), date: m.date, state: i18n.global.t('workbench.report.milestoneOverdue') }))
  const soon = milestones.filter(m => !m.done && !!m.date && m.date >= date && m.date <= addCalendarDays(date, 7))
    .map(m => i18n.global.t('workbench.report.milestoneLine', { name: plainReportText(m.name), date: m.date, state: '' }))
  return [...overdue, ...soon].filter(line => !line.includes('undefined'))
}

export function buildDailyReport({ date, author = '', team = '', tasks, milestones = [], meetings = [] }: ReportOptions): string {
  const today = selectTodayTasks(tasks, date)
  const tomorrow = addCalendarDays(date, 1)
  const done = today.filter(task => task.completed)
  const active = today.filter(task => !task.completed && !task.blocker && task.status !== 'blocked')
  const blocked = tasks.filter(task => !task.completed && (task.blocker || task.status === 'blocked'))
  const plan = tasks.filter(task => !task.completed && (task.due === tomorrow || task.date === tomorrow || today.includes(task)))
  const [year, month, day] = date.split('-').map(Number)
  const subject = [author.trim(), i18n.global.t('workbench.report.dateTitle', { year, month, day }), team.trim()].filter(Boolean).join(' - ')

  const withProgress = (task: WorkbenchTask) => withProgressNotes(task, at => at === date)
  const joiner = i18n.global.t('workbench.report.clauseJoiner')
  const reminderLines = milestoneReminderLines(milestones, date)
  const sections = [
    i18n.global.t('workbench.report.subjectLine', { subject }),
    reportSection(i18n.global.t('workbench.report.sectionDone'), done.map(withProgress)),
    reportSection(i18n.global.t('workbench.report.sectionActive'), active.map(withProgress)),
    reportSection(i18n.global.t('workbench.report.sectionBlocked'), blocked.map(task => `${reportTaskLabel(task)}${joiner}${plainReportText(task.blocker) || i18n.global.t('workbench.report.noBlockerNote')}`)),
    reportSection(i18n.global.t('workbench.report.sectionPlan'), plan.map(reportTaskLabel)),
  ]
  if (reminderLines.length) {
    sections.push(reportSection(i18n.global.t('workbench.report.sectionMilestoneReminders'), reminderLines))
  }
  if (meetings.length) {
    sections.push(reportSection(i18n.global.t('workbench.report.sectionDayMeetings'), meetings.map(m => meetingLine(m))))
  }
  return sections.join('\n\n')
}

/** ISO 周窗口：周四所在年份决定 ISO 年与周号（本地时区，正午规避夏令时边界）。 */
export interface WeekWindow {
  week: string
  year: number
  weekNumber: number
  weekStart: string
  weekEnd: string
  nextWeekStart: string
  nextWeekEnd: string
}

export function isoWeekWindow(day: string): WeekWindow {
  const [year, month, date] = day.split('-').map(Number)
  const anchor = new Date(year, month - 1, date, 12)
  const dayNum = (anchor.getDay() + 6) % 7 // 周一=0 … 周日=6
  const monday = new Date(year, month - 1, date - dayNum, 12)
  const thursday = new Date(monday.getFullYear(), monday.getMonth(), monday.getDate() + 3, 12)
  const yearStart = new Date(thursday.getFullYear(), 0, 1, 12)
  const weekNumber = Math.floor((thursday.getTime() - yearStart.getTime()) / (7 * 86400000)) + 1
  const at = (offset: number) => localDateKey(new Date(monday.getFullYear(), monday.getMonth(), monday.getDate() + offset, 12))
  return {
    week: `${thursday.getFullYear()}-W${String(weekNumber).padStart(2, '0')}`,
    year: thursday.getFullYear(),
    weekNumber,
    weekStart: at(0),
    weekEnd: at(6),
    nextWeekStart: at(7),
    nextWeekEnd: at(13),
  }
}

export interface MilestoneLite {
  name: string
  date: string
  done: boolean
}

export interface WeeklyReportOptions {
  author?: string
  team?: string
  tasks: WorkbenchTask[]
  milestones?: MilestoneLite[]
  /** 本周会议（Outlook 摄取，失败时为空列表）：非空才追加「本周会议」段 */
  meetings?: OutlookMeeting[]
  window: WeekWindow
}

export function buildWeeklyReport({ author = '', team = '', tasks, milestones = [], meetings = [], window: w }: WeeklyReportOptions): string {
  const inRange = (day: string | undefined, start: string, end: string) => !!day && day >= start && day <= end
  const inWeek = (day?: string) => inRange(day, w.weekStart, w.weekEnd)
  // 本周完成：完成时间落在周内；旧数据无 completedAt 时回退到 due/date 在周内
  const done = tasks.filter(task => task.completed && (inWeek(task.completedAt) || (!task.completedAt && (inWeek(task.due) || inWeek(task.date)))))
  const active = tasks.filter(task => !task.completed && task.progress.some(entry => inWeek(entry.at.slice(0, 10))))
  const blocked = tasks.filter(task => !task.completed && (task.blocker || task.status === 'blocked'))
  const overdueMilestones = milestones.filter(m => !m.done && !!m.date && m.date < w.weekStart)
  const weekMilestones = milestones.filter(m => inWeek(m.date))
  const plan = tasks.filter(task => !task.completed && (inRange(task.due, w.nextWeekStart, w.nextWeekEnd) || inRange(task.date, w.nextWeekStart, w.nextWeekEnd)))

  const subject = [author.trim(), i18n.global.t('workbench.report.weekTitle', { year: w.year, week: w.weekNumber }), team.trim()].filter(Boolean).join(' - ')
  const joiner = i18n.global.t('workbench.report.clauseJoiner')
  const riskLines = [
    ...blocked.map(task => `${reportTaskLabel(task)}${joiner}${plainReportText(task.blocker) || i18n.global.t('workbench.report.noBlockerNote')}`),
    ...overdueMilestones.map(m => i18n.global.t('workbench.report.milestoneLine', { name: plainReportText(m.name), date: m.date, state: i18n.global.t('workbench.report.milestoneOverdue') })),
  ]
  const milestoneLines = weekMilestones.map(m =>
    `${m.done ? '✅' : '⏳'} ${i18n.global.t('workbench.report.milestoneLine', { name: plainReportText(m.name), date: m.date, state: m.done ? i18n.global.t('workbench.report.milestoneDone') : '' })}`
  )
  const sections = [
    i18n.global.t('workbench.report.weekSubjectLine', { subject }),
    reportSection(i18n.global.t('workbench.report.sectionWeekDone'), done.map(task => withProgressNotes(task, inWeek))),
    reportSection(i18n.global.t('workbench.report.sectionWeekActive'), active.map(task => withProgressNotes(task, inWeek))),
    reportSection(i18n.global.t('workbench.report.sectionWeekRisk'), riskLines),
    reportSection(i18n.global.t('workbench.report.sectionWeekMilestones'), milestoneLines),
  ]
  if (meetings.length) {
    sections.push(reportSection(i18n.global.t('workbench.report.sectionWeekMeetings'), weekMeetingLines(meetings)))
  }
  sections.push(reportSection(i18n.global.t('workbench.report.sectionWeekPlan'), plan.map(reportTaskLabel)))
  return sections.join('\n\n')
}

// meetingLine 单条会议行：日报用「HH:mm-HH:mm 主题（地点）」，
// 周报带日期前缀（MM/DD）；主题 Markdown 噪声已剥除。
export function meetingLine(meeting: OutlookMeeting, withDate = false): string {
  const start = meeting.start.slice(11, 16)
  const end = meeting.end.slice(11, 16)
  const datePrefix = withDate ? `${meeting.start.slice(5, 10).replace('-', '/')} ` : ''
  const where = meeting.location ? i18n.global.t('workbench.report.meetingAt', { where: meeting.location }) : ''
  return `${datePrefix}${start}-${end} ${plainReportText(meeting.subject)}${where}`
}

// weekMeetingLines 周报会议行，超过 15 条折叠为尾注（周报篇幅可控）。
const weekMeetingsCap = 15
function weekMeetingLines(meetings: OutlookMeeting[]): string[] {
  const lines = meetings.slice(0, weekMeetingsCap).map(m => meetingLine(m, true))
  if (meetings.length > weekMeetingsCap) {
    lines.push(i18n.global.t('workbench.report.meetingMore', { n: meetings.length - weekMeetingsCap }))
  }
  return lines
}

// fetchMeetingsSafe 会议数据源的安全封装：COM 不可用（未装 Outlook / 平台不支持 /
// 超时）一律降级为空列表并 warn——会议是日报周报的增强段，绝不阻断生成主链路。
export async function fetchMeetingsSafe(start: string, end: string): Promise<OutlookMeeting[]> {
  try {
    const meetings = (await OutlookService.ListMeetings(start, end)) as OutlookMeeting[] | null
    return meetings ?? []
  } catch (cause) {
    console.warn('Outlook 会议不可用，报告将不含会议段：', cause)
    return []
  }
}

// meetingMinutesPath 会议纪要落盘路径（与后端 meetingFileName 同一套清洗规则：
// 文件系统非法字符替换为连字符，空主题回落「会议」）。
export function meetingMinutesPath(date: string, subject: string): string {
  const clean = subject.trim().replace(/[\\/:*?"<>|]/g, '-') || '会议'
  return `Daily/Meetings/${date}-${clean}.md`
}

// buildMeetingMinutes 生成会议纪要模板正文。
export function buildMeetingMinutes(meeting: OutlookMeeting): string {
  const time = `${meeting.start.slice(11, 16)}-${meeting.end.slice(11, 16)}`
  const lines = [
    '---',
    'type: meeting',
    `date: ${meeting.start.slice(0, 10)}`,
    `time: "${time}"`,
  ]
  if (meeting.location) lines.push(`location: ${meeting.location}`)
  if (meeting.organizer) lines.push(`organizer: ${meeting.organizer}`)
  if (meeting.attendees?.length) lines.push(`attendees: [${meeting.attendees.join(', ')}]`)
  lines.push('---', '', `# ${meeting.subject} 会议纪要`, '')
  if (meeting.attendees?.length) lines.push(`- 参会人：${meeting.attendees.join('、')}`, '')
  return lines.join('\n')
}

// --- Outlook 邮件摄取（二期）：旗标邮件 → Inbox 笔记 → 蒸馏流 ---

// fetchFlaggedEmailsSafe 邮件数据源的安全封装：COM 不可用一律降级为空列表。
export async function fetchFlaggedEmailsSafe(limit = 10): Promise<OutlookEmail[]> {
  try {
    const emails = (await OutlookService.ListFlaggedEmails(limit)) as OutlookEmail[] | null
    return emails ?? []
  } catch (cause) {
    console.warn('Outlook 旗标邮件不可用：', cause)
    return []
  }
}

// emailNotePath 旗标邮件收录到 Inbox 的落盘路径（清洗规则与会议纪要一致）。
export function emailNotePath(date: string, subject: string): string {
  const clean = subject.trim().replace(/[:*?"<>|]/g, '-') || '邮件'
  return `Inbox/邮件/${date}-${clean}.md`
}

// fetchEmailBodySafe 按 EntryID 拉取邮件全文：失败降级返回空串（调用方回落摘要）。
export async function fetchEmailBodySafe(entryId: string): Promise<string> {
  try {
    return (await OutlookService.GetEmailBody(entryId)) as string
  } catch (cause) {
    console.warn('Outlook 邮件全文不可用，收录笔记将使用摘要：', cause)
    return ''
  }
}

// buildEmailNote 生成收录笔记：frontmatter 保留来源元数据，正文供蒸馏流提炼。
// fullBody 非空时用全文替换列表接口带回的 2000 字摘要。
export function buildEmailNote(email: OutlookEmail, fullBody = ''): string {
  const body = fullBody.trim() || email.excerpt
  const lines = [
    '---',
    'type: email',
    'source: outlook',
    `from: ${email.sender}`,
    `received: ${email.receivedTime}`,
    '---',
    '',
    `# 邮件：${email.subject}`,
    '',
    '## 要点',
    '',
  ]
  if (body) lines.push(body, '')
  lines.push('## 待办', '', '- [ ] ')
  return lines.join('\n')
}
