import type { WorkbenchDocument, WorkbenchProject, WorkbenchTask } from '@/api/workbench'
import { i18n } from '@/i18n'

export type TaskPeriod = 'all' | 'today' | 'week' | 'overdue'
export type RadarCategory = 'assess' | 'trial' | 'adopt' | 'hold'
export interface RadarEntry {
  name: string
  reason: string
}
export interface RadarGroup {
  id: RadarCategory
  label: string
  description: string
  entries: RadarEntry[]
}

export function projectSummaryContext(
  project: WorkbenchProject,
  tasks: WorkbenchTask[],
  documents: WorkbenchDocument[],
  today: string
): string {
  const since = new Date(today + 'T00:00:00')
  since.setDate(since.getDate() - 6)
  const until = new Date(today + 'T00:00:00')
  until.setDate(until.getDate() + 1)
  const startDay = [
    since.getFullYear(), String(since.getMonth() + 1).padStart(2, '0'), String(since.getDate()).padStart(2, '0'),
  ].join('-')
  const completed = tasks.filter((task) => task.completed && task.completedAt >= startDay && task.completedAt <= today)
  const pending = tasks.filter((task) => !task.completed)
  const progress = tasks.flatMap((task) => task.progress).filter((item) => {
    const time = new Date(item.at).getTime()
    return time >= since.getTime() && time < until.getTime()
  }).sort((a, b) => a.at.localeCompare(b.at))
  const taskLine = (task: WorkbenchTask) => '- [' + task.type + '] ' + task.title +
    (task.completed ? i18n.global.t('workbench.collections.taskDoneAt', { date: task.completedAt }) : task.due ? i18n.global.t('workbench.collections.taskDue', { date: task.due }) : '') +
    (!task.completed && task.blocker ? i18n.global.t('workbench.collections.taskBlocker', { blocker: task.blocker }) : '')
  return [
    i18n.global.t('workbench.collections.projectHeader', { name: project.name }),
    i18n.global.t('workbench.collections.statsRange', { start: startDay, end: today }),
    i18n.global.t('workbench.collections.statusLine', { status: projectStatusLabel(project.status) }),
    i18n.global.t('workbench.collections.nextStepLine', { next: project.nextStep || i18n.global.t('workbench.collections.notRecorded') }),
    i18n.global.t('workbench.collections.techStackLine', { items: project.techStack.join(i18n.global.t('common.listJoiner')) }),
    i18n.global.t('workbench.collections.sectionDone'), ...(completed.length ? completed.map(taskLine) : [i18n.global.t('workbench.collections.noDoneRecord')]),
    i18n.global.t('workbench.collections.sectionTodo'), ...pending.map(taskLine),
    i18n.global.t('workbench.collections.sectionProgress'), ...progress.map((item) => i18n.global.t('workbench.collections.progressLine', { at: item.at, title: item.taskTitle, note: item.note })),
    i18n.global.t('workbench.collections.sectionDocs'), ...documents.map((doc) => '- ' + doc.title + ' (' + doc.path + ')'),
  ].join('\n')
}

export function matchesTaskPeriod(task: WorkbenchTask, period: TaskPeriod, today: string): boolean {
  if (period === 'all') return true
  if (period === 'overdue') return !task.completed && Boolean(task.due) && task.due < today
  const dates = [task.date, task.due].filter(Boolean)
  if (period === 'today') return dates.includes(today)

  // Calendar arithmetic uses a fixed zone; the input is already the workspace's local day.
  const start = new Date(`${today}T00:00:00Z`)
  start.setUTCDate(start.getUTCDate() - ((start.getUTCDay() + 6) % 7))
  const monday = start.toISOString().slice(0, 10)
  start.setUTCDate(start.getUTCDate() + 6)
  const sunday = start.toISOString().slice(0, 10)
  return dates.some((date) => date >= monday && date <= sunday)
}

export function normalizedCollectionPath(path: string): string {
  return path.replaceAll('\\', '/').replace(/\/+$/, '')
}

export function tasksForProject(
  project: WorkbenchProject,
  tasks: WorkbenchTask[]
): WorkbenchTask[] {
  const ids = new Set(project.taskIds)
  const path = normalizedCollectionPath(project.path)
  const folder = normalizedCollectionPath(project.folder)
  return tasks.filter((task) => {
    if (ids.has(task.id) || task.project === project.name) return true
    if (task.projectPath) return [path, folder].includes(normalizedCollectionPath(task.projectPath))
    return !task.project && normalizedCollectionPath(task.filePath).startsWith(`${folder}/`)
  })
}

