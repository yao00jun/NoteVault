import { describe, expect, it } from 'vitest'
import type { WorkbenchProject, WorkbenchTask } from '@/api/workbench'
import { defaultTaskProject } from './defaultTaskProject'

function project(name: string, values: Partial<WorkbenchProject> = {}): WorkbenchProject {
  return { name, folder: `Projects/${name}`, path: `Projects/${name}/project.md`, status: '进行中', nextStep: '', techStack: [], taskIds: [], notes: [], modifiedAt: '', ...values }
}

function task(projectName: string, values: Partial<WorkbenchTask> = {}): WorkbenchTask {
  return {
    id: `${projectName}-task`, filePath: `Projects/${projectName}/Tasks.md`, fileName: 'Tasks.md',
    lineIndex: 0, sourceLine: '- [ ] Work', content: 'Work', title: 'Work', type: 'US',
    project: projectName, projectPath: `Projects/${projectName}/project.md`, completed: false,
    priority: '', due: '', date: '2026-09-09', completedAt: '', status: 'todo', blocker: '', progress: [],
    ...values,
  }
}

describe('the default task project', () => {
  it('prefers frequent task activity in active projects over file modification time', () => {
    const projects = [project('Quiet', { modifiedAt: '2026-09-09T12:00:00+08:00' }), project('Busy', { status: 'in-progress' }), project('Closed', { status: 'completed' })]
    const tasks = [task('Quiet'), task('Busy'), task('Busy', { id: 'busy-2', date: '2026-09-05' }), ...[1, 2, 3].map(id => task('Closed', { id: `closed-${id}` }))]
    expect(defaultTaskProject(projects, tasks, '2026-09-09')).toBe('Projects/Busy')
  })

  it('counts recent completions and progress on older tasks but ignores old and future schedules', () => {
    const projects = [project('Backlog', { modifiedAt: '2026-09-09T12:00:00+08:00' }), project('Current')]
    const tasks = [
      task('Backlog', { id: 'old', date: '2026-08-01' }),
      task('Backlog', { id: 'future', date: '2026-09-10' }),
      task('Current', { id: 'done', date: '2026-08-01', completed: true, completedAt: '2026-09-08' }),
      task('Current', { id: 'progress', date: '2026-08-01', progress: [{ id: 'p1', taskId: 'progress', taskTitle: 'Work', project: 'Current', filePath: 'Projects/Current/Tasks.md', note: 'Advanced', at: '2026-09-09T08:00:00+08:00' }] }),
    ]
    expect(defaultTaskProject(projects, tasks, '2026-09-09')).toBe('Projects/Current')
  })

  it('uses the latest modification when recent task activity is equal', () => {
    const projects = [project('Earlier', { modifiedAt: '2026-09-09T10:00:00+08:00' }), project('Later', { status: 'active', modifiedAt: '2026-09-09T03:00:00Z' })]
    expect(defaultTaskProject(projects, [task('Earlier'), task('Later')], '2026-09-09')).toBe('Projects/Later')
  })

  it('breaks equal or invalid dates by folder without mutating the project list', () => {
    const projects = [project('B', { modifiedAt: 'invalid' }), project('A')]
    expect(defaultTaskProject(projects, [], '2026-09-09')).toBe('Projects/A')
    expect(defaultTaskProject([...projects].reverse(), [], '2026-09-09')).toBe('Projects/A')
    expect(projects.map(item => item.name)).toEqual(['B', 'A'])
  })

  it('uses general chores when there are no active projects', () => {
    const projects = [project('Paused', { status: 'paused' }), project('Future', { status: 'planned' }), project('Done', { status: '已完成' }), project('Archived', { status: 'archived' })]
    expect(defaultTaskProject(projects, projects.map(item => task(item.name)), '2026-09-09')).toBe('Projects/通用事务')
    expect(defaultTaskProject([], [], '2026-09-09')).toBe('Projects/通用事务')
  })
})
