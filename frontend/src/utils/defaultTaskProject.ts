import type { WorkbenchProject, WorkbenchTask } from '@/api/workbench'
import { addCalendarDays } from './workbench'
import { projectStatusLabel, tasksForProject } from './workbenchCollections'

export const GENERAL_PROJECT_FOLDER = 'Projects/通用事务'

/** Prefer active projects used in the last seven days, then their latest file edit. */
export function defaultTaskProject(projects: WorkbenchProject[], tasks: WorkbenchTask[], today: string): string {
  const since = addCalendarDays(today, -6)
  const recent = (day: string) => day >= since && day <= today
  const ranked = projects.filter(project => projectStatusLabel(project.status) === '进行中').map(project => ({
    project,
    activity: tasksForProject(project, tasks).filter(task => recent(task.date) || recent(task.completedAt)
      || task.progress.some(entry => recent(entry.at.slice(0, 10)))).length,
    modifiedAt: Date.parse(project.modifiedAt) || 0,
  }))
  ranked.sort((a, b) => b.activity - a.activity || b.modifiedAt - a.modifiedAt
    || (a.project.folder < b.project.folder ? -1 : a.project.folder > b.project.folder ? 1 : 0))
  return ranked[0]?.project.folder ?? GENERAL_PROJECT_FOLDER
}