// 项目状态标签不接 i18n：返回值同时被 ProjectsView / defaultTaskProject 当作
// 匹配令牌（=== '进行中' 等）参与筛选，且属于后端状态值的领域词，保持原样。
export function projectStatusLabel(status: string): string {
  const value = status.trim().toLowerCase()
  if (['done', 'completed', 'complete', '已完成', '完成'].includes(value)) return '已完成'
  if (['paused', 'pause', 'on-hold', 'on hold', '暂停', '已暂停'].includes(value)) return '暂停'
  if (['planned', 'planning', 'not-started', '待启动', '未开始'].includes(value)) return '待启动'
  if (!value || ['active', 'ongoing', 'in-progress', 'in progress', '进行中'].includes(value))
    return '进行中'
  return status
}

export function collectionDateLabel(value: string): string {
  if (!value) return i18n.global.t('workbench.collections.noRecord')
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return i18n.global.t('workbench.collections.noRecord')
  return date.toLocaleString('zh-CN', {
    month: 'numeric',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function radarCategory(value: string): RadarCategory | undefined {
  if (/放弃|废弃|暂缓|\bhold\b/i.test(value)) return 'hold'
  if (/采用|推荐|\badopt\b/i.test(value)) return 'adopt'
  if (/尝试|试用|试验|\btrial\b/i.test(value)) return 'trial'
  if (/评估|探索|\bassess\b/i.test(value)) return 'assess'
  return undefined
}

function plainText(value: string): string {
  return value
    .replace(/\[([^\]]+)\]\([^)]*\)/g, '$1')
    .replace(/[*_`]/g, '')
    .trim()
}

function radarEntry(value: string): RadarEntry {
  const text = plainText(value.replace(/^\[[ xX]\]\s*/, ''))
  const parts = text.split(/\s*[：:]\s*|\s+[—–-]\s+/, 2)
  const name = parts[0]?.trim() || text
  const separator = text.slice(name.length).match(/^\s*(?:[：:]|[—–-])\s*/)?.[0] ?? ''
  return { name, reason: separator ? text.slice(name.length + separator.length).trim() : '' }
}

export function parseTechnologyRadar(markdown: string): RadarGroup[] {
  const groups: RadarGroup[] = [
    { id: 'assess', label: i18n.global.t('workbench.collections.radar.assess.label'), description: i18n.global.t('workbench.collections.radar.assess.description'), entries: [] },
    { id: 'trial', label: i18n.global.t('workbench.collections.radar.trial.label'), description: i18n.global.t('workbench.collections.radar.trial.description'), entries: [] },
    { id: 'adopt', label: i18n.global.t('workbench.collections.radar.adopt.label'), description: i18n.global.t('workbench.collections.radar.adopt.description'), entries: [] },
    { id: 'hold', label: i18n.global.t('workbench.collections.radar.hold.label'), description: i18n.global.t('workbench.collections.radar.hold.description'), entries: [] },
  ]
  let current: RadarCategory | undefined
  let categoryHeadingLevel = 0
  let fence = ''
  const add = (id: RadarCategory, entry: RadarEntry) => {
    if (entry.name) groups.find((group) => group.id === id)?.entries.push(entry)
  }

  for (const line of markdown.split(/\r?\n/)) {
    const marker = line.match(/^ {0,3}(`{3,}|~{3,})/)?.[1]
    if (fence) {
      if (marker?.[0] === fence[0] && marker.length >= fence.length && line.trim() === marker)
        fence = ''
      continue
    }
    if (marker) {
      fence = marker
      continue
    }
    const heading = line.match(/^(#{1,6})\s+(.+?)\s*#*$/)
    if (heading) {
      const category = radarCategory(plainText(heading[2] ?? ''))
      if (category) {
        current = category
        categoryHeadingLevel = heading[1]?.length ?? 0
      } else if ((heading[1]?.length ?? 0) <= categoryHeadingLevel) {
        current = undefined
      }
      continue
    }
    if (line.trim().startsWith('|')) {
      const cells = line
        .trim()
        .replace(/^\||\|$/g, '')
        .split('|')
        .map(plainText)
      const category = radarCategory(cells[1] ?? '')
      if (category && cells[0])
        add(category, { name: cells[0], reason: cells.slice(2).join(' · ') })
      continue
    }
    const item = line.match(/^\s*[-*+]\s+(.+)$/)
    if (current && item?.[1]) add(current, radarEntry(item[1]))
  }
  return groups
}
